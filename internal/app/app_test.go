package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExerciseAnswersAreNotExposed(t *testing.T) {
	recorder := httptest.NewRecorder()
	Server{}.Routes().ServeHTTP(recorder, httptest.NewRequest("GET", "/api/exercises", nil))
	if recorder.Code != 200 || strings.Contains(recorder.Body.String(), `"answer"`) || strings.Contains(recorder.Body.String(), `"explanation"`) {
		t.Fatalf("Unexpected exercise payload: %s", recorder.Body.String())
	}
}

func TestInvalidAttemptsRejectedBeforeDatabase(t *testing.T) {
	tests := []struct {
		name, body, origin string
		status             int
	}{
		{"bad json", "{", "", 400},
		{"missing id", `{"exerciseId":"home-1","answer":"case"}`, "", 400},
		{"unknown exercise", `{"id":"0123456789abcdef","exerciseId":"unknown","answer":"case"}`, "", 400},
		{"invalid option", `{"id":"0123456789abcdef","exerciseId":"home-1","answer":"bogus"}`, "", 400},
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
