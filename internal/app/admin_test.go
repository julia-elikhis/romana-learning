package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAdminRolesProtectEveryManagementRoute(t *testing.T) {
	db := isolatedDB(t)
	h := Server{DB: db}.Routes()
	admin := testAccount(t, db, "test-owner")
	learner := testAccount(t, db, "learner")
	if _, err := db.Exec(`UPDATE app_users SET github_id=901 WHERE id='test-owner'`); err != nil {
		t.Fatal(err)
	}
	if err := GrantAdmin(context.Background(), db, 901); err != nil {
		t.Fatal(err)
	}
	if err := GrantAdmin(context.Background(), db, 902); err == nil {
		t.Fatal("An unknown GitHub account was granted access")
	}
	id, questions := questionFixtures(t, db, 1, "draft")
	request := func(cookie *http.Cookie, method, path string, body any, status int) []byte {
		t.Helper()
		wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cookie != nil {
				r.AddCookie(cookie)
			}
			h.ServeHTTP(w, r)
		})
		return questionRequest(t, wrapped, method, path, body, status)
	}
	routes := [][2]string{{"GET", "/api/admin/users"}, {"PATCH", "/api/admin/users/learner"}, {"GET", "/api/admin/questions"}, {"GET", "/api/library"}, {"POST", "/api/materials"}, {"GET", "/api/materials/" + id}, {"GET", "/api/materials/" + id + "/original"}, {"PATCH", "/api/materials/" + id}, {"POST", "/api/materials/" + id + "/generate"}, {"POST", "/api/materials/" + id + "/publish"}, {"PATCH", "/api/drafts/" + questions[0].ID}, {"PATCH", "/api/exercises/" + questions[0].ID}, {"POST", "/api/drafts/" + questions[0].ID + "/status"}, {"DELETE", "/api/exercises/" + questions[0].ID}}
	for _, route := range routes {
		request(nil, route[0], route[1], map[string]any{"isAdmin": true}, 401)
		request(learner, route[0], route[1], map[string]any{"isAdmin": true}, 403)
	}
	if !bytes.Contains(request(admin, "GET", "/api/auth/session", nil, 200), []byte(`"isAdmin":true`)) {
		t.Fatal("Session omitted admin role")
	}
	request(admin, "PATCH", "/api/admin/users/test-owner", map[string]any{"isAdmin": false}, 409)
	request(admin, "PATCH", "/api/admin/users/learner", map[string]any{}, 400)
	request(admin, "PATCH", "/api/admin/users/learner", map[string]any{"isAdmin": true}, 200)
	// The existing session gets its new role, without a fresh login, and the
	// promoted user can manage another admin's course material.
	request(learner, "GET", "/api/materials/"+id, nil, 200)
	request(learner, "GET", "/api/admin/questions", nil, 200)
	request(admin, "PATCH", "/api/admin/users/learner", map[string]any{"isAdmin": false}, 200)
	request(learner, "POST", "/api/materials/"+id+"/generate", map[string]any{"count": 5}, 403)
	request(learner, "GET", "/api/history", nil, 200)
	request(learner, "GET", "/api/exercises", nil, 200)
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	request(admin, "GET", "/api/admin/users", nil, 200)
}

func TestQuestionSearchAndPublishedEditsPreserveHistory(t *testing.T) {
	db := isolatedDB(t)
	h := authenticatedHandler(t, db, Server{DB: db}.Routes())
	id, questions := questionFixtures(t, db, 35, "published")
	request := func(method, path string, body any, status int) []byte {
		return questionRequest(t, h, method, path, body, status)
	}
	var page struct {
		Questions             []ManagedQuestion
		Total, Page, PageSize int
	}
	json.Unmarshal(request("GET", "/api/admin/questions?q=Exemplul&status=published", nil, 200), &page)
	if page.Total != 35 || len(page.Questions) != 30 || page.PageSize != 30 {
		t.Fatal("Search did not include all lesson questions")
	}
	json.Unmarshal(request("GET", "/api/admin/questions?q=Exemplul&status=published&page=2", nil, 200), &page)
	if len(page.Questions) != 5 || page.Questions[0].MaterialID != id {
		t.Fatal("Pagination or source title missing")
	}
	json.Unmarshal(request("GET", "/api/admin/questions?q=Starter", nil, 200), &page)
	if page.Total != 5 {
		t.Fatal("Starter questions not manageable")
	}
	json.Unmarshal(request("GET", "/api/admin/questions?q=%25", nil, 200), &page)
	if page.Total != 0 {
		t.Fatal("Search interpreted a literal wildcard")
	}
	json.Unmarshal(request("GET", "/api/admin/questions?q=ACASA", nil, 200), &page)
	if page.Total < 35 {
		t.Fatal("Search did not normalize Romanian diacritics")
	}
	request("GET", "/api/admin/questions?status=deleted", nil, 400)
	request("GET", "/api/admin/questions?page=-1", nil, 400)
	question := questions[0]
	attempt := map[string]any{"id": "before-edit-00000001", "exerciseId": question.ID, "answer": "sunt"}
	before := request("POST", "/api/attempts", attempt, 200)
	question.Prompt = "Complete with a fi: Noi ____ acasă."
	question.Answers = []string{"suntem"}
	question.Explanation = "Noi takes suntem (we are)."
	// A correction is allowed even when its answer differs from the original quote.
	body := map[string]any{"kind": question.Kind, "prompt": question.Prompt, "answers": question.Answers, "options": question.Options, "explanation": question.Explanation}
	request("PATCH", "/api/exercises/"+question.ID, body, 200)
	if !bytes.Equal(before, request("POST", "/api/attempts", attempt, 200)) {
		t.Fatal("Published edit rewrote saved grading")
	}
	var history []HistoryItem
	json.Unmarshal(request("GET", "/api/history", nil, 200), &history)
	if history[0].Prompt == question.Prompt || history[0].Explanation == question.Explanation {
		t.Fatal("Published edit rewrote history snapshots")
	}
	attempt["id"] = "after-edit-000000001"
	attempt["answer"] = "suntem"
	var result Result
	json.Unmarshal(request("POST", "/api/attempts", attempt, 200), &result)
	if !result.Correct || result.Explanation != question.Explanation {
		t.Fatal("Future grading ignored the correction")
	}
	starter := legacyExercises[0]
	body = map[string]any{"kind": "multiple_choice", "prompt": starter.Prompt, "options": starter.Options, "answers": []string{starter.Answer}, "explanation": "An updated explanation."}
	request("PATCH", "/api/exercises/"+starter.ID, body, 200)
	request("DELETE", "/api/exercises/"+question.ID, nil, 200)
	request("PATCH", "/api/exercises/"+question.ID, body, 404)
	if strings.Contains(string(request("GET", "/api/admin/questions?q=suntem", nil, 200)), question.ID) {
		t.Fatal("Deleted question appeared in search")
	}
}
