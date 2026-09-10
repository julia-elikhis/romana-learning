package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/julia-elikhis/romana-learning/internal/filestore"
	"github.com/julia-elikhis/romana-learning/internal/materials"
)

type Server struct {
	DB        *sql.DB
	Files     filestore.Store
	Generator *materials.API
	Auth      *GitHubAuth
}
type Attempt struct {
	ID         string   `json:"id"`
	ExerciseID string   `json:"exerciseId"`
	Answer     string   `json:"answer"`
	Selections []string `json:"selections,omitempty"`
}
type Result struct {
	Saved       bool     `json:"saved"`
	Correct     bool     `json:"correct"`
	Answer      string   `json:"answer"`
	Explanation string   `json:"explanation"`
	SourceQuote string   `json:"sourceQuote,omitempty"`
	Answers     []string `json:"answers,omitempty"`
}

var validID = regexp.MustCompile(`^[a-zA-Z0-9-]{16,80}$`)

func (s Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if s.DB.PingContext(ctx) != nil {
			respond(w, 503, map[string]string{"error": "Database unavailable"})
			return
		}
		respond(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/exercises", s.exercises)
	mux.HandleFunc("GET /api/practice/question", s.practiceQuestion)
	mux.HandleFunc("GET /api/progress", s.progress)
	mux.HandleFunc("GET /api/leaderboard", s.leaderboard)
	mux.HandleFunc("POST /api/attempts", s.attempt)
	mux.HandleFunc("POST /api/exercises/{id}/reports", s.reportQuestion)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { respond(w, 404, map[string]string{"error": "Not found"}) })
	mux.HandleFunc("GET /api/auth/session", s.authSession)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /auth/github", s.githubLogin)
	mux.HandleFunc("GET /auth/github/callback", s.githubCallback)
	mux.HandleFunc("GET /api/history", requireUser(s.history))
	mux.HandleFunc("GET /api/library", requireAdmin(s.library))
	mux.HandleFunc("POST /api/materials", requireAdmin(s.upload))
	mux.HandleFunc("GET /api/materials/{id}", requireAdmin(s.material))
	mux.HandleFunc("GET /api/materials/{id}/original", requireAdmin(s.download))
	mux.HandleFunc("PATCH /api/materials/{id}", requireAdmin(s.reviewSource))
	mux.HandleFunc("POST /api/materials/{id}/generate", requireAdmin(s.generate))
	mux.HandleFunc("PATCH /api/drafts/{id}", requireAdmin(s.editExercise))
	mux.HandleFunc("POST /api/drafts/{id}/status", requireAdmin(s.publishExercise))
	mux.HandleFunc("POST /api/materials/{id}/publish", requireAdmin(s.bulkPublish))
	mux.HandleFunc("DELETE /api/exercises/{id}", requireAdmin(s.deleteExercise))
	mux.HandleFunc("GET /api/admin/users", requireAdmin(s.adminUsers))
	mux.HandleFunc("PATCH /api/admin/users/{id}", requireAdmin(s.setAdmin))
	mux.HandleFunc("GET /api/admin/questions", requireAdmin(s.adminQuestions))
	mux.HandleFunc("GET /api/admin/reports", requireAdmin(s.adminReports))
	mux.HandleFunc("PATCH /api/admin/reports/{id}", requireAdmin(s.resolveReport))
	mux.HandleFunc("PATCH /api/exercises/{id}", requireAdmin(s.editExercise))
	sessions := s.sessionMiddleware(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
				fail(w, 403, "Origin not allowed")
				return
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "Origin not allowed")
				return
			}
		}
		sessions.ServeHTTP(w, r)
	})
}

