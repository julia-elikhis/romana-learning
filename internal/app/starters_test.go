package app

import (
	"context"
	"encoding/json"
	"testing"
)

func TestStarterRestorationPreservesCourseQuestionsAndHistory(t *testing.T) {
	db := isolatedDB(t)
	h := Server{DB: db}.Routes()
	authed := authenticatedHandler(t, db, h)
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM exercises WHERE generator_version='starter-v1' AND status='published'`).Scan(&count); err != nil || count != 5 {
		t.Fatal("A fresh migration must provide the five original starters")
	}
	for _, status := range []string{"draft", "rejected", "deleted"} {
		questionFixtures(t, db, 1, status)
	}
	// Reproduce the installed release that retired the starters, then upgrade twice.
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version=5; UPDATE exercises SET status='deleted' WHERE generator_version='starter-v1'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	var deck []PracticeExercise
	if err := json.Unmarshal(questionRequest(t, h, "GET", "/api/exercises", nil, 200), &deck); err != nil || len(deck) != 5 {
		t.Fatal("Upgrade must restore only the five starters to public practice")
	}
	seen := map[string]bool{}
	for _, e := range deck {
		seen[e.ID] = true
	}
	for _, e := range legacyExercises {
		if !seen[e.ID] {
			t.Fatalf("Missing starter %s", e.ID)
		}
		var result Result
		json.Unmarshal(questionRequest(t, h, "POST", "/api/attempts", Attempt{ID: "anonymous-starter-" + e.ID, ExerciseID: e.ID, Answer: e.Answer}, 200), &result)
		if !result.Correct || result.Saved || result.Explanation != e.Explanation {
			t.Fatal("Anonymous starter practice must grade correctly without saving history")
		}
	}
	if err := db.QueryRow(`SELECT count(*) FROM attempts`).Scan(&count); err != nil || count != 1 {
		t.Fatal("Restoration or anonymous practice changed saved history")
	}
	if err := db.QueryRow(`SELECT count(*) FROM exercises WHERE material_id IS NOT NULL AND status IN ('draft','rejected','deleted')`).Scan(&count); err != nil || count != 3 {
		t.Fatal("Restoration changed course question status")
	}
	var result Result
	json.Unmarshal(questionRequest(t, authed, "POST", "/api/attempts", Attempt{ID: "signed-in-starter-attempt", ExerciseID: "home-2", Answer: "sunt"}, 200), &result)
	if !result.Correct || !result.Saved {
		t.Fatal("Signed-in starter practice must save the answer")
	}
	var progress struct{ Attempts, Practiced, Available int }
	json.Unmarshal(questionRequest(t, authed, "GET", "/api/progress", nil, 200), &progress)
	if progress.Attempts != 2 || progress.Practiced != 2 || progress.Available != 5 {
		t.Fatalf("Starter progress must include both historical and new answers: %+v", progress)
	}
}
