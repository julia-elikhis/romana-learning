package app

import (
	"net/http"
	"time"
)

type HistoryItem struct {
	ID            string    `json:"id"`
	ExerciseID    string    `json:"exerciseId"`
	Prompt        string    `json:"prompt"`
	Answer        string    `json:"answer"`
	Correct       bool      `json:"correct"`
	CorrectAnswer string    `json:"correctAnswer"`
	Explanation   string    `json:"explanation"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (s Server) history(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextFor(r)
	defer cancel()
	rows, err := s.DB.QueryContext(ctx, `SELECT id,exercise_id,question_prompt,answer,correct,correct_answer,explanation,created_at FROM attempts WHERE user_id=$1 ORDER BY created_at DESC,id DESC LIMIT 100`, currentUser(r).ID)
	if err != nil {
		fail(w, 503, "Could not load your history")
		return
	}
	defer rows.Close()
	items := []HistoryItem{}
	for rows.Next() {
		var item HistoryItem
		if err = rows.Scan(&item.ID, &item.ExerciseID, &item.Prompt, &item.Answer, &item.Correct, &item.CorrectAnswer, &item.Explanation, &item.CreatedAt); err != nil {
			fail(w, 503, "Could not load your history")
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		fail(w, 503, "Could not load your history")
		return
	}
	respond(w, 200, items)
}
