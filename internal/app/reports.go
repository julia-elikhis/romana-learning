package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/julia-elikhis/romana-learning/internal/materials"
)

type QuestionReport struct {
	ID         string           `json:"id"`
	ExerciseID string           `json:"exerciseId"`
	Prompt     string           `json:"prompt"`
	Note       string           `json:"note"`
	Reporter   string           `json:"reporter"`
	Status     string           `json:"status"`
	CreatedAt  time.Time        `json:"createdAt"`
	Question   *materials.Draft `json:"question"`
}

func (s Server) reportQuestion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID   string `json:"id"`
		Note string `json:"note"`
	}
	if !decode(w, r, &body, 8192) {
		return
	}
	body.Note = strings.TrimSpace(body.Note)
	if !validID.MatchString(body.ID) || len([]rune(body.Note)) > 1000 || strings.ContainsRune(body.Note, 0) {
		fail(w, 400, "Use a note of up to 1,000 characters")
		return
	}
	reporter := ""
	if user := currentUser(r); user != nil {
		reporter = user.ID
	}
	ctx, cancel := contextFor(r)
	defer cancel()
	var id string
	// Take the question snapshot from the database. Anonymous reports contain
	// no account, IP address, cookies, or practice-answer history.
	err := s.DB.QueryRowContext(ctx, `INSERT INTO question_reports(id,exercise_id,reporter_id,question_prompt,note)
 SELECT $1,e.id,NULLIF($3,''),e.prompt,$4 FROM exercises e WHERE e.id=$2 AND e.status='published'
 ON CONFLICT(id) DO NOTHING RETURNING id`, body.ID, r.PathValue("id"), reporter, body.Note).Scan(&id)
	if err == sql.ErrNoRows {
		var matches bool
		err = s.DB.QueryRowContext(ctx, `SELECT exercise_id=$2 AND COALESCE(reporter_id,'')=$3 AND note=$4 FROM question_reports WHERE id=$1`, body.ID, r.PathValue("id"), reporter, body.Note).Scan(&matches)
		if err == sql.ErrNoRows {
			fail(w, 404, "This question is no longer available")
			return
		}
		if err == nil && !matches {
			fail(w, 409, "Report identifier already used")
			return
		}
	}
	if err != nil {
		fail(w, 503, "Could not send the report. Please retry")
		return
	}
	respond(w, 200, map[string]bool{"reported": true})
}

func (s Server) adminReports(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	page := 1
	if value := r.URL.Query().Get("page"); value != "" {
		var err error
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 || page > 100000 {
			fail(w, 400, "Invalid report page")
			return
		}
	}
	if status != "open" && status != "resolved" {
		fail(w, 400, "Invalid report status")
		return
	}
	ctx, cancel := contextFor(r)
	defer cancel()
	const pageSize = 20
	var total int
	if s.DB.QueryRowContext(ctx, `SELECT count(*) FROM question_reports WHERE status=$1`, status).Scan(&total) != nil {
		fail(w, 503, "Could not load reports")
		return
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id,r.exercise_id,r.question_prompt,r.note,COALESCE(u.login,''),r.status,r.created_at,
 CASE WHEN e.status='deleted' THEN 'null'::jsonb ELSE jsonb_build_object(
 'id',e.id,'kind',e.kind,'prompt',e.prompt,'options',e.options,'answers',e.answers,
 'explanation',e.explanation,'sourceQuote',e.source_quote,'sourceLine',e.source_line,'status',e.status,'skill',e.skill,'target',e.target,'difficulty',e.difficulty) END
 FROM question_reports r JOIN exercises e ON e.id=r.exercise_id
 LEFT JOIN app_users u ON u.id=r.reporter_id
 WHERE r.status=$1 ORDER BY r.created_at,r.id LIMIT $2 OFFSET $3`, status, pageSize, (page-1)*pageSize)
	if err != nil {
		fail(w, 503, "Could not load reports")
		return
	}
	defer rows.Close()
	reports := []QuestionReport{}
	for rows.Next() {
		var report QuestionReport
		var raw []byte
		if rows.Scan(&report.ID, &report.ExerciseID, &report.Prompt, &report.Note, &report.Reporter, &report.Status, &report.CreatedAt, &raw) != nil || json.Unmarshal(raw, &report.Question) != nil {
			fail(w, 503, "Could not read reports")
			return
		}
		reports = append(reports, report)
	}
	if rows.Err() != nil {
		fail(w, 503, "Could not read reports")
		return
	}
	respond(w, 200, map[string]any{"reports": reports, "total": total, "page": page, "pageSize": pageSize})
}

func (s Server) resolveReport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &body, 1024) {
		return
	}
	if body.Status != "resolved" && body.Status != "open" {
		fail(w, 400, "Choose open or resolved")
		return
	}
	ctx, cancel := contextFor(r)
	defer cancel()
	result, err := s.DB.ExecContext(ctx, `UPDATE question_reports SET status=$2,
 resolved_at=CASE WHEN $2='resolved' THEN now() ELSE NULL END,
 resolved_by=CASE WHEN $2='resolved' THEN $3 ELSE NULL END WHERE id=$1`, r.PathValue("id"), body.Status, currentUser(r).ID)
	if err != nil {
		fail(w, 503, "Could not update the report")
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		fail(w, 503, "Could not confirm report update")
		return
	}
	if count == 0 {
		fail(w, 404, "Report not found")
		return
	}
	respond(w, 200, map[string]string{"status": body.Status})
}
