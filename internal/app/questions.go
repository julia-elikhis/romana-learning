package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"sort"

	"github.com/julia-elikhis/romana-learning/internal/materials"
)

// Each selected question carries the edits being reviewed in the browser.
type questionEdit struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Prompt      string   `json:"prompt"`
	Options     []string `json:"options"`
	Answers     []string `json:"answers"`
	Explanation string   `json:"explanation"`
}

func (s Server) bulkPublish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Exercises []questionEdit `json:"exercises"`
		Reviewed  bool           `json:"reviewed"`
	}
	if !decode(w, r, &body, 1<<20) {
		return
	}
	if !body.Reviewed || len(body.Exercises) == 0 || len(body.Exercises) > 100 {
		fail(w, 400, "Select 1–100 draft questions and confirm that you reviewed their answers")
		return
	}
	seen := map[string]bool{}
	for _, edit := range body.Exercises {
		if edit.ID == "" || len(edit.ID) > 80 || seen[edit.ID] {
			fail(w, 400, "Select each question only once")
			return
		}
		seen[edit.ID] = true
	}
	// Consistent lock order avoids deadlocks between overlapping bulk requests.
	sort.Slice(body.Exercises, func(i, j int) bool { return body.Exercises[i].ID < body.Exercises[j].ID })
	ctx, cancel := contextFor(r)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		fail(w, 503, "Could not publish selected questions")
		return
	}
	defer tx.Rollback()
	for _, edit := range body.Exercises {
		d, err := scanDraft(tx.QueryRowContext(ctx, `SELECT `+exerciseFields+` FROM exercises WHERE id=$1 AND material_id=$2 AND status<>'deleted' FOR UPDATE`, edit.ID, r.PathValue("id")))
		if err == sql.ErrNoRows {
			fail(w, 404, "A selected question no longer exists in this document. Nothing was published")
			return
		}
		if err != nil {
			fail(w, 503, "Could not load selected questions")
			return
		}
		if edit.Options == nil {
			edit.Options = []string{}
		}
		if d.Status == "published" && d.Kind == edit.Kind && d.Prompt == edit.Prompt &&
			slices.Equal(d.Options, edit.Options) && slices.Equal(d.Answers, edit.Answers) && d.Explanation == edit.Explanation {
			continue // A lost response can safely be retried without changing published questions.
		}
		if d.Status != "draft" {
			fail(w, 409, "A selected question has already been reviewed. Nothing was published")
			return
		}
		d.Kind, d.Prompt, d.Options, d.Answers, d.Explanation = edit.Kind, edit.Prompt, edit.Options, edit.Answers, edit.Explanation
		if err = materials.Validate(d); err != nil {
			fail(w, 400, "A selected question has invalid text or answers. Nothing was published: "+err.Error())
			return
		}
		options, _ := json.Marshal(d.Options)
		answers, _ := json.Marshal(d.Answers)
		if _, err = tx.ExecContext(ctx, `UPDATE exercises SET kind=$2,prompt=$3,options=$4,answers=$5,explanation=$6,status='published' WHERE id=$1`,
			d.ID, d.Kind, d.Prompt, string(options), string(answers), d.Explanation); err != nil {
			fail(w, 503, "Could not publish selected questions")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(w, 503, "Could not confirm publication. Retry to check the selected questions")
		return
	}
	respond(w, 200, map[string]int{"count": len(body.Exercises)})
}

func (s Server) deleteExercise(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextFor(r)
	defer cancel()
	// Retain the historical record and saved grading snapshots, but exclude the
	// question from both the course library and every future practice session.
	result, err := s.DB.ExecContext(ctx, `UPDATE exercises SET status='deleted' WHERE id=$1`, r.PathValue("id"))
	if err != nil {
		fail(w, 503, "Could not delete the question")
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		fail(w, 503, "Could not confirm deletion")
		return
	}
	if count == 0 {
		fail(w, 404, "Question not found")
		return
	}
	respond(w, 200, map[string]bool{"deleted": true})
}
