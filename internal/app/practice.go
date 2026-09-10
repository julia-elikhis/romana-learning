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
	recent := query["recentId"]
	if len(materialID) > 80 || len(currentID) > 80 || len(answered) > 200 || len(recent) > 8 || strings.ContainsRune(materialID+currentID, 0) {
		fail(w, 400, "Invalid practice selection")
		return
	}
	for _, id := range append(append([]string{}, answered...), recent...) {
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
	recentJSON, _ := json.Marshal(nonNilStrings(recent))
	ctx, cancel := contextFor(r)
	defer cancel()
	var exercise PracticeExercise
	var options []byte
	err := s.DB.QueryRowContext(ctx, `WITH shown AS (
 SELECT e.kind,e.skill,e.target,j.ordinality AS position
 FROM jsonb_array_elements_text($5::jsonb) WITH ORDINALITY j(id,ordinality)
 JOIN exercises e ON e.id=j.id
), saved AS (
 SELECT question_kind AS kind,question_skill AS skill,question_target AS target,
 row_number() OVER(ORDER BY created_at DESC,id DESC) AS position
 FROM attempts WHERE user_id=$1 AND question_kind<>'' ORDER BY created_at DESC,id DESC LIMIT 8
), recent AS (
 SELECT * FROM shown UNION ALL SELECT * FROM saved WHERE NOT EXISTS(SELECT 1 FROM shown)
)
 SELECT e.id,e.kind,e.prompt,e.options
 FROM exercises e WHERE e.status='published' AND ($2='' OR e.material_id=$2)
 ORDER BY CASE WHEN $1='' THEN e.id IN (SELECT jsonb_array_elements_text($4::jsonb))
 ELSE EXISTS(SELECT 1 FROM attempts a WHERE a.user_id=$1 AND a.exercise_id=e.id) END,
 e.id=$3,
 (SELECT count(*)=2 AND bool_and(kind=e.kind) FROM (SELECT kind FROM recent ORDER BY position LIMIT 2) last_two),
 COALESCE(e.target<>'' AND e.target=(SELECT target FROM recent ORDER BY position LIMIT 1),false),
 (SELECT count(*) FROM recent WHERE kind=e.kind)::float / CASE e.kind WHEN 'multiple_choice' THEN 5 WHEN 'cloze' THEN 4 ELSE 1 END,
 (SELECT count(*) FROM recent WHERE skill=e.skill AND e.skill<>''),random() LIMIT 1`, viewer, materialID, currentID, string(guestAnswers), string(recentJSON)).Scan(&exercise.ID, &exercise.Kind, &exercise.Prompt, &options)
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
