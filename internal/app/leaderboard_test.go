package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLeaderboardWeekUsesMondayUTC(t *testing.T) {
	for _, value := range []struct{ now, start string }{
		{"2026-09-13T23:59:59Z", "2026-09-07T00:00:00Z"},
		{"2026-09-14T00:00:00Z", "2026-09-14T00:00:00Z"},
		{"2026-09-14T01:00:00+03:00", "2026-09-07T00:00:00Z"},
		{"2027-01-01T12:00:00Z", "2026-12-28T00:00:00Z"},
	} {
		now, _ := time.Parse(time.RFC3339, value.now)
		start, end := leaderboardWeek(now)
		if start.Format(time.RFC3339) != value.start || end.Sub(start) != 7*24*time.Hour {
			t.Fatalf("Incorrect UTC week for %s", value.now)
		}
	}
}

func TestLeaderboardScoresDistinctCorrectAnswersAndKeepsHistoryPrivate(t *testing.T) {
	db := isolatedDB(t)
	h := Server{DB: db}.Routes()
	start, end := leaderboardWeek(time.Now())
	userIDs := []string{"learner-one", "learner-two", "learner-three", "learner-four", "learner-five", "learner-six", "learner-seven", "learner-zero"}
	cookies := map[string]*http.Cookie{}
	for i, id := range userIDs {
		cookies[id] = testAccount(t, db, id)
		if _, err := db.Exec(`UPDATE app_users SET github_id=$2,name='Private full name',is_admin=$3 WHERE id=$1`, id, i+100, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	testAccount(t, db, "unverified-test")
	sequence := 0
	answer := func(user, exercise string, correct bool, at time.Time) {
		t.Helper()
		sequence++
		_, err := db.Exec(`INSERT INTO attempts(id,user_id,exercise_id,answer,correct,created_at,question_prompt,explanation) VALUES($1,$2,$3,'private answer',$4,$5,'private prompt','private explanation')`, fmt.Sprintf("attempt-%d", sequence), user, exercise, correct, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, score := range []int{7, 6, 5, 4, 3, 2, 2} {
		for q := 0; q < score; q++ {
			answer(userIDs[i], fmt.Sprintf("question-%d", q), true, start)
		}
	}
	// Repeats, mistakes, prior weeks, the next week, and legacy/test accounts
	// must not create extra points or public leaderboard identities.
	for i := 0; i < 8; i++ {
		answer(userIDs[5], "question-0", true, start)
	}
	answer(userIDs[5], "wrong", false, start)
	answer(userIDs[5], "old", true, start.Add(-time.Nanosecond))
	answer(userIDs[5], "future", true, end)
	answer(userIDs[7], "wrong", false, start)
	answer("legacy-local-owner", "legacy", true, start)
	answer("unverified-test", "test", true, start)
	read := func(cookie *http.Cookie) (Leaderboard, []byte) {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/leaderboard", nil)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("Leaderboard returned %d: %s", w.Code, w.Body)
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("Leaderboard created a tracking cookie")
		}
		var board Leaderboard
		if json.Unmarshal(w.Body.Bytes(), &board) != nil {
			t.Fatal("Invalid leaderboard response")
		}
		return board, w.Body.Bytes()
	}
	board, raw := read(nil)
	if len(board.Entries) != 5 || board.Participants != 7 || board.You != nil || !board.WeekStart.Equal(start) || !board.WeekEnd.Equal(end) {
		t.Fatalf("Invalid public board: %+v", board)
	}
	for i, entry := range board.Entries {
		if entry.Login != userIDs[i] || entry.Score != 7-i || entry.Rank != i+1 || entry.IsYou {
			t.Fatalf("Incorrect ranking: %+v", entry)
		}
	}
	for _, private := range []string{"private answer", "private prompt", "private explanation", "Private full name", "github_id", "isAdmin", "userId", "attempt-", "legacy-local-owner", "unverified-test"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("Leaderboard exposed %s", private)
		}
	}
	for _, id := range userIDs[5:7] {
		mine, _ := read(cookies[id])
		if len(mine.Entries) != 5 || mine.You == nil || mine.You.Score != 2 || mine.You.Rank != 6 || !mine.You.IsYou {
			t.Fatalf("Missing own rank or incorrect tie: %+v", mine)
		}
	}
	first, _ := read(cookies[userIDs[0]])
	if first.You == nil || !first.Entries[0].IsYou {
		t.Fatal("Admin learner's score was excluded or not highlighted")
	}
	zero, _ := read(cookies[userIDs[7]])
	if zero.You != nil {
		t.Fatal("A zero-point learner received a rank")
	}
}

func TestLeaderboardUpdatesAfterSavedAnswersWithoutCountingGuestsOrRetries(t *testing.T) {
	db := isolatedDB(t)
	h := Server{DB: db}.Routes()
	admin := authenticatedHandler(t, db, h)
	cookie := testAccount(t, db, "test-owner")
	if _, err := db.Exec(`UPDATE app_users SET github_id=101 WHERE id='test-owner'; UPDATE attempts SET created_at='2000-01-01'`); err != nil {
		t.Fatal(err)
	}
	_, questions := questionFixtures(t, db, 2, "published")
	read := func() Leaderboard {
		var board Leaderboard
		if json.Unmarshal(questionRequest(t, h, "GET", "/api/leaderboard", nil, 200), &board) != nil {
			t.Fatal("Invalid leaderboard")
		}
		return board
	}
	if board := read(); len(board.Entries) != 0 || board.Participants != 0 {
		t.Fatal("Empty board contains learners")
	}
	body := map[string]any{"id": "leaderboard-answer-0001", "exerciseId": questions[0].ID, "answer": "sunt"}
	questionRequest(t, h, "POST", "/api/attempts", body, 200)
	if len(read().Entries) != 0 {
		t.Fatal("Anonymous answer was scored")
	}
	learner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.AddCookie(cookie); h.ServeHTTP(w, r) })
	questionRequest(t, learner, "POST", "/api/attempts", body, 200)
	questionRequest(t, learner, "POST", "/api/attempts", body, 200)
	body["id"] = "leaderboard-answer-0002"
	questionRequest(t, learner, "POST", "/api/attempts", body, 200)
	if board := read(); len(board.Entries) != 1 || board.Entries[0].Score != 1 {
		t.Fatal("Saved retries or repeated questions inflated score")
	}
	body["id"] = "leaderboard-answer-0003"
	body["exerciseId"] = questions[1].ID
	questionRequest(t, learner, "POST", "/api/attempts", body, 200)
	if read().Entries[0].Score != 2 {
		t.Fatal("New question did not increase score")
	}
	questionRequest(t, admin, "DELETE", "/api/exercises/"+questions[0].ID, nil, 200)
	if read().Entries[0].Score != 2 {
		t.Fatal("Question deletion removed previously earned credit")
	}
}
