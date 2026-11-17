package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"imuslab.com/dezkvm/dezkvmd/mod/db"
)

func openDB(t *testing.T, path string) *db.DB {
	t.Helper()
	d, err := db.NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func newManager(t *testing.T, d *db.DB) *AuthManager {
	t.Helper()
	a, err := NewAuthManager(Options{DB: d})
	if err != nil {
		t.Fatal(err)
	}
	a.failureDelay = 0
	return a
}

// login posts the password and returns the session cookie (nil on failure)
func login(t *testing.T, a *AuthManager, password string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(`{"password":"`+password+`"}`))
	rec := httptest.NewRecorder()
	if err := a.LoginUser(rec, req); err != nil {
		return nil
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == CookieName {
			return c
		}
	}
	t.Fatal("login succeeded without a session cookie")
	return nil
}

func requestWith(cookie *http.Cookie) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func TestForgedCookieRejected(t *testing.T) {
	a := newManager(t, openDB(t, filepath.Join(t.TempDir(), "sys.db")))
	if err := a.SetPassword("hunter2"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"1", "true", "", strings.Repeat("A", 43)} {
		if a.UserIsLoggedIn(requestWith(&http.Cookie{Name: CookieName, Value: value})) {
			t.Errorf("forged cookie %q accepted", value)
		}
	}

	// The wrapped handlers answer 401 to it
	mux := http.NewServeMux()
	a.HandleFunc("/api/v1/instances", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("secret")) }, mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, requestWith(&http.Cookie{Name: CookieName, Value: "1"}))
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("forged cookie got %d %q", rec.Code, rec.Body.String())
	}
}

func TestLoginLogout(t *testing.T) {
	a := newManager(t, openDB(t, filepath.Join(t.TempDir(), "sys.db")))
	if err := a.SetPassword("hunter2"); err != nil {
		t.Fatal(err)
	}
	if login(t, a, "wrong") != nil {
		t.Fatal("wrong password logged in")
	}
	cookie := login(t, a, "hunter2")
	if cookie == nil {
		t.Fatal("correct password rejected")
	}
	if len(cookie.Value) < 40 || cookie.Value == "1" {
		t.Errorf("weak session token %q", cookie.Value)
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != int(SessionTTL.Seconds()) {
		t.Errorf("cookie flags: %+v", cookie)
	}
	if !a.UserIsLoggedIn(requestWith(cookie)) {
		t.Fatal("fresh session not accepted")
	}

	// A second login gets a different token
	if other := login(t, a, "hunter2"); other == nil || other.Value == cookie.Value {
		t.Fatal("tokens must be unique per login")
	}

	rec := httptest.NewRecorder()
	a.LogoutUser(rec, requestWith(cookie))
	if a.UserIsLoggedIn(requestWith(cookie)) {
		t.Fatal("session still valid after logout")
	}
	cleared := rec.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Errorf("logout did not clear the cookie: %+v", cleared)
	}
}

func TestSecureCookieOverTLS(t *testing.T) {
	a := newManager(t, openDB(t, filepath.Join(t.TempDir(), "sys.db")))
	a.SetPassword("pw")
	req := httptest.NewRequest(http.MethodPost, "https://kvm/api/v1/login", strings.NewReader(`{"password":"pw"}`))
	rec := httptest.NewRecorder()
	if err := a.LoginUser(rec, req); err != nil {
		t.Fatal(err)
	}
	if c := rec.Result().Cookies(); len(c) != 1 || !c[0].Secure {
		t.Fatalf("cookie over HTTPS must be Secure: %+v", c)
	}
}

func TestSessionExpiry(t *testing.T) {
	a := newManager(t, openDB(t, filepath.Join(t.TempDir(), "sys.db")))
	a.SetPassword("pw")
	now := time.Now()
	a.now = func() time.Time { return now }
	cookie := login(t, a, "pw")

	now = now.Add(SessionTTL - time.Minute)
	if !a.UserIsLoggedIn(requestWith(cookie)) {
		t.Fatal("session expired early")
	}
	now = now.Add(2 * time.Minute)
	if a.UserIsLoggedIn(requestWith(cookie)) {
		t.Fatal("expired session accepted")
	}
}

func TestSessionsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sys.db")
	d := openDB(t, path)
	a := newManager(t, d)
	a.SetPassword("pw")
	cookie := login(t, a, "pw")
	d.Close()

	d = openDB(t, path)
	defer d.Close()
	b := newManager(t, d)
	if !b.UserIsLoggedIn(requestWith(cookie)) {
		t.Fatal("session lost across a restart")
	}
	// The database holds a hash of the token, never the token itself
	d.List(sessionBucket, func(key, value []byte) error {
		if strings.Contains(string(key), cookie.Value) || strings.Contains(string(value), cookie.Value) {
			t.Error("raw session token stored in the database")
		}
		return nil
	})
}

func TestLegacyPasswordMigrated(t *testing.T) {
	d := openDB(t, filepath.Join(t.TempDir(), "sys.db"))
	defer d.Close()
	d.NewBucket(authBucket)
	d.Write(authBucket, legacyPassKey, []byte("old-plain"))

	a := newManager(t, d)
	if v, _ := d.Read(authBucket, legacyPassKey); v != nil {
		t.Fatal("plaintext password still stored after start")
	}
	hash, _ := d.Read(authBucket, passHashKey)
	if hash == nil || strings.Contains(string(hash), "old-plain") {
		t.Fatalf("no proper hash stored: %q", hash)
	}
	if ok, _ := a.ValidatePassword("old-plain"); !ok {
		t.Fatal("migrated password no longer accepted")
	}
	if ok, _ := a.ValidatePassword("other"); ok {
		t.Fatal("wrong password accepted after migration")
	}
}

func TestPasswordChangeRevokesSessions(t *testing.T) {
	a := newManager(t, openDB(t, filepath.Join(t.TempDir(), "sys.db")))
	a.SetPassword("first")
	cookie := login(t, a, "first")

	if err := a.ChangePassword("nope", "second"); err == nil {
		t.Fatal("password changed with a wrong old password")
	}
	if err := a.ChangePassword("first", "second"); err != nil {
		t.Fatal(err)
	}
	if a.UserIsLoggedIn(requestWith(cookie)) {
		t.Fatal("old session survived a password change")
	}
	if login(t, a, "first") != nil || login(t, a, "second") == nil {
		t.Fatal("password change not applied")
	}

	if err := a.SetPassword(strings.Repeat("x", 73)); err != ErrPasswordTooLong {
		t.Fatalf("expected ErrPasswordTooLong, got %v", err)
	}
	if err := a.SetPassword(""); err == nil {
		t.Fatal("empty password accepted")
	}
}

func TestNoPasswordSet(t *testing.T) {
	a := newManager(t, openDB(t, filepath.Join(t.TempDir(), "sys.db")))
	if ok, _ := a.ValidatePassword(""); ok {
		t.Fatal("empty password accepted with no password set")
	}
	if login(t, a, "anything") != nil {
		t.Fatal("login without a configured password")
	}
}

func TestSessionCountBounded(t *testing.T) {
	a := newManager(t, openDB(t, filepath.Join(t.TempDir(), "sys.db")))
	for i := 0; i < maxSessions+10; i++ {
		if _, _, err := a.createSession(); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.sessions) > maxSessions {
		t.Fatalf("%d sessions kept, limit %d", len(a.sessions), maxSessions)
	}
}
