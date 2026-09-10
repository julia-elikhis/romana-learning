package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

func requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return requireUser(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin {
			fail(w, http.StatusForbidden, "Administrator access is required")
			return
		}
		next(w, r)
	})
}

// GrantAdmin is an operator-only bootstrap command. The account must have
// signed in through GitHub; a mutable login name is never used as identity.
func GrantAdmin(ctx context.Context, db *sql.DB, githubID int64) error {
	if githubID <= 0 {
		return errors.New("Provide a positive GitHub account ID")
	}
	result, err := db.ExecContext(ctx, `UPDATE app_users SET is_admin=true WHERE github_id=$1`, githubID)
	if err != nil {
		return errors.New("Could not grant administrator access")
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return errors.New("GitHub account not found; sign in to the app first")
	}
	return nil
}

func (s Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextFor(r)
	defer cancel()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 200 {
		fail(w, 400, "Search must be under 200 characters")
		return
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,login,name,is_admin FROM app_users WHERE id<>'legacy-local-owner' AND (strpos(lower(login),lower($1))>0 OR strpos(lower(name),lower($1))>0) ORDER BY is_admin DESC,lower(login),id LIMIT 100`, q)
	if err != nil {
		fail(w, 503, "Could not load users")
		return
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var user User
		if rows.Scan(&user.ID, &user.Login, &user.Name, &user.IsAdmin) != nil {
			fail(w, 503, "Could not read users")
			return
		}
		users = append(users, user)
	}
	if rows.Err() != nil {
		fail(w, 503, "Could not read users")
		return
	}
	respond(w, 200, users)
}

func (s Server) setAdmin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IsAdmin *bool `json:"isAdmin"`
	}
	if !decode(w, r, &body, 1024) {
		return
	}
	if body.IsAdmin == nil {
		fail(w, 400, "Choose an administrator role")
		return
	}
	actor := currentUser(r)
	if r.PathValue("id") == actor.ID && !*body.IsAdmin {
		fail(w, 409, "Another administrator must remove your admin role")
		return
	}
	ctx, cancel := contextFor(r)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		fail(w, 503, "Could not update role")
		return
	}
	defer tx.Rollback()
	// Serialize role changes and recheck the actor so concurrent demotions
	// cannot remove every admin or reuse privileges that were just revoked.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(726662033)`); err != nil {
		fail(w, 503, "Could not update role")
		return
	}
	var allowed bool
	if err = tx.QueryRowContext(ctx, `SELECT is_admin FROM app_users WHERE id=$1`, actor.ID).Scan(&allowed); err != nil {
		fail(w, 503, "Could not verify administrator access")
		return
	}
	if !allowed {
		fail(w, 403, "Administrator access is required")
		return
	}
	var user User
	err = tx.QueryRowContext(ctx, `UPDATE app_users SET is_admin=$2 WHERE id=$1 AND id<>'legacy-local-owner' RETURNING id,login,name,is_admin`, r.PathValue("id"), *body.IsAdmin).Scan(&user.ID, &user.Login, &user.Name, &user.IsAdmin)
	if err == sql.ErrNoRows {
		fail(w, 404, "User not found")
		return
	}
	if err != nil {
		fail(w, 503, "Could not update role")
		return
	}
	if tx.Commit() != nil {
		fail(w, 503, "Could not confirm role change")
		return
	}
	respond(w, 200, user)
}
