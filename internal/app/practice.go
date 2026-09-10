package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

// practiceQuestion chooses from the entire published pool, preferring questions
// this learner has never attempted. A shuffle avoids the current question within
// that priority group, but a sole unanswered question still takes precedence.
func (s Server) practiceQuestion(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	materialID, currentID := query.Get("materialId"), query.Get("currentId")
	answered := query["answeredId"]
	if len(materialID) > 80 || len(currentID) > 80 || len(answered) > 200 || strings.ContainsRune(materialID+currentID, 0) {
		fail(w, 400, "Invalid practice selection")
		return
	}
	for _, id := range answered {
		if id == "" || len(id) > 80 || strings.ContainsRune(id, 0) {
			fail(w, 400, "Invalid practice selection")
			return
		}
	}
	viewer := ""
	if user := currentUser(r); user != nil {
		viewer = user.ID
		answered = nil // Authenticated history comes exclusively from saved attempts.
	}
	if answered == nil {
		answered = []string{}
	}
	guestAnswers, _ := json.Marshal(answered)
	ctx, cancel := contextFor(r)
	defer cancel()
	var exercise PracticeExercise
	var options []byte
	err := s.DB.QueryRowContext(ctx, `SELECT e.id,e.kind,e.prompt,e.options
 FROM exercises e WHERE e.status='published' AND ($2='' OR e.material_id=$2)
 ORDER BY CASE WHEN $1='' THEN e.id IN (SELECT jsonb_array_elements_text($4::jsonb))
 ELSE EXISTS(SELECT 1 FROM attempts a WHERE a.user_id=$1 AND a.exercise_id=e.id) END,
 e.id=$3,random() LIMIT 1`, viewer, materialID, currentID, string(guestAnswers)).Scan(&exercise.ID, &exercise.Kind, &exercise.Prompt, &options)
	if err == sql.ErrNoRows {
		respond(w, 200, nil)
		return
	}
	if err != nil || json.Unmarshal(options, &exercise.Options) != nil {
		fail(w, 503, "Could not load a practice question")
		return
	}
	respond(w, 200, exercise)
}
