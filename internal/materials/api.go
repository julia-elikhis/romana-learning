package materials

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const APIGeneratorVersion = "chat-completions-reviewed-v3"

const apiRequestTimeout = 55 * time.Second
const GenerationTimeout = 2*apiRequestTimeout + 10*time.Second

// API keeps the complete endpoint and bearer key private. Never serialize or log it.
type API struct {
	endpoint, key, model string
	effort               string
	effortFormat         string
	client               *http.Client
	modelRequired        bool
}

func APIFromEnv() (*API, error) {
	api, err := NewAPI(os.Getenv("EXERCISE_API_URL"), os.Getenv("EXERCISE_API_KEY"), os.Getenv("EXERCISE_API_MODEL"))
	if err != nil || api == nil {
		return api, err
	}
	effort := strings.ToLower(strings.TrimSpace(os.Getenv("EXERCISE_API_EFFORT")))
	switch effort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh", "max":
		api.effort = effort
	default:
		return nil, errors.New("Invalid EXERCISE_API_EFFORT; leave it blank or use none, minimal, low, medium, high, xhigh, or max")
	}
	switch format := os.Getenv("EXERCISE_API_EFFORT_FORMAT"); format {
	case "", "reasoning_effort", "reasoning":
		api.effortFormat = format
	default:
		return nil, errors.New("EXERCISE_API_EFFORT_FORMAT must be reasoning_effort or reasoning")
	}
	return api, nil
}
func NewAPI(endpoint, key, model string) (*API, error) {
	if endpoint == "" {
		return nil, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.Scheme != "https" || u.Fragment != "" || u.User != nil {
		return nil, errors.New("EXERCISE_API_URL must be a complete HTTPS endpoint without a fragment or embedded username")
	}
	base := strings.TrimRight(u.Path, "/")
	requiresModel := base == "" || strings.HasSuffix(base, "/v1")
	if requiresModel {
		if base == "" {
			base = "/v1"
		}
		u.Path = base + "/chat/completions"
		u.RawPath = ""
		endpoint = u.String()
	}
	if strings.ContainsAny(key, "\r\n") {
		return nil, errors.New("Invalid EXERCISE_API_KEY")
	}
	return &API{endpoint: endpoint, key: key, model: model, modelRequired: requiresModel, client: &http.Client{Timeout: apiRequestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (a *API) Ready() bool { return a != nil && (!a.modelRequired || a.model != "") }

type GenerationIssue struct {
	Question int    `json:"question"`
	Reason   string `json:"reason"`
}

type GenerationResult struct {
	Exercises []Draft
	Skipped   []GenerationIssue
	positions map[string]int
}

//go:embed prompts/generate.txt
var exerciseInstruction string

func (a *API) Generate(ctx context.Context, source, materialID string, count int) (GenerationResult, error) {
	if !a.Ready() {
		return GenerationResult{}, errors.New("Set EXERCISE_API_MODEL on the server before generating with this API")
	}
	if count < 1 || count > 20 {
		return GenerationResult{}, errors.New("Choose between 1 and 20 exercises")
	}
	inputSource := GenerationSource(source)
	if strings.TrimSpace(inputSource) == "" {
		return GenerationResult{}, errors.New("No Romanian or English lesson text found")
	}
	if len(inputSource) > 30000 {
		return GenerationResult{}, errors.New("For API generation, shorten the Romanian and English teaching text to 30 KB or split it into lessons")
	}
	content, err := a.chat(ctx, exerciseInstruction, fmt.Sprintf("Create at most %d exercises from this source. Source begins after the next newline.\n%s", count, inputSource))
	if err != nil {
		return GenerationResult{}, err
	}
	var output struct {
		Exercises []Draft `json:"exercises"`
	}
	if json.Unmarshal([]byte(content), &output) != nil || len(output.Exercises) == 0 || len(output.Exercises) > count {
		return GenerationResult{}, errors.New("Generation API returned invalid exercise JSON")
	}
	generated, err := validateGenerated(output.Exercises, source, materialID)
	if err != nil {
		return GenerationResult{}, err
	}
	return a.reviewRomanian(ctx, inputSource, generated)
}

// Each pass starts a fresh conversation with the same private provider settings.
func (a *API) chat(ctx context.Context, instruction, input string) (string, error) {
	body := map[string]any{"messages": []map[string]string{{"role": "system", "content": instruction}, {"role": "user", "content": input}}, "response_format": map[string]string{"type": "json_object"}, "stream": false, "store": false}
	if a.model != "" {
		body["model"] = a.model
	}
	if a.effort != "" {
		if a.effortFormat == "reasoning" {
			body["reasoning"] = map[string]string{"effort": a.effort}
		} else {
			body["reasoning_effort"] = a.effort
		}
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", a.endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", errors.New("Could not prepare the generation request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if a.key != "" {
		req.Header.Set("Authorization", "Bearer "+a.key)
	}
	response, err := a.client.Do(req)
	if err != nil {
		return "", errors.New("Could not reach the generation API or the request timed out. Check server configuration")
	}
	defer response.Body.Close()
	// Do not expose upstream bodies, headers, or network errors: URLs may contain credentials.
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("Generation API returned HTTP %d. Check the endpoint, credentials, model, and chat-completions compatibility", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if err != nil || len(data) > 256<<10 {
		return "", errors.New("Generation API returned an unreadable or oversized response")
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &envelope) != nil || len(envelope.Choices) != 1 || envelope.Choices[0].Message.Refusal != "" || envelope.Choices[0].FinishReason != "stop" {
		return "", errors.New("Generation API did not return a complete chat-completions answer")
	}
	return envelope.Choices[0].Message.Content, nil
}

func validateGenerated(candidates []Draft, source, materialID string) (GenerationResult, error) {
	result := GenerationResult{Exercises: []Draft{}, Skipped: []GenerationIssue{}, positions: map[string]int{}}
	seen := map[string]bool{}
	for i, d := range candidates {
		if strings.Count(d.Prompt, "____") != 1 {
			result.Skipped = append(result.Skipped, GenerationIssue{i + 1, "A generated question needs exactly one ____ gap"})
			continue
		}
		// Source locations are derived from exact evidence, never model-supplied line counts.
		position := strings.Index(source, d.SourceQuote)
		if len(d.SourceQuote) < 5 || len(d.SourceQuote) > 2000 || position < 0 {
			result.Skipped = append(result.Skipped, GenerationIssue{i + 1, "The source quote must be copied exactly from the teaching text"})
			continue
		}
		if !latinText(d.SourceQuote + d.Prompt + d.Explanation + strings.Join(d.Answers, " ") + strings.Join(d.Options, " ")) {
			result.Skipped = append(result.Skipped, GenerationIssue{i + 1, "Use Romanian questions and English explanations; ignore other-language annotations"})
			continue
		}
		d.SourceLine = 1 + strings.Count(source[:position], "\n")
		if d.Options == nil {
			d.Options = []string{}
		}
		if err := Validate(d); err != nil {
			// Validate returns fixed rule descriptions, without model text or private configuration.
			result.Skipped = append(result.Skipped, GenerationIssue{i + 1, err.Error()})
			continue
		}
		fingerprint := Normalize(d.Prompt)
		if seen[fingerprint] {
			result.Skipped = append(result.Skipped, GenerationIssue{i + 1, "This question duplicates another generated question"})
			continue
		}
		seen[fingerprint] = true
		hash := sha256.Sum256([]byte(materialID + "\x00" + APIGeneratorVersion + "\x00" + d.Prompt))
		d.ID = "generated-" + hex.EncodeToString(hash[:16])
		d.Status = "draft"
		result.positions[d.ID] = i + 1
		result.Exercises = append(result.Exercises, d)
	}
	if len(result.Exercises) == 0 {
		if len(result.Skipped) > 0 {
			first := result.Skipped[0]
			return GenerationResult{}, fmt.Errorf("The AI returned no valid questions. Question %d: %s. Nothing was saved; try generating again", first.Question, first.Reason)
		}
		return GenerationResult{}, errors.New("The AI returned no questions. Nothing was saved; try generating again")
	}
	return result, nil
}