func (s Server) progress(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var attempts, correct, skills int
	user := currentUser(r)
	if user != nil {
		err := s.DB.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE correct), count(DISTINCT exercise_id) FILTER (WHERE correct AND EXISTS(SELECT 1 FROM exercises e WHERE e.id=attempts.exercise_id AND e.status='published')) FROM attempts WHERE user_id=$1`, user.ID).Scan(&attempts, &correct, &skills)
		if err != nil {
			fail(w, 503, "Could not load progress")
			return
		}
	}
	var available int
	if s.DB.QueryRowContext(ctx, `SELECT count(*) FROM exercises WHERE status='published'`).Scan(&available) != nil {
		fail(w, 503, "Could not load progress")
		return
	}
	respond(w, 200, map[string]any{"attempts": attempts, "correct": correct, "practiced": skills, "available": available, "tracked": user != nil})
}

type PracticeExercise struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
}

func (s Server) exercises(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextFor(r)
	defer cancel()
	materialID := r.URL.Query().Get("materialId")
	query := `SELECT id,kind,prompt,options FROM exercises WHERE status='published'`
	var args []any
	if materialID == "" {
		query += ` ORDER BY random() LIMIT 10`
	} else {
		query += ` AND material_id=$1 ORDER BY source_line,id`
		args = append(args, materialID)
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		fail(w, 503, "Could not load exercises")
		return
	}
	defer rows.Close()
	list := []PracticeExercise{}
	for rows.Next() {
		var e PracticeExercise
		var options []byte
		if err = rows.Scan(&e.ID, &e.Kind, &e.Prompt, &options); err != nil {
			fail(w, 503, "Could not load exercises")
			return
		}
		if json.Unmarshal(options, &e.Options) != nil {
			fail(w, 503, "Could not load exercises")
			return
		}
		list = append(list, e)
	}
	if rows.Err() != nil {
		fail(w, 503, "Could not load exercises")
		return
	}
	respond(w, 200, list)
}
func (s Server) attempt(w http.ResponseWriter, r *http.Request) {
	var a Attempt
	if !decode(w, r, &a, 4096) {
		return
	}
	if !validID.MatchString(a.ID) || a.ExerciseID == "" || len(a.ExerciseID) > 80 || !prepareAttempt(&a) {
		fail(w, 400, "Invalid attempt")
		return
	}
	ctx, cancel := contextFor(r)
	defer cancel()
	var storedExercise, storedAnswer string
	var storedSelections, correctOptions []byte
	var result Result
	user := currentUser(r)
	read := func() error {
		return s.DB.QueryRowContext(ctx, `SELECT exercise_id,answer,correct,correct_answer,explanation,source_quote,selected_options,correct_options FROM attempts WHERE id=$1 AND user_id=$2`, a.ID, user.ID).Scan(&storedExercise, &storedAnswer, &result.Correct, &result.Answer, &result.Explanation, &result.SourceQuote, &storedSelections, &correctOptions)
	}
	reply := func() {
		var selected []string
		if json.Unmarshal(storedSelections, &selected) != nil || json.Unmarshal(correctOptions, &result.Answers) != nil {
			fail(w, 503, "Could not read saved answer")
			return
		}
		if storedExercise != a.ExerciseID || storedAnswer != a.Answer || !slices.Equal(selected, a.Selections) {
			fail(w, 409, "Attempt identifier already used")
			return
		}
		result.Saved = true
		respond(w, 200, result)
	}
	if user != nil {
		err := read()
		if err == nil {
			reply()
			return
		}
		if err != sql.ErrNoRows {
			fail(w, 503, "Could not confirm save")
			return
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		fail(w, 503, "Could not save the answer")
		return
	}
	defer tx.Rollback()
	// A deletion must wait for an in-flight answer to finish saving its snapshot.
	d, err := scanDraft(tx.QueryRowContext(ctx, `SELECT `+exerciseFields+` FROM exercises WHERE id=$1 AND status='published' FOR SHARE`, a.ExerciseID))
	if err == sql.ErrNoRows {
		fail(w, 400, "Unknown or unpublished exercise")
		return
	}
	if err != nil {
		fail(w, 503, "Could not load exercise")
		return
	}
	correct, correctAnswer, answerSet, err := gradeAttempt(d, a)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if user == nil {
		respond(w, 200, Result{Correct: correct, Answer: correctAnswer, Answers: answerSet, Explanation: d.Explanation, SourceQuote: d.SourceQuote, Saved: false})
		return
	}
	selectedJSON, _ := json.Marshal(nonNilStrings(a.Selections))
	correctJSON, _ := json.Marshal(nonNilStrings(answerSet))
	_, err = tx.ExecContext(ctx, `INSERT INTO attempts(id,exercise_id,answer,correct,correct_answer,explanation,source_quote,user_id,question_prompt,selected_options,correct_options,question_kind,question_skill,question_target) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT(user_id,id) DO NOTHING`, a.ID, a.ExerciseID, a.Answer, correct, correctAnswer, d.Explanation, d.SourceQuote, user.ID, d.Prompt, string(selectedJSON), string(correctJSON), d.Kind, d.Skill, d.Target)
	if err != nil {
		fail(w, 503, "Answer was not saved. Please retry.")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 503, "Could not confirm the saved answer. Please retry")
		return
	}
	if read() != nil {
		fail(w, 503, "Could not confirm save")
		return
	}
	reply()
}

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
