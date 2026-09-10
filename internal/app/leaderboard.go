package app

import (
	"context"
	"net/http"
	"time"
)

type LeaderboardEntry struct {
	Rank  int    `json:"rank"`
	Login string `json:"login"`
	Score int    `json:"score"`
	IsYou bool   `json:"isYou"`
}

type Leaderboard struct {
	Entries      []LeaderboardEntry `json:"entries"`
	You          *LeaderboardEntry  `json:"you"`
	Participants int                `json:"participants"`
	WeekStart    time.Time          `json:"weekStart"`
	WeekEnd      time.Time          `json:"weekEnd"`
}

func leaderboardWeek(now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start = start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
	return start, start.AddDate(0, 0, 7)
}

func (s Server) leaderboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	start, end := leaderboardWeek(time.Now())
	board := Leaderboard{Entries: []LeaderboardEntry{}, WeekStart: start, WeekEnd: end}
	viewer := ""
	if user := currentUser(r); user != nil {
		viewer = user.ID
	}
	// Saved grading snapshots retain earned credit after question edits or
	// deletion. Repeat answers cannot inflate the score. Unverified legacy and
	// test identities are excluded; no personal answer history leaves this query.
	rows, err := s.DB.QueryContext(ctx, `WITH scores AS (
 SELECT u.id,u.login,u.github_id,count(DISTINCT a.exercise_id) AS score
 FROM attempts a JOIN app_users u ON u.id=a.user_id
 WHERE a.correct AND a.created_at >= $1 AND a.created_at < $2 AND u.github_id IS NOT NULL
 GROUP BY u.id,u.login,u.github_id
), ranked AS (
 SELECT id,login,score,rank() OVER (ORDER BY score DESC) AS rank,
 row_number() OVER (ORDER BY score DESC,lower(login),github_id) AS position,
 count(*) OVER () AS participants FROM scores
)
SELECT id,login,score,rank,position,participants FROM ranked
WHERE position<=5 OR id=$3 ORDER BY position`, start, end, viewer)
	if err != nil {
		fail(w, 503, "Could not load the leaderboard")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var entry LeaderboardEntry
		var id string
		var position int
		if rows.Scan(&id, &entry.Login, &entry.Score, &entry.Rank, &position, &board.Participants) != nil {
			fail(w, 503, "Could not read the leaderboard")
			return
		}
		entry.IsYou = viewer != "" && viewer == id
		if position <= 5 {
			board.Entries = append(board.Entries, entry)
		}
		if entry.IsYou {
			board.You = &entry
		}
	}
	if rows.Err() != nil {
		fail(w, 503, "Could not read the leaderboard")
		return
	}
	respond(w, 200, board)
}
