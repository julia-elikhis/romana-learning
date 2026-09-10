package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type User struct {
	ID      string `json:"id"`
	Login   string `json:"login"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"isAdmin"`
}
type userContextKey struct{}

func currentUser(r *http.Request) *User {
	u, _ := r.Context().Value(userContextKey{}).(*User)
	return u
}

type GitHubAuth struct {
	clientID, clientSecret string
	baseURL                *url.URL
	legacyGitHubID         int64
	client                 *http.Client
	tokenURL, profileURL   string
}

func (a *GitHubAuth) PublicReady() bool { return a != nil && a.baseURL.Scheme == "https" }

func GitHubAuthFromEnv() (*GitHubAuth, error) {
	id, secret := os.Getenv("GITHUB_CLIENT_ID"), os.Getenv("GITHUB_CLIENT_SECRET")
	if id == "" && secret == "" {
		return nil, nil
	}
	if id == "" || secret == "" || strings.ContainsAny(id+secret, "\r\n") {
		return nil, errors.New("Configure both GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET")
	}
	base := os.Getenv("APP_BASE_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("APP_BASE_URL must be the application's public HTTP(S) origin")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return nil, errors.New("APP_BASE_URL requires HTTPS except on localhost")
	}
	u.Path = ""
	var legacyID int64
	if value := os.Getenv("AUTH_LEGACY_GITHUB_ID"); value != "" {
		legacyID, err = strconv.ParseInt(value, 10, 64)
		if err != nil || legacyID <= 0 {
			return nil, errors.New("AUTH_LEGACY_GITHUB_ID must be a numeric GitHub account ID")
		}
	}
	return &GitHubAuth{clientID: id, clientSecret: secret, baseURL: u, legacyGitHubID: legacyID,
		client:   &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		tokenURL: "https://github.com/login/oauth/access_token", profileURL: "https://api.github.com/user"}, nil
}

var validToken = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (s Server) secureCookies() bool { return s.Auth != nil && s.Auth.baseURL.Scheme == "https" }
func (s Server) cookieName(name string) string {
	if s.secureCookies() {
		return "__Host-romana_" + name
	}
	return "romana_" + name
}
func (s Server) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	c := &http.Cookie{Name: s.cookieName(name), Value: value, Path: "/", HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode, MaxAge: maxAge}
	if maxAge < 0 {
		c.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, c)
}

