package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/julia-elikhis/romana-learning/internal/filestore"
	"github.com/julia-elikhis/romana-learning/internal/materials"
)

func isolatedDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("Set TEST_DATABASE_URL to run the Postgres integration test")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("Invalid TEST_DATABASE_URL")
	}
	admin := stdlib.OpenDB(*cfg)
	schema := "test_" + newID()
	if _, err = admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		admin.Close()
		t.Fatal("Could not create isolated test schema")
	}
	cfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { db.Close(); admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	// Simulate the previous release with saved progress before the versioned migration.
	_, err = db.Exec(`CREATE TABLE attempts(id TEXT PRIMARY KEY,exercise_id TEXT NOT NULL,answer TEXT NOT NULL,correct BOOLEAN NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now());INSERT INTO attempts(id,exercise_id,answer,correct) VALUES('legacy-attempt-01','home-1','case',true)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	var answer string
	if err = db.QueryRow(`SELECT correct_answer FROM attempts WHERE id='legacy-attempt-01'`).Scan(&answer); err != nil || answer != "case" {
		t.Fatal("Migration did not preserve prior grading")
	}
	return db
}
func TestLibraryLifecycle(t *testing.T) {
	db := isolatedDB(t)
	files, err := filestore.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	handler := authenticatedHandler(t, db, Server{DB: db, Files: files}.Routes())
	request := func(method, path string, body any, status int) []byte {
		t.Helper()
		var input io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			input = bytes.NewReader(raw)
		}
		r := httptest.NewRequest(method, path, input)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s got %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w.Body.Bytes()
	}
	source := "Eu sunt acasă în fiecare zi.\nNoi avem o casă foarte frumoasă.\nTu mergi la școală dimineața."
	upload := func(text, kind, replaces string) string {
		t.Helper()
		var b bytes.Buffer
		form := multipart.NewWriter(&b)
		form.WriteField("title", "Test lesson")
		form.WriteField("kind", kind)
		form.WriteField("replacesId", replaces)
		file, _ := form.CreateFormFile("file", "lesson.txt")
		io.WriteString(file, text)
		form.Close()
		r := httptest.NewRequest("POST", "/api/materials", &b)
		r.Header.Set("Content-Type", form.FormDataContentType())
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 && w.Code != 201 {
			t.Fatalf("Upload failed %s", w.Body)
		}
		var got struct {
			ID string `json:"id"`
		}
		json.Unmarshal(w.Body.Bytes(), &got)
		return got.ID
	}
	id := upload(source, "notes", "")
	if upload(source, "notes", "") != id {
		t.Fatal("Duplicate import created a second document")
	}
	if string(request("GET", "/api/materials/"+id+"/original", nil, 200)) != source {
		t.Fatal("Original did not round trip")
	}
	request("POST", "/api/materials/"+id+"/generate", map[string]any{"count": 5}, 409)
	request("PATCH", "/api/materials/"+id, map[string]any{"text": source, "reviewed": false}, 400)
	request("PATCH", "/api/materials/"+id, map[string]any{"text": source, "reviewed": true}, 200)
	request("POST", "/api/materials/"+id+"/generate", map[string]any{"count": 5, "mode": "api"}, 400)
	request("POST", "/api/materials/"+id+"/generate", map[string]any{"count": 5}, 201)
	request("POST", "/api/materials/"+id+"/generate", map[string]any{"count": 5}, 200)
	var detail struct {
		Material  Material          `json:"material"`
		Exercises []materials.Draft `json:"exercises"`
	}
	json.Unmarshal(request("GET", "/api/materials/"+id, nil, 200), &detail)
	if len(detail.Exercises) != 3 {
		t.Fatalf("Unexpected generated count %d", len(detail.Exercises))
	}
	var deck []PracticeExercise
	json.Unmarshal(request("GET", "/api/exercises?materialId="+id, nil, 200), &deck)
	if len(deck) != 0 {
		t.Fatal("Unreviewed exercises leaked into practice")
	}
	first := detail.Exercises[0]
	attempt := map[string]any{"id": "test-attempt-0001", "exerciseId": first.ID, "answer": first.Answers[0]}
	request("POST", "/api/attempts", attempt, 400)
	request("POST", "/api/drafts/"+first.ID+"/status", map[string]any{"status": "published", "reviewed": false}, 400)
	edit := map[string]any{"kind": first.Kind, "prompt": first.Prompt, "options": first.Options, "answers": []string{"invented"}, "explanation": "Edited explanation"}
	request("PATCH", "/api/drafts/"+first.ID, edit, 400)
	edit["answers"] = first.Answers
	request("PATCH", "/api/drafts/"+first.ID, edit, 200)
	request("POST", "/api/drafts/"+first.ID+"/status", map[string]any{"status": "published", "reviewed": true}, 200)
	request("PATCH", "/api/drafts/"+first.ID, edit, 409)
	request("PATCH", "/api/materials/"+id, map[string]any{"text": "Changed source", "reviewed": true}, 409)
	raw := request("GET", "/api/exercises?materialId="+id, nil, 200)
	json.Unmarshal(raw, &deck)
	if len(deck) != 1 || strings.Contains(string(raw), `"answers"`) || strings.Contains(string(raw), `"sourceQuote"`) {
		t.Fatal("Practice payload exposed grading data")
	}
	graded := request("POST", "/api/attempts", attempt, 200)
	replayed := request("POST", "/api/attempts", attempt, 200)
	if !bytes.Equal(graded, replayed) {
		t.Fatal("Retry changed grading")
	}
	var result Result
	json.Unmarshal(graded, &result)
	if !result.Correct || result.Explanation != "Edited explanation" || result.SourceQuote != first.SourceQuote {
		t.Fatal("Missing grading evidence")
	}
	attempt["answer"] = "other"
	request("POST", "/api/attempts", attempt, 409)
	var count int
	db.QueryRow(`SELECT count(*) FROM attempts WHERE id='test-attempt-0001'`).Scan(&count)
	if count != 1 {
		t.Fatal("Retry duplicated progress")
	}
	// Typed responses preserve Romanian diacritics while accepting case and spacing.
	typed := detail.Exercises[1]
	request("POST", "/api/drafts/"+typed.ID+"/status", map[string]any{"status": "published", "reviewed": true}, 200)
	request("POST", "/api/attempts", map[string]any{"id": "test-attempt-0002", "exerciseId": typed.ID, "answer": " " + strings.ToUpper(typed.Answers[0]) + " "}, 200)
	request("POST", "/api/drafts/"+detail.Exercises[2].ID+"/status", map[string]any{"status": "rejected"}, 200)
	revision := upload(source+"\nEi lucrează în fiecare dimineață.", "notes", id)
	if revision == id {
		t.Fatal("Revision reused identity")
	}
	json.Unmarshal(request("GET", "/api/materials/"+revision, nil, 200), &detail)
	if detail.Material.Revision != 2 || detail.Material.Reviewed {
		t.Fatal("Revision inherited review")
	}
	homework := upload(source, "homework", "")
	request("PATCH", "/api/materials/"+homework, map[string]any{"text": source, "reviewed": true}, 400)
	request("GET", "/api/materials/unknown", nil, 404)
	for _, path := range []string{"/api/materials", "/api/materials/" + id + "/generate", "/api/drafts/" + first.ID + "/status"} {
		r := httptest.NewRequest("POST", path, strings.NewReader("{}"))
		r.Header.Set("Origin", "https://foreign.example")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatal(fmt.Sprintf("Cross-origin write allowed: %s", path))
		}
	}
}
