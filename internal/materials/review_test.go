package materials

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func boolPointer(value bool) *bool { return &value }

func TestLanguageReviewRejectsBadRomanianEvenWhenCopiedFromDocument(t *testing.T) {
	source := "Eu este acasă.\nNoi suntem acasă."
	incorrect := Draft{Kind: "cloze", Prompt: "Eu ____ acasă.", Answers: []string{"este"}, Explanation: "An incorrect explanation copied from teaching notes.", SourceQuote: "Eu este acasă."}
	correct := Draft{Kind: "cloze", Prompt: "Noi ____ acasă.", Answers: []string{"suntem"}, Explanation: "Noi suntem means we are.", SourceQuote: "Noi suntem acasă."}
	unsupported := correct
	unsupported.Answers = []string{}
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var output any
		{
			if calls != 1 || len(request.Messages) != 2 || request.Messages[0].Content != romanianReviewInstruction || request.Messages[1].Role != "user" {
				t.Error("Language review reused the generation conversation or made extra calls")
			}
			var input struct {
				TeachingText string  `json:"teachingText"`
				Questions    []Draft `json:"questions"`
			}
			if json.Unmarshal([]byte(request.Messages[1].Content), &input) != nil || len(input.Questions) != 2 || input.TeachingText != source {
				t.Error("Language reviewer must receive document context and only validated candidates")
				w.WriteHeader(400)
				return
			}
			output = map[string]any{"reviews": []languageVerdict{
				{ID: input.Questions[1].ID, Approved: boolPointer(true), Issues: []string{}},
				{ID: input.Questions[0].ID, Approved: boolPointer(false), Issues: []string{"grammar", "explanation"}},
			}}
		}
		content, _ := json.Marshal(output)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(content)}}}})
	}))
	defer server.Close()
	api, _ := NewAPI(server.URL, "", "test-model")
	api.client.Transport = server.Client().Transport
	candidates, err := validateGenerated([]Draft{unsupported, incorrect, correct}, source, "lesson")
	if err != nil {
		t.Fatal(err)
	}
	result, err := api.reviewRomanian(context.Background(), source, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(result.Exercises) != 1 || result.Exercises[0].Prompt != correct.Prompt || result.Exercises[0].Status != "draft" {
		t.Fatal("Unreviewed or linguistically rejected content was retained")
	}
	if len(result.Skipped) != 2 || result.Skipped[0].Question != 1 || result.Skipped[1].Question != 2 || !strings.Contains(result.Skipped[1].Reason, "Romanian review") {
		t.Fatal("Validation and linguistic rejection reasons lost their original question numbers")
	}
}

func TestIncompleteOrInconsistentLanguageReviewFailsClosed(t *testing.T) {
	generated := GenerationResult{Exercises: []Draft{{ID: "one"}, {ID: "two"}}}
	approved := languageVerdict{ID: "one", Approved: boolPointer(true), Issues: []string{}}
	other := languageVerdict{ID: "two", Approved: boolPointer(true), Issues: []string{}}
	for name, verdicts := range map[string][]languageVerdict{
		"missing":                 {approved},
		"duplicate":               {approved, approved},
		"unknown id":              {approved, {ID: "private-unknown-value", Approved: boolPointer(true)}},
		"missing verdict":         {approved, {ID: "two"}},
		"approval with issue":     {approved, {ID: "two", Approved: boolPointer(true), Issues: []string{"grammar"}}},
		"rejection without issue": {approved, {ID: "two", Approved: boolPointer(false)}},
		"unknown issue":           {approved, {ID: "two", Approved: boolPointer(false), Issues: []string{"private-unknown-value"}}},
		"extra":                   {approved, other, {ID: "extra", Approved: boolPointer(true)}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := applyLanguageReview(generated, verdicts)
			if err == nil || len(result.Exercises) != 0 || strings.Contains(err.Error(), "private-unknown-value") {
				t.Fatal("Invalid review must save nothing and must not expose provider content")
			}
		})
	}
	result, err := applyLanguageReview(generated, []languageVerdict{
		{ID: "one", Approved: boolPointer(false), Issues: []string{"grammar"}},
		{ID: "two", Approved: boolPointer(false), Issues: []string{"ambiguity"}},
	})
	if err == nil || len(result.Exercises) != 0 || !strings.Contains(err.Error(), "No questions passed") {
		t.Fatal("A completely rejected review must not return drafts")
	}
}

func TestLanguageReviewProviderFailuresNeverReturnUnreviewedDrafts(t *testing.T) {
	generated := GenerationResult{Exercises: []Draft{{ID: "one", Kind: "cloze", Prompt: "Eu ____ acasă.", Answers: []string{"sunt"}, SourceQuote: "Eu sunt acasă."}}}
	for _, content := range []string{
		`private-provider-error`,
		`{"reviews":[{"id":"one","approved":true,"issues":[],"correctedPrompt":"private-provider-value"}]}`,
		`{"reviews":[{"id":"one","approved":true,"issues":[]}]} {"extra":true}`,
	} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}}})
		}))
		api, _ := NewAPI(server.URL, "", "test-model")
		api.client.Transport = server.Client().Transport
		result, err := api.reviewRomanian(context.Background(), "Eu sunt acasă.", generated)
		server.Close()
		if err == nil || len(result.Exercises) != 0 || strings.Contains(err.Error(), "private-provider") {
			t.Fatal("Malformed review must not expose private text or return drafts")
		}
	}
	api, _ := NewAPI("https://provider.invalid", "", "test-model")
	api.client.Transport = brokenTransport{}
	result, err := api.reviewRomanian(context.Background(), "Eu sunt acasă.", generated)
	if err == nil || len(result.Exercises) != 0 || !strings.Contains(err.Error(), "language review could not complete") || strings.Contains(err.Error(), "secret") {
		t.Fatal("Network failure must not bypass the required language review")
	}
}
