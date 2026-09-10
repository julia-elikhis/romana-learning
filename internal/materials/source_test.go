package materials

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMixedNotesExcludeHebrewFromBothModelPasses(t *testing.T) {
	source := "Present tense of a fi\nEu este acasă. אני בביית\nשגיאות תרגום\nNoi suntem acasă.\nשלום Tu ești aici."
	filtered := GenerationSource(source)
	if !latinText(filtered) || !strings.Contains(filtered, "Eu este acasă.") || !strings.Contains(filtered, "Tu ești aici.") || !strings.Contains(filtered, "Present tense of a fi") {
		t.Fatal("Filtering lost Romanian or retained Hebrew")
	}
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct{ Messages []struct{ Content string } }
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Fatal("Invalid model request")
		}
		for _, m := range request.Messages {
			if !latinText(m.Content) {
				t.Fatal("Non-Latin annotations were sent to the model")
			}
		}
		var output any
		if calls == 1 {
			output = map[string]any{"exercises": []Draft{{Kind: "cloze", Prompt: "Use a fi in the present: Eu ____ acasă.", Answers: []string{"sunt"}, Explanation: "Eu takes sunt. The lesson's este is a mistake.", SourceQuote: "Eu este acasă.", Skill: "grammar", Target: "a fi / eu", Difficulty: "easy"}}}
		} else {
			var input struct{ Questions []Draft }
			if json.Unmarshal([]byte(request.Messages[1].Content), &input) != nil || len(input.Questions) != 1 {
				t.Fatal("Corrected question did not reach independent review")
			}
			output = map[string]any{"reviews": []any{map[string]any{"id": input.Questions[0].ID, "approved": true, "issues": []string{}}}}
		}
		raw, _ := json.Marshal(output)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(raw)}}}})
	}))
	defer server.Close()
	api, _ := NewAPI(server.URL, "", "test-model")
	api.client.Transport = server.Client().Transport
	result, err := api.Generate(context.Background(), source, "lesson", 1)
	if err != nil || len(result.Exercises) != 1 || calls != 2 {
		t.Fatalf("Correction failed: %v", err)
	}
	if result.Exercises[0].SourceLine != 2 || result.Exercises[0].SourceQuote != "Eu este acasă." {
		t.Fatal("Correction lost original evidence location")
	}
	if _, err = api.Generate(context.Background(), "רק עברית", "lesson", 1); err == nil || calls != 2 {
		t.Fatal("An all-Hebrew source reached the model")
	}
	bad := result.Exercises[0]
	bad.Explanation = "עברית"
	if _, err = validateGenerated([]Draft{bad}, source, "lesson"); err == nil {
		t.Fatal("Hebrew model output was accepted")
	}
}
