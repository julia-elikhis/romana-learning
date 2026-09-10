package app

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testAccount(t *testing.T, db *sql.DB, id string) *http.Cookie {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO app_users(id,login) VALUES($1,$1) ON CONFLICT DO NOTHING`, id); err != nil {
		t.Fatal(err)
	}
	token := randomToken()
	if _, err := db.Exec(`INSERT INTO auth_sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '1 hour')`, tokenHash(token), id); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "romana_session", Value: token}
}
func authenticatedHandler(t *testing.T, db *sql.DB, handler http.Handler) http.Handler {
	t.Helper()
	cookie := testAccount(t, db, "test-owner")
	if _, err := db.Exec(`UPDATE app_users SET is_admin=true WHERE id='test-owner'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE attempts SET user_id='test-owner' WHERE user_id='legacy-local-owner'`); err != nil {
		t.Fatal(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.AddCookie(cookie); handler.ServeHTTP(w, r) })
}

func TestAccountIsolationAndAnonymousPractice(t *testing.T) {
	db := isolatedDB(t)
	server := Server{DB: db}
	handler := server.Routes()
	alice := testAccount(t, db, "test-owner")
	bob := testAccount(t, db, "another-owner")
	id, questions := questionFixtures(t, db, 2, "published")
	request := func(cookie *http.Cookie, method, path string, body any, status int) []byte {
		t.Helper()
		wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cookie != nil {
				r.AddCookie(cookie)
			}
			handler.ServeHTTP(w, r)
		})
		return questionRequest(t, wrapped, method, path, body, status)
	}
	for _, path := range []string{"/api/library", "/api/history", "/api/materials/" + id, "/api/materials/" + id + "/original"} {
		request(nil, "GET", path, nil, 401)
	}
	for _, path := range []string{"/api/materials", "/api/materials/" + id + "/generate", "/api/materials/" + id + "/publish", "/api/drafts/" + questions[0].ID + "/status"} {
		request(nil, "POST", path, map[string]any{}, 401)
	}
	request(nil, "DELETE", "/api/exercises/"+questions[0].ID, nil, 401)
	for _, path := range []string{"/api/materials/" + id, "/api/materials/" + id + "/original"} {
		request(bob, "GET", path, nil, 403)
	}
	request(bob, "POST", "/api/materials/"+id+"/generate", map[string]any{}, 403)
	request(bob, "POST", "/api/materials/"+id+"/publish", map[string]any{}, 403)
	request(bob, "PATCH", "/api/drafts/"+questions[0].ID, map[string]any{}, 403)
	request(bob, "DELETE", "/api/exercises/"+questions[0].ID, nil, 403)
	if bytes.Contains(request(bob, "GET", "/api/library", nil, 403), []byte(id)) {
		t.Fatal("Another user's library was exposed")
	}
	request(nil, "GET", "/api/exercises?materialId="+id, nil, 200)
	payload := map[string]any{"id": "same-attempt-000001", "exerciseId": questions[0].ID, "answer": "sunt"}
	var before, after int
	db.QueryRow(`SELECT count(*) FROM attempts`).Scan(&before)
	var graded Result
	json.Unmarshal(request(nil, "POST", "/api/attempts", payload, 200), &graded)
	db.QueryRow(`SELECT count(*) FROM attempts`).Scan(&after)
	if !graded.Correct || graded.Saved || before != after {
		t.Fatal("Anonymous grading wrote history or failed to grade")
	}
	request(alice, "POST", "/api/attempts", payload, 200)
	payload["answer"] = "este"
	json.Unmarshal(request(bob, "POST", "/api/attempts", payload, 200), &graded)
	if graded.Correct || !graded.Saved {
		t.Fatal("Attempt id collided across users")
	}
	for _, cookie := range []*http.Cookie{alice, bob} {
		var history []HistoryItem
		json.Unmarshal(request(cookie, "GET", "/api/history", nil, 200), &history)
		if len(history) != 1 || history[0].Prompt != questions[0].Prompt {
			t.Fatal("History was not isolated or lacked question text")
		}
		if history[0].Correct != (cookie == alice) {
			t.Fatal("Another user's answer appeared in history")
		}
		var progress struct {
			Attempts int
			Tracked  bool
		}
		json.Unmarshal(request(cookie, "GET", "/api/progress", nil, 200), &progress)
		if progress.Attempts != 1 || !progress.Tracked {
			t.Fatal("Progress was not per-user")
		}
	}
	var guest struct {
		Attempts int
		Tracked  bool
	}
	json.Unmarshal(request(nil, "GET", "/api/progress", nil, 200), &guest)
	if guest.Attempts != 0 || guest.Tracked {
		t.Fatal("Guest progress exposed saved history")
	}
	request(alice, "POST", "/api/auth/logout", nil, 200)
	request(alice, "GET", "/api/history", nil, 401)
	request(bob, "GET", "/api/history", nil, 200)
	db.Exec(`UPDATE auth_sessions SET expires_at=now()-interval '1 minute' WHERE token_hash=$1`, tokenHash(bob.Value))
	request(bob, "GET", "/api/history", nil, 401)
}

func TestGitHubOAuthStatePKCEAndSessionLifecycle(t *testing.T) {
	db := isolatedDB(t)
	var expectedChallenge string
	exchanges := 0
	profileID := int64(901)
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			exchanges++
			r.ParseForm()
			challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.Form.Get("client_secret") != "private-client-secret" || base64.RawURLEncoding.EncodeToString(challenge[:]) != expectedChallenge {
				t.Error("Missing secret or PKCE verification")
			}
			json.NewEncoder(w).Encode(map[string]string{"access_token": "private-provider-token", "token_type": "bearer"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer private-provider-token" {
			t.Error("Profile identity was not verified with the access token")
		}
		json.NewEncoder(w).Encode(map[string]any{"id": profileID, "login": "github-learner", "name": "Learner"})
	}))
	defer provider.Close()
	base, _ := url.Parse("http://127.0.0.1")
	auth := &GitHubAuth{clientID: "client-id", clientSecret: "private-client-secret", baseURL: base, legacyGitHubID: 902, client: provider.Client(), tokenURL: provider.URL + "/token", profileURL: provider.URL + "/user"}
	auth.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	app := httptest.NewServer(Server{DB: db, Auth: auth}.Routes())
	defer app.Close()
	auth.baseURL, _ = url.Parse(app.URL)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	start := func() string {
		t.Helper()
		response, err := client.Get(app.URL + "/auth/github")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		location, _ := url.Parse(response.Header.Get("Location"))
		q := location.Query()
		expectedChallenge = q.Get("code_challenge")
		if location.Host != "github.com" || q.Get("code_challenge_method") != "S256" || q.Get("scope") != "" || len(expectedChallenge) != 43 {
			t.Fatal("Unsafe OAuth authorization request")
		}
		for _, cookie := range response.Cookies() {
			if cookie.Name == "romana_oauth_state" && (!cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode) {
				t.Fatal("OAuth cookie is not protected")
			}
		}
		return q.Get("state")
	}
	state := start()
	response, err := client.Get(app.URL + "/auth/github/callback?code=code&state=" + randomToken())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 400 || exchanges != 0 {
		t.Fatal("Mismatched OAuth state reached the token endpoint")
	}
	state = start()
	response, err = client.Get(app.URL + "/auth/github/callback?code=code&state=" + state)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 303 || response.Header.Get("Location") != app.URL+"/" || exchanges != 1 {
		t.Fatal("OAuth callback failed")
	}
	var sessionCookie *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == "romana_session" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteLaxMode || sessionCookie.MaxAge != 604800 {
		t.Fatal("Session cookie is missing or unsafe")
	}
	var storedToken, owner string
	db.QueryRow(`SELECT token_hash FROM auth_sessions`).Scan(&storedToken)
	if storedToken == sessionCookie.Value || storedToken != tokenHash(sessionCookie.Value) {
		t.Fatal("Session token was not hashed at rest")
	}
	db.QueryRow(`SELECT user_id FROM attempts WHERE id='legacy-attempt-01'`).Scan(&owner)
	if owner != "legacy-local-owner" {
		t.Fatal("First unrelated GitHub login claimed legacy data")
	}
	response, err = client.Get(app.URL + "/auth/github/callback?code=code&state=" + state)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 400 || exchanges != 1 {
		t.Fatal("OAuth state was reusable")
	}
	profileID = 902
	state = start()
	response, err = client.Get(app.URL + "/auth/github/callback?code=code&state=" + state)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	db.QueryRow(`SELECT user_id FROM attempts WHERE id='legacy-attempt-01'`).Scan(&owner)
	var githubID int64
	db.QueryRow(`SELECT github_id FROM app_users WHERE id=$1`, owner).Scan(&githubID)
	if githubID != 902 {
		t.Fatal("Verified legacy owner did not receive existing history")
	}
	var oldSessions int
	db.QueryRow(`SELECT count(*) FROM auth_sessions WHERE token_hash=$1`, storedToken).Scan(&oldSessions)
	if oldSessions != 0 {
		t.Fatal("Signing in again did not rotate the previous session")
	}
}

func TestAuthenticationConfigurationAndSecureCookies(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "id")
	t.Setenv("GITHUB_CLIENT_SECRET", "secret")
	for _, base := range []string{"http://public.invalid", "https://user:secret@public.invalid", "https://public.invalid/path", "https://public.invalid?token=secret"} {
		t.Setenv("APP_BASE_URL", base)
		if _, err := GitHubAuthFromEnv(); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("Unsafe origin accepted or echoed")
		}
	}
	t.Setenv("APP_BASE_URL", "https://learning.example")
	auth, err := GitHubAuthFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	Server{Auth: auth}.setCookie(w, "session", randomToken(), 600)
	cookie := w.Result().Cookies()[0]
	if cookie.Name != "__Host-romana_session" || !cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.Path != "/" {
		t.Fatal("HTTPS session cookie is not host-bound")
	}
}
