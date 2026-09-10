package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julia-elikhis/romana-learning/internal/materials"
)

func questionRequest(t *testing.T, h http.Handler, method, path string, body any, status int) []byte {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(raw)))
	if w.Code != status {
		t.Fatalf("%s %s: got %d, want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	return w.Body.Bytes()
}

func questionFixtures(t *testing.T, db *sql.DB, count int, status string) (string, []questionEdit) {
	t.Helper()
	id := newID()
	_, err := db.Exec(`INSERT INTO materials(id,root_id,revision,title,filename,kind,content_hash,object_key,extracted_text,reviewed,owner_id) VALUES($1,$1,1,'Questions test','lesson.txt','notes',$1,$1,'Eu sunt acasă.',true,'test-owner')`, id)
	if err != nil {
		t.Fatal(err)
	}
	edits := []questionEdit{}
	for n := 0; n < count; n++ {
		e := questionEdit{ID: fmt.Sprintf("%s-%02d", id, n), Kind: "cloze", Prompt: fmt.Sprintf("Exemplul %d: Eu ____ acasă.", n), Options: []string{}, Answers: []string{"sunt"}, Explanation: "Eu sunt means I am."}
		_, err = db.Exec(`INSERT INTO exercises(id,material_id,kind,prompt,options,answers,explanation,source_quote,source_line,status,generator_version) VALUES($1,$2,$3,$4,'[]','["sunt"]',$5,'Eu sunt acasă.',$6,$7,'test')`, e.ID, id, e.Kind, e.Prompt, e.Explanation, n+1, status)
		if err != nil {
			t.Fatal(err)
		}
		edits = append(edits, e)
	}
	return id, edits
}

func TestBulkPublicationAndDeletionPreserveHistory(t *testing.T) {
	db := isolatedDB(t)
	h := authenticatedHandler(t, db, Server{DB: db}.Routes())
	id, edits := questionFixtures(t, db, 4, "draft")
	_, foreign := questionFixtures(t, db, 1, "draft")
	path := "/api/materials/" + id + "/publish"
	request := func(method, path string, body any, status int) []byte {
		return questionRequest(t, h, method, path, body, status)
	}
	batch := func(items []questionEdit, reviewed bool) any {
		return map[string]any{"exercises": items, "reviewed": reviewed}
	}
	assertUnchanged := func() {
		t.Helper()
		var changed int
		if err := db.QueryRow(`SELECT count(*) FROM exercises WHERE material_id=$1 AND (status<>'draft' OR explanation<>'Eu sunt means I am.')`, id).Scan(&changed); err != nil || changed != 0 {
			t.Fatal("Failed bulk request partially changed questions")
		}
	}
	edits[0].Explanation = "A reviewed explanation from the editor."
	request("POST", path, batch([]questionEdit{}, true), 400)
	request("POST", path, batch([]questionEdit{edits[0], edits[0]}, true), 400)
	request("POST", path, batch([]questionEdit{edits[0], foreign[0]}, true), 404)
	invalid := edits[1]
	invalid.Answers = []string{}
	request("POST", path, batch([]questionEdit{edits[0], invalid}, true), 400)
	assertUnchanged()
	request("POST", path, batch(edits[:2], false), 200)
	request("POST", path, batch(edits[:2], false), 200) // lost-response retry
	changedPublished := edits[0]
	changedPublished.Explanation = "Do not rewrite a published answer."
	request("POST", path, batch([]questionEdit{changedPublished, edits[2]}, true), 409)
	var status string
	db.QueryRow(`SELECT status FROM exercises WHERE id=$1`, edits[2].ID).Scan(&status)
	if status != "draft" {
		t.Fatal("Mixed-state batch published a subset")
	}
	attempt := map[string]any{"id": "managed-attempt-0001", "exerciseId": edits[0].ID, "answer": "sunt"}
	graded := request("POST", "/api/attempts", attempt, 200)
	var result Result
	json.Unmarshal(graded, &result)
	if !result.Correct || result.Explanation != edits[0].Explanation || result.SourceQuote != "Eu sunt acasă." {
		t.Fatal("Bulk publication did not persist the reviewed edits and grading evidence")
	}
	request("DELETE", "/api/exercises/"+edits[0].ID, nil, 200)
	request("DELETE", "/api/exercises/"+edits[0].ID, nil, 200)
	if replay := request("POST", "/api/attempts", attempt, 200); !bytes.Equal(graded, replay) {
		t.Fatal("Deletion changed the saved answer's result")
	}
	attempt["id"] = "managed-attempt-0002"
	request("POST", "/api/attempts", attempt, 400)
	request("POST", path, batch([]questionEdit{edits[0], edits[2]}, true), 404)
	request("POST", "/api/drafts/"+edits[3].ID+"/status", map[string]any{"status": "rejected"}, 200)
	request("POST", path, batch([]questionEdit{edits[2], edits[3]}, true), 409)
	for _, e := range edits[1:] {
		request("DELETE", "/api/exercises/"+e.ID, nil, 200)
	}
	request("DELETE", "/api/exercises/missing", nil, 404)
	var detail struct {
		Material  Material          `json:"material"`
		Exercises []materials.Draft `json:"exercises"`
	}
	json.Unmarshal(request("GET", "/api/materials/"+id, nil, 200), &detail)
	if len(detail.Exercises) != 0 || detail.Material.DraftCount != 0 || detail.Material.PublishedCount != 0 || !detail.Material.Generated {
		t.Fatal("Deleted questions remained visible or unlocked historical source text")
	}
	request("PATCH", "/api/materials/"+id, map[string]any{"text": "Changed", "reviewed": true}, 409)
	var progress struct{ Attempts, Correct, Practiced, Available int }
	json.Unmarshal(request("GET", "/api/progress", nil, 200), &progress)
	if progress.Attempts != 2 || progress.Correct != 2 || progress.Practiced != 1 || progress.Available != len(legacyExercises) {
		t.Fatalf("Saved progress or current availability changed incorrectly: %+v", progress)
	}
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var remaining []PracticeExercise
	json.Unmarshal(request("GET", "/api/exercises", nil, 200), &remaining)
	if len(remaining) != len(legacyExercises) {
		t.Fatal("Deleted course questions returned after migration rerun")
	}
	for _, e := range remaining {
		if !strings.HasPrefix(e.ID, "home-") {
			t.Fatal("Deleted course question remained in practice")
		}
	}
	for _, methodPath := range [][2]string{{"DELETE", "/api/exercises/" + foreign[0].ID}, {"POST", path}} {
		r := httptest.NewRequest(methodPath[0], methodPath[1], strings.NewReader(`{}`))
		r.Header.Set("Origin", "https://foreign.example")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("Cross-origin question mutation was allowed")
		}
	}
}

