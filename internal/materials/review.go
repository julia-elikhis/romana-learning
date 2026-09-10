package materials

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

//go:embed prompts/review.txt
var romanianReviewInstruction string

var languageIssueReasons = map[string]string{
	"language":    "The question uses annotations outside Romanian and English",
	"grammar":     "The Romanian grammar or phrasing needs correction",
	"spelling":    "The Romanian spelling or diacritics need correction",
	"answer":      "The accepted answer is incorrect or incomplete",
	"ambiguity":   "The question allows an unclear or ambiguous answer",
	"source":      "The question or intended answer is not supported by the lesson",
	"explanation": "The explanation does not correctly explain the Romanian",
}

type languageVerdict struct {
	ID       string   `json:"id"`
	Approved *bool    `json:"approved"`
	Issues   []string `json:"issues"`
}

func (a *API) reviewRomanian(ctx context.Context, source string, generated GenerationResult) (GenerationResult, error) {
	// Only document content and validated candidates go into this fresh request;
	// the generation conversation and server configuration are never included.
	input, _ := json.Marshal(map[string]any{"teachingText": source, "questions": generated.Exercises})
	content, err := a.chat(ctx, romanianReviewInstruction, string(input))
	if err != nil {
		return GenerationResult{}, fmt.Errorf("The Romanian language review could not complete. Nothing was saved. %w", err)
	}
	var output struct {
		Reviews []languageVerdict `json:"reviews"`
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&output) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return GenerationResult{}, errors.New("The Romanian language review returned invalid JSON. Nothing was saved; try again")
	}
	return applyLanguageReview(generated, output.Reviews)
}

func applyLanguageReview(generated GenerationResult, verdicts []languageVerdict) (GenerationResult, error) {
	invalid := func() (GenerationResult, error) {
		return GenerationResult{}, errors.New("The Romanian language review returned incomplete or inconsistent decisions. Nothing was saved; try again")
	}
	if len(generated.Exercises) == 0 || len(verdicts) != len(generated.Exercises) {
		return invalid()
	}
	expected := map[string]bool{}
	for _, draft := range generated.Exercises {
		expected[draft.ID] = true
	}
	decisions := map[string]languageVerdict{}
	for _, verdict := range verdicts {
		if !expected[verdict.ID] || verdict.Approved == nil {
			return invalid()
		}
		if _, duplicate := decisions[verdict.ID]; duplicate {
			return invalid()
		}
		if (*verdict.Approved && len(verdict.Issues) != 0) || (!*verdict.Approved && len(verdict.Issues) == 0) {
			return invalid()
		}
		for _, issue := range verdict.Issues {
			if _, known := languageIssueReasons[issue]; !known {
				return invalid()
			}
		}
		decisions[verdict.ID] = verdict
	}
	approved := []Draft{}
	for i, draft := range generated.Exercises {
		verdict := decisions[draft.ID]
		if *verdict.Approved {
			approved = append(approved, draft)
			continue
		}
		reasons := []string{}
		seen := map[string]bool{}
		for _, issue := range verdict.Issues {
			if !seen[issue] {
				reasons = append(reasons, languageIssueReasons[issue])
				seen[issue] = true
			}
		}
		number := generated.positions[draft.ID]
		if number == 0 {
			number = i + 1
		}
		generated.Skipped = append(generated.Skipped, GenerationIssue{number, "Romanian review: " + strings.Join(reasons, "; ")})
	}
	if len(approved) == 0 {
		last := generated.Skipped[len(generated.Skipped)-1]
		generated.Exercises = approved
		return generated, fmt.Errorf("No questions passed the Romanian language review. Question %d: %s. Nothing was saved; try generating again", last.Question, last.Reason)
	}
	generated.Exercises = approved
	return generated, nil
}
