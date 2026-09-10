package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"testing"
)

func TestMultiSelectGradingHistoryAndRetries(t *testing.T) {
	db := isolatedDB(t)
	h := Server{DB: db}.Routes()
	admin := authenticatedHandler(t, db, h)
	material, questions := questionFixtures(t, db, 1, "draft")
	id := questions[0].ID
	options := []string{"Eu sunt acasă.", "Noi suntem acasă.", "Tu este acasă."}
	body := map[string]any{"kind": "multi_select", "prompt": "Select all correct sentences.", "options": options, "answers": options[:2], "explanation": "Eu takes sunt; noi takes suntem; tu takes ești.", "skill": "grammar", "target": "a fi / subject agreement", "difficulty": "medium"}
	questionRequest(t, admin, "PATCH", "/api/exercises/"+id, body, 200)
	body["id"] = id
	questionRequest(t, admin, "POST", "/api/materials/"+material+"/publish", map[string]any{"exercises": []any{body}}, 200)
	questionRequest(t, admin, "POST", "/api/materials/"+material+"/publish", map[string]any{"exercises": []any{body}}, 200)
	for n, selections := range [][]string{options[:1], options, options[1:]} {
		var result Result
		raw := questionRequest(t, admin, "POST", "/api/attempts", map[string]any{"id": fmt.Sprintf("multi-incorrect-%04d", n), "exerciseId": id, "selections": selections}, 200)
		if json.Unmarshal(raw, &result) != nil || result.Correct || !result.Saved || len(result.Answers) != 2 {
			t.Fatal("Partial or excessive selection was marked correct")
		}
	}
	for _, payload := range []map[string]any{
		{"selections": []string{}}, {"selections": []string{options[0], options[0]}}, {"selections": []string{"invented"}}, {"answer": options[0]}, {"answer": "text", "selections": options[:2]},
	} {
		payload["id"] = "invalid-multi-answer"
		payload["exerciseId"] = id
		questionRequest(t, admin, "POST", "/api/attempts", payload, 400)
	}
	submission := map[string]any{"id": "multi-correct-00001", "exerciseId": id, "selections": []string{options[1], options[0]}}
	raw := questionRequest(t, admin, "POST", "/api/attempts", submission, 200)
	var correct Result
	json.Unmarshal(raw, &correct)
	if !correct.Correct || !correct.Saved || len(correct.Answers) != 2 {
		t.Fatal("Complete correct set was not accepted")
	}
	submission["selections"] = options[:2]
	if replay := questionRequest(t, admin, "POST", "/api/attempts", submission, 200); !bytes.Equal(raw, replay) {
		t.Fatal("Selection order changed idempotent result")
	}
	submission["selections"] = options[1:]
	questionRequest(t, admin, "POST", "/api/attempts", submission, 409)
	var guest Result
	json.Unmarshal(questionRequest(t, h, "POST", "/api/attempts", map[string]any{"id": "guest-multi-000001", "exerciseId": id, "selections": options[:2]}, 200), &guest)
	if !guest.Correct || guest.Saved {
		t.Fatal("Anonymous multi-select tracking or grading incorrect")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM attempts WHERE exercise_id=$1`, id).Scan(&count); err != nil || count != 4 {
		t.Fatal("Retry or guest created an extra attempt")
	}
	// Changing the format/key afterwards must leave both full selected and correct
	// option snapshots intact, including retries after soft deletion.
	delete(body, "id")
	body["kind"] = "cloze"
	body["prompt"] = "Use a fi: Noi ____ acasă."
	body["options"] = []string{}
	body["answers"] = []string{"suntem"}
	questionRequest(t, admin, "PATCH", "/api/exercises/"+id, body, 200)
	questionRequest(t, admin, "DELETE", "/api/exercises/"+id, nil, 200)
	submission["selections"] = options[:2]
	if replay := questionRequest(t, admin, "POST", "/api/attempts", submission, 200); !bytes.Equal(raw, replay) {
		t.Fatal("Question edit or deletion rewrote saved answer set")
	}
	var history []HistoryItem
	json.Unmarshal(questionRequest(t, admin, "GET", "/api/history", nil, 200), &history)
	found := false
	for _, item := range history {
		if item.ID == "multi-correct-00001" {
			found = true
			if !slices.Equal(item.Selections, options[:2]) || !slices.Equal(item.CorrectSelections, options[:2]) || item.Prompt != "Select all correct sentences." {
				t.Fatal("Multi-select history lost its snapshot")
			}
		}
	}
	if !found {
		t.Fatal("Saved multi-select history missing")
	}
}

func TestPracticeVariesFormatsButNeverOverridesUnansweredPriority(t *testing.T) {
	db := isolatedDB(t)
	h := authenticatedHandler(t, db, Server{DB: db}.Routes())
	material, questions := questionFixtures(t, db, 4, "published")
	if _, err := db.Exec(`UPDATE exercises SET kind='multiple_choice',options='["sunt","este","suntem"]' WHERE id=$1`, questions[3].ID); err != nil {
		t.Fatal(err)
	}
	params := url.Values{"materialId": {material}, "currentId": {questions[0].ID}, "recentId": {questions[0].ID, questions[1].ID}}
	read := func() PracticeExercise {
		t.Helper()
		var got PracticeExercise
		if json.Unmarshal(questionRequest(t, h, "GET", "/api/practice/question?"+params.Encode(), nil, 200), &got) != nil {
			t.Fatal("Invalid selection")
		}
		return got
	}
	if got := read(); got.Kind != "multiple_choice" {
		t.Fatal("A third cloze was chosen despite another unanswered format")
	}
	for i := 1; i < len(questions); i++ {
		if _, err := db.Exec(`INSERT INTO attempts(id,user_id,exercise_id,answer,correct,question_kind) VALUES($1,'test-owner',$2,'answer',false,'cloze')`, fmt.Sprintf("priority-attempt-%d", i), questions[i].ID); err != nil {
			t.Fatal(err)
		}
	}
	if got := read(); got.ID != questions[0].ID {
		t.Fatal("Format variety displaced the only unanswered question")
	}
	// Once every question is attempted, two recent cloze answers favor a different
	// format even after a reload has discarded browser-local display history.
	if _, err := db.Exec(`INSERT INTO attempts(id,user_id,exercise_id,answer,correct,question_kind) VALUES('priority-final','test-owner',$1,'answer',false,'cloze')`, questions[0].ID); err != nil {
		t.Fatal(err)
	}
	params.Del("recentId")
	if got := read(); got.Kind != "multiple_choice" {
		t.Fatal("Saved recent format history was ignored")
	}
}

func TestPracticeAvoidsRepeatingSpecificTargets(t *testing.T) {
	db := isolatedDB(t)
	h := authenticatedHandler(t, db, Server{DB: db}.Routes())
	material, questions := questionFixtures(t, db, 3, "published")
	if _, err := db.Exec(`UPDATE exercises SET skill='grammar',target='a fi / eu' WHERE material_id=$1;`, material); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE exercises SET target='a fi / noi' WHERE id=$1`, questions[2].ID); err != nil {
		t.Fatal(err)
	}
	params := url.Values{"materialId": {material}, "currentId": {questions[0].ID}, "recentId": {questions[0].ID}}
	var got PracticeExercise
	json.Unmarshal(questionRequest(t, h, "GET", "/api/practice/question?"+params.Encode(), nil, 200), &got)
	if got.ID != questions[2].ID {
		t.Fatal("Selection repeated the same target unnecessarily")
	}
}
