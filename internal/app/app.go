package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Exercise struct {
	ID          string   `json:"id"`
	Prompt      string   `json:"prompt"`
	Options     []string `json:"options"`
	Answer      string   `json:"-"`
	Explanation string   `json:"-"`
}

var Exercises = []Exercise{
	{"home-1", "Choose the plural: o casă → două …", []string{"case", "casă", "casi"}, "case", "O casă, două case — a house, two houses."},
	{"home-2", "Complete: Eu … acasă.", []string{"este", "sunt", "suntem"}, "sunt", "Eu sunt = I am. Eu sunt acasă = I am at home."},
	{"home-3", "Choose the plural: un apartament → două …", []string{"apartament", "apartamente", "apartamenti"}, "apartamente", "Apartament is neuter: un apartament, două apartamente."},
	{"home-4", "Complete: Noi … o problemă.", []string{"avem", "am", "are"}, "avem", "Noi avem o problemă = We have a problem."},
	{"home-5", "Choose: two beautiful houses", []string{"două case frumoase", "două case frumos", "doi case frumoase"}, "două case frumoase", "Case is feminine plural, so use două and frumoase."},
}

type Server struct{ DB *sql.DB }
type Attempt struct {
	ID         string `json:"id"`
	ExerciseID string `json:"exerciseId"`
	Answer     string `json:"answer"`
}
type Result struct {
	Correct     bool   `json:"correct"`
	Answer      string `json:"answer"`
	Explanation string `json:"explanation"`
}

var validID = regexp.MustCompile(`^[a-zA-Z0-9-]{16,80}$`)

func Migrate(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS attempts (
 id TEXT PRIMARY KEY, exercise_id TEXT NOT NULL, answer TEXT NOT NULL,
 correct BOOLEAN NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
 )`)
	return err
}

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
	mux.HandleFunc("GET /api/exercises", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, Exercises) })
	mux.HandleFunc("GET /api/progress", s.progress)
	mux.HandleFunc("POST /api/attempts", s.attempt)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { respond(w, 404, map[string]string{"error": "Not found"}) })
	return mux
}

func (s Server) progress(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var attempts, correct, skills int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE correct), count(DISTINCT exercise_id) FILTER (WHERE correct) FROM attempts`).Scan(&attempts, &correct, &skills)
	if err != nil {
		respond(w, 503, map[string]string{"error": "Could not load progress"})
		return
	}
	respond(w, 200, map[string]int{"attempts": attempts, "correct": correct, "practiced": skills})
}

func (s Server) attempt(w http.ResponseWriter, r *http.Request) {
	// This first release is intentionally a single-user, local development mode.
	// Reject browser cross-origin writes; no public authentication is implemented yet.
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
		respond(w, 403, map[string]string{"error": "Origin not allowed"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var a Attempt
	if dec.Decode(&a) != nil || !validID.MatchString(a.ID) {
		respond(w, 400, map[string]string{"error": "Invalid attempt"})
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		respond(w, 400, map[string]string{"error": "Invalid body"})
		return
	}
	var exercise *Exercise
	for i := range Exercises {
		if Exercises[i].ID == a.ExerciseID {
			exercise = &Exercises[i]
			break
		}
	}
	if exercise == nil {
		respond(w, 400, map[string]string{"error": "Unknown exercise"})
		return
	}
	found := false
	for _, option := range exercise.Options {
		if a.Answer == option {
			found = true
		}
	}
	if !found {
		respond(w, 400, map[string]string{"error": "Choose an available answer"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	correct := strings.TrimSpace(a.Answer) == exercise.Answer
	_, err := s.DB.ExecContext(ctx, `INSERT INTO attempts(id,exercise_id,answer,correct) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO NOTHING`, a.ID, a.ExerciseID, a.Answer, correct)
	if err != nil {
		respond(w, 503, map[string]string{"error": "Answer was not saved. Please retry."})
		return
	}
	var storedExercise, storedAnswer string
	err = s.DB.QueryRowContext(ctx, `SELECT exercise_id,answer,correct FROM attempts WHERE id=$1`, a.ID).Scan(&storedExercise, &storedAnswer, &correct)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		respond(w, 503, map[string]string{"error": "Could not confirm save"})
		return
	}
	if storedExercise != a.ExerciseID || storedAnswer != a.Answer {
		respond(w, 409, map[string]string{"error": "Attempt identifier already used"})
		return
	}
	respond(w, 200, Result{correct, exercise.Answer, exercise.Explanation})
}

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
