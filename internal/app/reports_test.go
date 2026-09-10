package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQuestionReportsPrivacyRetriesAndAdminReview(t *testing.T) {
	db := isolatedDB(t)
	h := Server{DB: db}.Routes()
	admin := authenticatedHandler(t, db, h)
	cookie := testAccount(t, db, "learner")
	learner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.AddCookie(cookie); h.ServeHTTP(w, r) })
	_, questions := questionFixtures(t, db, 1, "published")
	_, drafts := questionFixtures(t, db, 1, "draft")
	path := "/api/exercises/" + questions[0].ID + "/reports"
	adminPath := "/api/admin/reports/report-question-0001"
	report := map[string]string{"id": "report-question-0001", "note": "The accepted answer seems wrong."}
	var attempts int
	if err := db.QueryRow(`SELECT count(*) FROM attempts`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	questionRequest(t, learner, "POST", path, report, 200)
	questionRequest(t, learner, "POST", path, report, 200)
	questionRequest(t, h, "POST", path, report, 409) // another identity cannot reuse a report ID
	for _, route := range []string{"/api/admin/reports", adminPath} {
		method := "GET"
		if route == adminPath {
			method = "PATCH"
		}
		questionRequest(t, h, method, route, map[string]string{"status": "resolved"}, 401)
		questionRequest(t, learner, method, route, map[string]string{"status": "resolved"}, 403)
	}
	// Guests can send a report with no note, but receive neither report contents
	// nor an identifying cookie. Their report does not create practice history.
	guest := map[string]string{"id": "report-question-0002"}
	body, _ := json.Marshal(guest)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewReader(body)))
	if w.Code != 200 || len(w.Result().Cookies()) != 0 || w.Body.String() != "{\"reported\":true}\n" {
		t.Fatal("Guest report failed or exposed private data")
	}
	questionRequest(t, h, "POST", path, guest, 200)
	var count, after int
	if err := db.QueryRow(`SELECT count(*) FROM question_reports`).Scan(&count); err != nil || count != 2 {
		t.Fatal("Report retries created duplicates")
	}
	if err := db.QueryRow(`SELECT count(*) FROM attempts`).Scan(&after); err != nil || attempts != after {
		t.Fatal("Reporting changed practice history")
	}
	var anonymous bool
	if err := db.QueryRow(`SELECT reporter_id IS NULL FROM question_reports WHERE id='report-question-0002'`).Scan(&anonymous); err != nil || !anonymous {
		t.Fatal("Anonymous report acquired an account")
	}
	read := func(status string) []QuestionReport {
		t.Helper()
		var page struct {
			Reports []QuestionReport `json:"reports"`
		}
		if json.Unmarshal(questionRequest(t, admin, "GET", "/api/admin/reports?status="+status, nil, 200), &page) != nil {
			t.Fatal("Could not read reports")
		}
		return page.Reports
	}
	open := read("open")
	if len(open) != 2 || open[0].Note != report["note"] || open[0].Reporter != "learner" || open[1].Reporter != "" || open[0].Question == nil {
		t.Fatal("Admin review did not retain reports and question details")
	}
	// Admins correct the live question using the existing editor, preserving both
	// the report's original prompt and previously recorded grading snapshots.
	graded := questionRequest(t, learner, "POST", "/api/attempts", map[string]string{"id": "report-saved-answer-01", "exerciseId": questions[0].ID, "answer": "sunt"}, 200)
	edited := questions[0]
	edited.Prompt, edited.Answers, edited.Explanation = "Noi ____ acasă.", []string{"suntem"}, "Noi suntem means we are."
	questionRequest(t, admin, "PATCH", "/api/exercises/"+edited.ID, map[string]any{"kind": edited.Kind, "prompt": edited.Prompt, "answers": edited.Answers, "options": edited.Options, "explanation": edited.Explanation}, 200)
	open = read("open")
	if open[0].Prompt != questions[0].Prompt || open[0].Question.Prompt != edited.Prompt || open[0].Question.Answers[0] != "suntem" {
		t.Fatal("Reported snapshot or corrected question was lost")
	}
	if replay := questionRequest(t, learner, "POST", "/api/attempts", map[string]string{"id": "report-saved-answer-01", "exerciseId": questions[0].ID, "answer": "sunt"}, 200); !bytes.Equal(graded, replay) {
		t.Fatal("Review changed the learner's saved grading result")
	}
	questionRequest(t, admin, "PATCH", adminPath, map[string]string{"status": "resolved"}, 200)
	if len(read("open")) != 1 || len(read("resolved")) != 1 {
		t.Fatal("Resolved reports did not leave the open queue")
	}
	var audited bool
	if err := db.QueryRow(`SELECT resolved_at IS NOT NULL AND resolved_by='test-owner' FROM question_reports WHERE id=$1`, report["id"]).Scan(&audited); err != nil || !audited {
		t.Fatal("Resolution did not record its administrator")
	}
	questionRequest(t, admin, "PATCH", adminPath, map[string]string{"status": "open"}, 200)
	questionRequest(t, admin, "DELETE", "/api/exercises/"+edited.ID, nil, 200)
	if open = read("open"); len(open) != 2 || open[0].Question != nil {
		t.Fatal("Deleted question reports could not be reviewed")
	}
	questionRequest(t, learner, "POST", path, report, 200) // retry remains safe after deletion
	questionRequest(t, h, "POST", path, map[string]string{"id": "report-question-0003"}, 404)
	questionRequest(t, h, "POST", "/api/exercises/"+drafts[0].ID+"/reports", guest, 409)
	questionRequest(t, h, "POST", "/api/exercises/"+drafts[0].ID+"/reports", map[string]string{"id": "report-unpublished-01"}, 404)
	questionRequest(t, h, "POST", path, map[string]string{"id": "report-long-note-0001", "note": strings.Repeat("a", 1001)}, 400)
	questionRequest(t, admin, "PATCH", adminPath, map[string]string{"status": "published"}, 400)
	questionRequest(t, admin, "PATCH", "/api/admin/reports/missing", map[string]string{"status": "resolved"}, 404)
}

func TestQuestionReportsPagination(t *testing.T) {
	db := isolatedDB(t)
	h := authenticatedHandler(t, db, Server{DB: db}.Routes())
	_, questions := questionFixtures(t, db, 1, "published")
	for i := 0; i < 23; i++ {
		questionRequest(t, h, "POST", "/api/exercises/"+questions[0].ID+"/reports", map[string]string{"id": fmt.Sprintf("paginated-report-%04d", i)}, 200)
	}
	for page, want := range []int{20, 3} {
		var got struct {
			Reports  []QuestionReport `json:"reports"`
			Total    int              `json:"total"`
			Page     int              `json:"page"`
			PageSize int              `json:"pageSize"`
		}
		if json.Unmarshal(questionRequest(t, h, "GET", fmt.Sprintf("/api/admin/reports?page=%d", page+1), nil, 200), &got) != nil || len(got.Reports) != want || got.Total != 23 || got.Page != page+1 || got.PageSize != 20 {
			t.Fatal("Incorrect report pagination")
		}
	}
	for _, query := range []string{"status=unknown", "page=0", "page=100001", "page=invalid"} {
		questionRequest(t, h, "GET", "/api/admin/reports?"+query, nil, 400)
	}
}
