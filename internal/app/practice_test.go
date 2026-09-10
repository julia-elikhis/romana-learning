package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPracticeQuestionPrioritizesUnansweredForEachLearner(t *testing.T) {
	db := isolatedDB(t)
	h := Server{DB: db}.Routes()
	testAccount(t, db, "test-owner")
	alice, bob := testAccount(t, db, "alice"), testAccount(t, db, "bob")
	material, questions := questionFixtures(t, db, 3, "published")
	read := func(cookie *http.Cookie, current string) PracticeExercise {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/practice/question?materialId="+material+"&currentId="+current, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var question PracticeExercise
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &question) != nil {
			t.Fatalf("Could not select question: %d", w.Code)
		}
		return question
	}
	// Any submitted answer counts as attempted, including incorrect answers.
	for i := 0; i < 2; i++ {
		if _, err := db.Exec(`INSERT INTO attempts(id,user_id,exercise_id,answer,correct) VALUES($1,'alice',$2,'answer',$3)`, fmt.Sprintf("alice-answer-%d", i), questions[i].ID, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := db.Exec(`INSERT INTO attempts(id,user_id,exercise_id,answer,correct) VALUES($1,'bob',$2,'answer',true)`, fmt.Sprintf("bob-answer-%d", i), questions[i+1].ID); err != nil {
			t.Fatal(err)
		}
	}
	if got := read(alice, questions[2].ID); got.ID != questions[2].ID {
		t.Fatal("A shuffle bypassed the sole unanswered question")
	}
	if got := read(bob, ""); got.ID != questions[0].ID {
		t.Fatal("Another learner's attempts affected question selection")
	}
	// Recording the final answer enables review of the whole pool, while avoiding
	// an immediate repeat. Exercise edits leave that attempt history intact.
	if _, err := db.Exec(`INSERT INTO attempts(id,user_id,exercise_id,answer,correct) VALUES('alice-final','alice',$1,'answer',true)`, questions[2].ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		got := read(alice, questions[2].ID)
		if got.ID != questions[0].ID && got.ID != questions[1].ID {
			t.Fatal("Review did not choose another published question")
		}
	}
}

func TestPracticeQuestionGuestMemoryFilteringAndEmptyPool(t *testing.T) {
	db := isolatedDB(t)
	h := Server{DB: db}.Routes()
	testAccount(t, db, "test-owner")
	material, questions := questionFixtures(t, db, 3, "published")
	for _, status := range []string{"draft", "rejected", "deleted"} {
		questionFixtures(t, db, 1, status)
	}
	var before int
	if err := db.QueryRow(`SELECT count(*) FROM attempts`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	get := func(params url.Values) *PracticeExercise {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/practice/question?"+params.Encode(), nil))
		if w.Code != 200 || len(w.Result().Cookies()) != 0 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("Guest selection failed or tracked a guest")
		}
		var question *PracticeExercise
		if json.Unmarshal(w.Body.Bytes(), &question) != nil {
			t.Fatal("Invalid question response")
		}
		for _, secret := range []string{"answers", "sourceQuote", "userId", "correct", "explanation"} {
			if strings.Contains(w.Body.String(), `"`+secret+`"`) {
				t.Fatal("Question response leaked grading or personal data")
			}
		}
		return question
	}
	params := url.Values{"materialId": {material}, "answeredId": {questions[0].ID}, "currentId": {questions[1].ID}}
	if got := get(params); got == nil || got.ID != questions[2].ID {
		t.Fatal("Guest shuffle did not choose a different unanswered question")
	}
	params["answeredId"] = []string{questions[0].ID, questions[1].ID, questions[2].ID}
	if got := get(params); got == nil || got.ID == questions[1].ID {
		t.Fatal("Guest review failed after all questions were answered")
	}
	params = url.Values{"materialId": {"missing"}}
	if get(params) != nil {
		t.Fatal("An empty lesson fell through to unrelated questions")
	}
	_, solo := questionFixtures(t, db, 1, "published")
	if _, err := db.Exec(`UPDATE exercises SET status='deleted' WHERE id<>$1`, solo[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := get(url.Values{"currentId": {solo[0].ID}}); got == nil || got.ID != solo[0].ID {
		t.Fatal("A single-question pool became empty on shuffle")
	}
	if _, err := db.Exec(`UPDATE exercises SET status='deleted'`); err != nil {
		t.Fatal(err)
	}
	if get(nil) != nil {
		t.Fatal("Unpublished questions entered practice")
	}
	var after int
	if err := db.QueryRow(`SELECT count(*) FROM attempts`).Scan(&after); err != nil || before != after {
		t.Fatal("Guest selection changed saved answer history")
	}
	questionRequest(t, h, "GET", "/api/practice/question?currentId="+strings.Repeat("x", 81), nil, 400)
	questionRequest(t, h, "GET", "/api/practice/question?"+strings.Repeat("answeredId=q&", 201), nil, 400)
}
