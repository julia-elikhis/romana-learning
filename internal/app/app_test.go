package app

import (
	"encoding/json"
	"github.com/julia-elikhis/romana-learning/internal/materials"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExerciseAnswersAreNotExposed(t *testing.T) {
	data, err := json.Marshal(PracticeExercise{ID: "example", Kind: "cloze", Prompt: "Eu ____ acasă.", Options: []string{}})
	if err != nil || strings.Contains(string(data), `"answer"`) || strings.Contains(string(data), `"explanation"`) {
		t.Fatalf("Unexpected exercise payload: %s", data)
	}
}

func TestInvalidAttemptsRejectedBeforeDatabase(t *testing.T) {
	tests := []struct {
		name, body, origin string
		status             int
	}{
		{"bad json", "{", "", 400},
		{"missing id", `{"exerciseId":"home-1","answer":"case"}`, "", 400},
		{"missing exercise", `{"id":"0123456789abcdef","answer":"case"}`, "", 400},
		{"empty answer", `{"id":"0123456789abcdef","exerciseId":"home-1","answer":""}`, "", 400},
		{"trailing body", `{"id":"0123456789abcdef","exerciseId":"home-1","answer":"case"} {}`, "", 400},
		{"foreign origin", `{}`, "https://other.example", 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://localhost/api/attempts", strings.NewReader(tt.body))
			req.Header.Set("Origin", tt.origin)
			recorder := httptest.NewRecorder()
			Server{}.Routes().ServeHTTP(recorder, req)
			if recorder.Code != tt.status {
				t.Fatalf("got %d want %d", recorder.Code, tt.status)
			}
		})
	}
}

func TestAPIRequiresAuthenticationBeforeSourceTransfer(t *testing.T) {
	generator, err := materials.NewAPI("https://provider.invalid/private-route?token=private-token", "private-key", "")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/materials/lesson/generate", strings.NewReader(`{"count":5,"mode":"api","sendToApi":false}`))
	w := httptest.NewRecorder()
	Server{Generator: generator}.Routes().ServeHTTP(w, r)
	if w.Code != 401 || strings.Contains(w.Body.String(), "private-token") || strings.Contains(w.Body.String(), "provider.invalid") {
		t.Fatal("Missing transfer confirmation or exposed configuration")
	}
}