func (s Server) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.cookieName("session"))
		if err == nil && validToken.MatchString(cookie.Value) {
			ctx, cancel := contextFor(r)
			defer cancel()
			var user User
			err = s.DB.QueryRowContext(ctx, `SELECT u.id,u.login,u.name,u.is_admin FROM auth_sessions a JOIN app_users u ON u.id=a.user_id WHERE a.token_hash=$1 AND a.expires_at>now()`, tokenHash(cookie.Value)).Scan(&user.ID, &user.Login, &user.Name, &user.IsAdmin)
			if err == nil {
				r = r.WithContext(context.WithValue(r.Context(), userContextKey{}, &user))
			} else if err == sql.ErrNoRows {
				s.setCookie(w, "session", "", -1)
			} else {
				fail(w, 503, "Could not verify your session. Please retry")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r) == nil {
			fail(w, 401, "Sign in with GitHub to continue")
			return
		}
		next(w, r)
	}
}
func (s Server) authSession(w http.ResponseWriter, r *http.Request) {
	respond(w, 200, map[string]any{"user": currentUser(r), "githubEnabled": s.Auth != nil})
}
func (s Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.cookieName("session")); err == nil {
		ctx, cancel := contextFor(r)
		defer cancel()
		if _, err = s.DB.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash=$1`, tokenHash(c.Value)); err != nil {
			fail(w, 503, "Could not sign out. Please retry")
			return
		}
	}
	s.setCookie(w, "session", "", -1)
	respond(w, 200, map[string]bool{"signedOut": true})
}
func (s Server) githubLogin(w http.ResponseWriter, r *http.Request) {
	a := s.Auth
	if a == nil {
		fail(w, 503, "GitHub sign-in is not configured on this server")
		return
	}
	if r.Host != a.baseURL.Host {
		http.Redirect(w, r, a.baseURL.String()+"/auth/github", http.StatusSeeOther)
		return
	}
	state, verifier := randomToken(), randomToken()
	ctx, cancel := contextFor(r)
	defer cancel()
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM oauth_states WHERE expires_at<=now()`); err != nil {
		fail(w, 503, "Could not start sign-in")
		return
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO oauth_states(state_hash,verifier,expires_at) VALUES($1,$2,now()+interval '10 minutes')`, tokenHash(state), verifier); err != nil {
		fail(w, 503, "Could not start sign-in")
		return
	}
	s.setCookie(w, "oauth_state", state, 600)
	challenge := sha256.Sum256([]byte(verifier))
	values := url.Values{"client_id": {a.clientID}, "redirect_uri": {a.baseURL.String() + "/auth/github/callback"}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+values.Encode(), http.StatusSeeOther)
}
func readAuthJSON(response *http.Response, out any) error {
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("provider rejected sign-in")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		return errors.New("invalid provider response")
	}
	return json.Unmarshal(data, out)
}
func (s Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	a := s.Auth
	if a == nil {
		fail(w, 503, "GitHub sign-in is not configured")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie(s.cookieName("oauth_state"))
	s.setCookie(w, "oauth_state", "", -1)
	if err != nil || !validToken.MatchString(state) || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		fail(w, 400, "Sign-in expired or could not be verified. Please try again")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	var verifier string
	err = s.DB.QueryRowContext(ctx, `DELETE FROM oauth_states WHERE state_hash=$1 AND expires_at>now() RETURNING verifier`, tokenHash(state)).Scan(&verifier)
	if err != nil {
		fail(w, 400, "Sign-in expired or was already used. Please try again")
		return
	}
	failed := func() { http.Redirect(w, r, a.baseURL.String()+"/?authError=github", http.StatusSeeOther) }
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 2048 || r.URL.Query().Get("error") != "" {
		failed()
		return
	}
	values := url.Values{"client_id": {a.clientID}, "client_secret": {a.clientSecret}, "code": {code}, "redirect_uri": {a.baseURL.String() + "/auth/github/callback"}, "code_verifier": {verifier}}
	req, err := http.NewRequestWithContext(ctx, "POST", a.tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		failed()
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	response, err := a.client.Do(req)
	if err != nil {
		failed()
		return
	}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if readAuthJSON(response, &token) != nil || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "bearer") || strings.ContainsAny(token.AccessToken, "\r\n") {
		failed()
		return
	}
	req, err = http.NewRequestWithContext(ctx, "GET", a.profileURL, nil)
	if err != nil {
		failed()
		return
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err = a.client.Do(req)
	if err != nil {
		failed()
		return
	}
	var profile struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if readAuthJSON(response, &profile) != nil || profile.ID <= 0 || profile.Login == "" || len(profile.Login) > 100 || len(profile.Name) > 300 {
		failed()
		return
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		failed()
		return
	}
	defer tx.Rollback()
	var userID string
	err = tx.QueryRowContext(ctx, `INSERT INTO app_users(id,github_id,login,name) VALUES($1,$2,$3,$4) ON CONFLICT(github_id) DO UPDATE SET login=EXCLUDED.login,name=EXCLUDED.name RETURNING id`, newID(), profile.ID, profile.Login, profile.Name).Scan(&userID)
	if err != nil {
		failed()
		return
	}
	if a.legacyGitHubID == profile.ID {
		if _, err = tx.ExecContext(ctx, `UPDATE materials SET owner_id=$1 WHERE owner_id='legacy-local-owner'`, userID); err != nil {
			failed()
			return
		}
		if _, err = tx.ExecContext(ctx, `UPDATE attempts SET user_id=$1 WHERE user_id='legacy-local-owner'`, userID); err != nil {
			failed()
			return
		}
	}
	if old, err := r.Cookie(s.cookieName("session")); err == nil {
		if _, err = tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash=$1`, tokenHash(old.Value)); err != nil {
			failed()
			return
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at<=now()`); err != nil {
		failed()
		return
	}
	session := randomToken()
	if _, err = tx.ExecContext(ctx, `INSERT INTO auth_sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '7 days')`, tokenHash(session), userID); err != nil {
		failed()
		return
	}
	if err = tx.Commit(); err != nil {
		failed()
		return
	}
	s.setCookie(w, "session", session, 7*24*60*60)
	http.Redirect(w, r, a.baseURL.String()+"/", http.StatusSeeOther)
}
