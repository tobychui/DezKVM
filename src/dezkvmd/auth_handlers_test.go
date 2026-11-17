package main

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"imuslab.com/dezkvm/dezkvmd/mod/auth"
	"imuslab.com/dezkvm/dezkvmd/mod/db"
)

// TestRoutingRejectsForgedCookie runs the real root router and auth APIs
// over HTTPS: the old constant cookie value must not open anything, a
// login must, and a logout must close it again.
func TestRoutingRejectsForgedCookie(t *testing.T) {
	d, err := db.NewDB(filepath.Join(t.TempDir(), "sys.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	authManager, err = auth.NewAuthManager(auth.Options{DB: d})
	if err != nil {
		t.Fatal(err)
	}
	if err := authManager.SetPassword("correct horse"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	handle_root_routing(mux)
	register_auth_apis(mux)
	authManager.HandleFunc("/api/v1/secret", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secret"))
	}, mux)
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	noRedirect := func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	client := srv.Client()
	client.CheckRedirect = noRedirect

	get := func(c *http.Client, path, cookie string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}

	// Forged cookie: pages redirect to the login, APIs answer 401
	if resp := get(client, "/index.html", "dezkvm_auth=1"); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login.html" {
		t.Fatalf("forged cookie on a page: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp := get(client, "/api/v1/secret", "dezkvm_auth=1"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forged cookie on an API: %d", resp.StatusCode)
	}

	// Real login through the API, with a cookie jar like a browser
	jar, _ := cookiejar.New(nil)
	browser := srv.Client()
	browser.Jar = jar
	browser.CheckRedirect = noRedirect
	post := func(path, body string) *http.Response {
		resp, err := browser.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	if resp := post("/api/v1/login", `{"password":"wrong"}`); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password login: %d", resp.StatusCode)
	}
	resp := post("/api/v1/login", `{"password":"correct horse"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			session = c
		}
	}
	if session == nil || !session.Secure || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie flags: %+v", session)
	}
	if resp := get(browser, "/api/v1/secret", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("API after login: %d", resp.StatusCode)
	}
	if resp := get(browser, "/index.html", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("page after login: %d", resp.StatusCode)
	}

	// Logout ends the session server side: replaying the old token fails
	if resp := post("/api/v1/logout", ``); resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	u, _ := url.Parse(srv.URL)
	if len(jar.Cookies(u)) != 0 {
		t.Error("cookie not cleared by logout")
	}
	if resp := get(client, "/api/v1/secret", auth.CookieName+"="+session.Value); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replayed token after logout: %d", resp.StatusCode)
	}
}
