package materials

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIProducesTraceableDrafts(t *testing.T) {
	source := "Eu sunt acasă în fiecare zi."
	candidate := Draft{ID: "untrusted", Kind: "cloze", Prompt: "Eu ____ acasă în fiecare zi.", Options: []string{}, Answers: []string{"sunt"}, Explanation: "Eu sunt means I am.", SourceQuote: source, SourceLine: 1, Status: "published"}
	var received, reviewed bool
	wantEffort := ""
	wantFormat := ""
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer private-key" {
			t.Error("Wrong request or authentication")
		}
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["model"] != "test-model" || payload["store"] != false || payload["stream"] != false {
			t.Error("Wrong provider options")
		}
		if wantEffort == "" {
			if payload["reasoning_effort"] != nil || payload["reasoning"] != nil {
				t.Error("Unset effort must be omitted to preserve the provider default")
			}
		} else if wantFormat == "reasoning" {
			reasoning, ok := payload["reasoning"].(map[string]any)
			if !ok || reasoning["effort"] != wantEffort || payload["reasoning_effort"] != nil {
				t.Error("Gateway must receive only the nested effort setting")
			}
		} else if payload["reasoning_effort"] != wantEffort || payload["reasoning"] != nil {
			t.Error("Configured reasoning effort was not forwarded")
		}
		raw, _ := json.Marshal(payload)
		if strings.Contains(string(raw), "private-key") || strings.Contains(string(raw), "private-endpoint") {
			t.Error("Credentials leaked into model input")
		}
		output, _ := json.Marshal(map[string]any{"exercises": []Draft{candidate}})
		messages := payload["messages"].([]any)
		if messages[0].(map[string]any)["content"] == romanianReviewInstruction {
			reviewed = true
			var input struct {
				TeachingText string  `json:"teachingText"`
				Questions    []Draft `json:"questions"`
			}
			if len(messages) != 2 || json.Unmarshal([]byte(messages[1].(map[string]any)["content"].(string)), &input) != nil || input.TeachingText != source || len(input.Questions) != 1 {
				t.Fatal("The language review did not receive an independent conversation and source")
			}
			output, _ = json.Marshal(map[string]any{"reviews": []any{map[string]any{"id": input.Questions[0].ID, "approved": true, "issues": []string{}}}})
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(output)}}}})
	}))
	defer server.Close()
	api, err := NewAPI(server.URL+"/private-endpoint?token=secret", "private-key", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	api.client.Transport = server.Client().Transport
	result, err := api.Generate(context.Background(), source, "material", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !received || !reviewed || len(result.Exercises) != 1 || result.Exercises[0].ID == "untrusted" || result.Exercises[0].Status != "draft" {
		t.Fatal("Provider controlled draft identity or publication")
	}
	t.Setenv("EXERCISE_API_URL", server.URL+"/private-endpoint?token=secret")
	t.Setenv("EXERCISE_API_KEY", "private-key")
	t.Setenv("EXERCISE_API_MODEL", "test-model")
	for _, format := range []string{"", "reasoning_effort", "reasoning"} {
		t.Setenv("EXERCISE_API_EFFORT_FORMAT", format)
		wantFormat = format
		for _, setting := range []string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max", " HIGH "} {
			t.Setenv("EXERCISE_API_EFFORT", setting)
			wantEffort = strings.ToLower(strings.TrimSpace(setting))
			api, err = APIFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			api.client.Transport = server.Client().Transport
			if _, err = api.Generate(context.Background(), source, "material", 5); err != nil {
				t.Fatal(err)
			}
		}
	}
	candidate.SourceQuote = "Noi avem o casă."
	if _, err = api.Generate(context.Background(), source, "material", 5); err == nil {
		t.Fatal("Invented source was accepted")
	}
	candidate.SourceQuote = source
	candidate.Answers = []string{"invented"}
	if _, err = api.Generate(context.Background(), source, "material", 5); err == nil {
		t.Fatal("Unsupported answer accepted")
	}
}

type brokenTransport struct{}

func (brokenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("private-endpoint?token=secret private-key")
}
func TestAPIErrorsAndRedirectsDoNotExposeSecrets(t *testing.T) {
	api, _ := NewAPI("https://private-endpoint.example/generate?token=secret", "private-key", "")
	api.client.Transport = brokenTransport{}
	_, err := api.Generate(context.Background(), "Eu sunt acasă în fiecare zi.", "material", 1)
	if err == nil || strings.Contains(err.Error(), "private-endpoint") || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private-key") {
		t.Fatal("Network errors leaked credentials")
	}
	visited := false
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { visited = true }))
	defer destination.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", destination.URL)
		w.WriteHeader(307)
		w.Write([]byte("private-key private-endpoint?token=secret"))
	}))
	defer server.Close()
	api, _ = NewAPI(server.URL, "private-key", "test-model")
	api.client.Transport = server.Client().Transport
	_, err = api.Generate(context.Background(), "Eu sunt acasă în fiecare zi.", "material", 1)
	if err == nil || visited || strings.Contains(err.Error(), "private-key") {
		t.Fatal("Redirect followed or upstream secret exposed")
	}
	for _, bad := range []string{"http://example.com", "https://user:password@example.com", "https://example.com/#secret", ":bad"} {
		if _, err = NewAPI(bad, "", ""); err == nil {
			t.Fatal("Unsafe endpoint accepted")
		}
	}
}