func TestMixedPracticeSamplesStartersAndPublishedCourseQuestions(t *testing.T) {
	db := isolatedDB(t)
	h := authenticatedHandler(t, db, Server{DB: db}.Routes())
	id, first := questionFixtures(t, db, 7, "published")
	_, second := questionFixtures(t, db, 7, "published")
	for _, status := range []string{"draft", "rejected", "deleted"} {
		questionFixtures(t, db, 2, status)
	}
	allowed := map[string]bool{}
	for _, e := range legacyExercises {
		allowed[e.ID] = true
	}
	for _, e := range append(first, second...) {
		allowed[e.ID] = true
	}
	orders := map[string]bool{}
	for i := 0; i < 5; i++ {
		raw := questionRequest(t, h, "GET", "/api/exercises", nil, 200)
		var deck []PracticeExercise
		json.Unmarshal(raw, &deck)
		if len(deck) != 10 || strings.Contains(string(raw), `"answers"`) || strings.Contains(string(raw), `"sourceQuote"`) {
			t.Fatal("Invalid random practice payload")
		}
		seen := map[string]bool{}
		ids := []string{}
		for _, e := range deck {
			if !allowed[e.ID] || seen[e.ID] {
				t.Fatal("Practice included a duplicate or an unpublished question")
			}
			seen[e.ID] = true
			ids = append(ids, e.ID)
		}
		orders[strings.Join(ids, ",")] = true
	}
	if len(orders) == 1 {
		t.Fatal("Mixed practice repeatedly returned a fixed deck")
	}
	var lesson []PracticeExercise
	json.Unmarshal(questionRequest(t, h, "GET", "/api/exercises?materialId="+id, nil, 200), &lesson)
	if len(lesson) != len(first) {
		t.Fatal("Lesson practice lost questions")
	}
	for i, e := range lesson {
		if e.ID != first[i].ID {
			t.Fatal("Lesson practice mixed in another document")
		}
	}
}