func TestInvalidEffortDoesNotEchoConfiguration(t *testing.T) {
	t.Setenv("EXERCISE_API_URL", "https://provider.invalid/v1")
	t.Setenv("EXERCISE_API_KEY", "private-key")
	t.Setenv("EXERCISE_API_MODEL", "test-model")
	t.Setenv("EXERCISE_API_EFFORT", "accidentally-pasted-private-value")
	api, err := APIFromEnv()
	if api != nil || err == nil || !strings.Contains(err.Error(), "EXERCISE_API_EFFORT") || strings.Contains(err.Error(), "accidentally-pasted-private-value") {
		t.Fatal("Invalid effort must fail without exposing its value")
	}
	t.Setenv("EXERCISE_API_EFFORT", "high")
	t.Setenv("EXERCISE_API_EFFORT_FORMAT", "accidentally-pasted-private-value")
	api, err = APIFromEnv()
	if api != nil || err == nil || !strings.Contains(err.Error(), "EXERCISE_API_EFFORT_FORMAT") || strings.Contains(err.Error(), "accidentally-pasted-private-value") {
		t.Fatal("Invalid effort format must fail without exposing its value")
	}
}

func TestAPIBaseURLNeedsModel(t *testing.T) {
	api, err := NewAPI("https://provider.invalid/api/v1?token=secret", "key", "")
	if err != nil {
		t.Fatal(err)
	}
	if api.Ready() {
		t.Fatal("Base URL must require a model")
	}
	if api.endpoint != "https://provider.invalid/api/v1/chat/completions?token=secret" {
		t.Fatal("Base URL was not resolved correctly")
	}
	api.client.Transport = brokenTransport{}
	if _, err = api.Generate(context.Background(), "source", "id", 1); err == nil || !strings.Contains(err.Error(), "EXERCISE_API_MODEL") {
		t.Fatal("Missing model was not caught before network access")
	}
	api.model = "chosen-model"
	if !api.Ready() {
		t.Fatal("Configured model not recognized")
	}
}

func TestGeneratedReferencesComeFromSourceAndInvalidCandidatesAreSkipped(t *testing.T) {
	source := "Teaching notes\n\nEu sunt acasă în fiecare zi.\nNoi avem o casă frumoasă."
	first := Draft{Kind: "cloze", Prompt: "Eu ____ acasă în fiecare zi.", Answers: []string{"sunt"}, Explanation: "Eu sunt means I am.", SourceQuote: "Eu sunt acasă în fiecare zi.", SourceLine: 4}
	second := Draft{Kind: "multiple_choice", Prompt: "Noi ____ o casă frumoasă.", Options: []string{"avem", "aveți", "au"}, Answers: []string{"avem"}, Explanation: "Noi takes avem.", SourceQuote: "Noi avem o casă frumoasă.", SourceLine: 999}
	unsupported := first
	unsupported.Answers = []string{"invented"}
	badOptions := second
	badOptions.Options = []string{"avem", " AVEM. "}
	fakeQuote := first
	fakeQuote.SourceQuote = "This text is absent from the source."
	result, err := validateGenerated([]Draft{unsupported, first, badOptions, fakeQuote, second, first}, source, "lesson")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Exercises) != 2 || len(result.Skipped) != 4 {
		t.Fatalf("Expected two validated drafts and four skipped candidates, got %d and %d", len(result.Exercises), len(result.Skipped))
	}
	if result.Exercises[0].SourceLine != 3 || result.Exercises[1].SourceLine != 4 {
		t.Fatal("Source line counts were trusted instead of derived from exact quotes")
	}
	for _, d := range result.Exercises {
		if err := Validate(d); err != nil || d.Status != "draft" || d.ID == "" {
			t.Fatal("A returned draft is not valid or ready for review")
		}
	}
	for i, want := range []struct {
		question int
		reason   string
	}{{1, "primary answer"}, {3, "distinct"}, {4, "source quote"}, {6, "duplicates"}} {
		if result.Skipped[i].Question != want.question || !strings.Contains(result.Skipped[i].Reason, want.reason) {
			t.Fatal("Skipped candidates lack the specific safe validation reason")
		}
	}
	// An exact quote spanning lines is still supported evidence; report its starting line.
	first.SourceQuote = "Eu sunt acasă în fiecare zi.\nNoi avem o casă frumoasă."
	first.SourceLine = -1
	result, err = validateGenerated([]Draft{first}, source, "lesson")
	if err != nil || result.Exercises[0].SourceLine != 3 {
		t.Fatal("An exact multiline quote was rejected")
	}
}

func TestAllInvalidGeneratedAnswersReportRuleWithoutLeakingText(t *testing.T) {
	source := "Eu sunt acasă în fiecare zi."
	candidate := Draft{Kind: "cloze", Prompt: "Eu ____ acasă în fiecare zi.", Answers: []string{"private-provider-value"}, Explanation: "A model explanation.", SourceQuote: source}
	result, err := validateGenerated([]Draft{candidate}, source, "lesson")
	if err == nil || len(result.Exercises) != 0 || !strings.Contains(err.Error(), "Question 1") || !strings.Contains(err.Error(), "primary answer must appear") || strings.Contains(err.Error(), "private-provider-value") {
		t.Fatal("Invalid answer must be rejected with a specific rule and no model content")
	}
	candidate.Answers = []string{"sunt"}
	candidate.Prompt = "A question without its required blank."
	_, err = validateGenerated([]Draft{candidate}, source, "lesson")
	if err == nil || !strings.Contains(err.Error(), "____ gap") {
		t.Fatal("The user cannot identify the rejected prompt rule")
	}
}
