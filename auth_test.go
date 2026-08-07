package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// authTestHandler returns a recorder + a middleware-wrapped mux with auth env
// set (or cleared when creds are empty) and HOME pointed at a temp dir so the
// session key never touches the real ~/.hourglass. The mux registers the real
// auth endpoints plus a dummy handler for everything else.
func authTestHandler(t *testing.T, user, pass string) (*httptest.ResponseRecorder, http.Handler) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if user == "" && pass == "" {
		os.Unsetenv("HOURGLASS_AUTH_USER")
		os.Unsetenv("HOURGLASS_AUTH_PASS")
	} else {
		t.Setenv("HOURGLASS_AUTH_USER", user)
		t.Setenv("HOURGLASS_AUTH_PASS", pass)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/login", handleAuthLogin)
	mux.HandleFunc("/api/auth/me", handleAuthMe)
	mux.HandleFunc("/api/auth/logout", handleAuthLogout)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	return httptest.NewRecorder(), authMiddleware(mux)
}

func doAuthRequest(h http.Handler, method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthMiddlewareDisabledPassesEverything(t *testing.T) {
	_, h := authTestHandler(t, "", "")
	rec := doAuthRequest(h, http.MethodGet, "/api/cron", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("no-auth passthrough broken: code=%d", rec.Code)
	}
}

func TestAuthMiddlewarePublicRoutes(t *testing.T) {
	_, h := authTestHandler(t, "admin", "s3cret")

	// HTML shell, static assets, and /api/version stay reachable without
	// credentials so the frontend can render the login view (no native
	// browser prompt). /ca.pem serves the public root CA certificate so
	// other LAN devices can fetch and trust it without logging in.
	for _, path := range []string{"/", "/dist/app.css", "/api/version", "/ca.pem"} {
		rec := doAuthRequest(h, http.MethodGet, path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("public route %s should be reachable without auth, got %d", path, rec.Code)
		}
	}

	// The auth endpoints themselves are NOT middleware-blocked: /api/auth/me
	// answers 401 from its own handler (that's how the frontend knows to show
	// the login view), and login with valid credentials works with no prior
	// session.
	rec := doAuthRequest(h, http.MethodGet, "/api/auth/me", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected /api/auth/me to answer 401 when unauthenticated, got %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"admin","password":"s3cret"}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Result().Cookies() == nil {
		t.Fatalf("expected login to succeed without a prior session, got %d", rec.Code)
	}
}

func TestAuthMiddlewareRequiresCredentials(t *testing.T) {
	_, h := authTestHandler(t, "admin", "s3cret")

	// No cookie → 401 JSON, no WWW-Authenticate challenge (the whole point:
	// kill the native Basic Auth dialog).
	rec := doAuthRequest(h, http.MethodGet, "/api/cron", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without creds, got %d", rec.Code)
	}
	if hdr := rec.Header().Get("WWW-Authenticate"); hdr != "" {
		t.Fatalf("expected no WWW-Authenticate challenge, got %q", hdr)
	}

	// Basic Auth still accepted as a fallback for scripts.
	req := httptest.NewRequest(http.MethodGet, "/api/cron", nil)
	req.SetBasicAuth("admin", "s3cret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with basic auth fallback, got %d", rec.Code)
	}

	// Wrong basic creds → 401.
	req = httptest.NewRequest(http.MethodGet, "/api/cron", nil)
	req.SetBasicAuth("admin", "wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong basic creds, got %d", rec.Code)
	}
}

func TestAuthLoginFlow(t *testing.T) {
	_, h := authTestHandler(t, "admin", "s3cret")

	// Login with wrong password → 401 and no cookie.
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"admin","password":"wrong"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on bad password, got %d", rec.Code)
	}
	if cookies := rec.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("no cookie should be set on failed login, got %v", cookies)
	}

	// Login with correct creds → 200 + session cookie.
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"admin","password":"s3cret"}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on login, got %d", rec.Code)
	}
	var login authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &login); err != nil || !login.OK || login.User != "admin" {
		t.Fatalf("bad login response: %s (err=%v)", rec.Body.String(), err)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatal("expected hg_session cookie on login")
	}

	// /api/auth/me with the cookie → {user: admin}.
	rec = doAuthRequest(h, http.MethodGet, "/api/auth/me", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/auth/me with session, got %d", rec.Code)
	}
	var me authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || !me.OK || me.User != "admin" {
		t.Fatalf("bad me response: %s (err=%v)", rec.Body.String(), err)
	}

	// API access with the session cookie → 200.
	rec = doAuthRequest(h, http.MethodGet, "/api/cron", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/cron with session, got %d", rec.Code)
	}

	// Logout clears the session → API returns 401 again.
	rec = doAuthRequest(h, http.MethodPost, "/api/auth/logout", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on logout, got %d", rec.Code)
	}
	rec = doAuthRequest(h, http.MethodGet, "/api/auth/me", cookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for /api/auth/me after logout, got %d", rec.Code)
	}
}

func TestAuthMeWhenDisabled(t *testing.T) {
	_, h := authTestHandler(t, "", "")
	rec := doAuthRequest(h, http.MethodGet, "/api/auth/me", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/auth/me when disabled, got %d", rec.Code)
	}
	var me authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || !me.OK || me.User != "" {
		t.Fatalf("expected {ok:true,user:\"\"} when auth disabled, got %s (err=%v)", rec.Body.String(), err)
	}
}

func TestVerifySession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Round-trip.
	tok := signSession("admin", time.Now().Add(sessionDuration))
	u, ok := verifySession(tok)
	if !ok || u != "admin" {
		t.Fatalf("round-trip failed: user=%q ok=%v", u, ok)
	}

	// Tampered token fails.
	parts := strings.SplitN(tok, ".", 2)
	bad := parts[0] + ".AAAA" + parts[1][4:]
	if _, ok := verifySession(bad); ok {
		t.Fatal("tampered signature should fail")
	}

	// Expired token fails.
	expired := signSession("admin", time.Now().Add(-time.Minute))
	if _, ok := verifySession(expired); ok {
		t.Fatal("expired token should fail")
	}

	// Garbage fails.
	if _, ok := verifySession("not-a-token"); ok {
		t.Fatal("garbage token should fail")
	}
}

func TestEnforceBindSecurity(t *testing.T) {
	cases := []struct {
		name     string
		bind     string
		user     string
		pass     string
		insecure string
		wantErr  bool
	}{
		{"loopback ok", "127.0.0.1:8080", "", "", "", false},
		{"localhost ok", "localhost:8080", "", "", "", false},
		{"all interfaces without creds fails", "0.0.0.0:8080", "", "", "", true},
		{"all interfaces with creds ok", "0.0.0.0:8080", "admin", "pass", "", false},
		{"lan ip without creds fails", "192.168.1.90:8080", "", "", "", true},
		{"lan ip with creds ok", "192.168.1.90:8080", "admin", "pass", "", false},
		{"explicit insecure opt-in ok", "0.0.0.0:8080", "", "", "1", false},
		{"bad addr fails", "not-an-addr", "", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Unsetenv("HOURGLASS_AUTH_USER")
			os.Unsetenv("HOURGLASS_AUTH_PASS")
			os.Unsetenv("HOURGLASS_ALLOW_INSECURE")
			if tc.user != "" {
				t.Setenv("HOURGLASS_AUTH_USER", tc.user)
			}
			if tc.pass != "" {
				t.Setenv("HOURGLASS_AUTH_PASS", tc.pass)
			}
			if tc.insecure != "" {
				t.Setenv("HOURGLASS_ALLOW_INSECURE", tc.insecure)
			}
			err := enforceBindSecurity(tc.bind)
			if (err != nil) != tc.wantErr {
				t.Fatalf("enforceBindSecurity(%q) err=%v, wantErr=%v", tc.bind, err, tc.wantErr)
			}
		})
	}
}

// TestEnsureCredentialsForBind covers the Home Assistant-style default: a
// LAN-exposed instance with no configured credentials gets a random password
// generated + persisted (and the same login is reused across restarts),
// while loopback binds and explicit creds stay untouched.
func TestEnsureCredentialsForBind(t *testing.T) {
	t.Run("loopback stays auth-free", func(t *testing.T) {
		os.Unsetenv("HOURGLASS_AUTH_USER")
		os.Unsetenv("HOURGLASS_AUTH_PASS")
		os.Unsetenv("HOURGLASS_ALLOW_INSECURE")
		u, p, err := ensureCredentialsForBind("127.0.0.1:8080")
		if err != nil {
			t.Fatalf("ensureCredentialsForBind(loopback) err=%v", err)
		}
		if u != "" || p != "" {
			t.Fatalf("loopback should stay auth-free, got user=%q pass=%q", u, p)
		}
	})

	t.Run("lan bind generates and persists", func(t *testing.T) {
		os.Unsetenv("HOURGLASS_AUTH_USER")
		os.Unsetenv("HOURGLASS_AUTH_PASS")
		os.Unsetenv("HOURGLASS_ALLOW_INSECURE")
		t.Setenv("HOME", t.TempDir())

		u1, p1, err := ensureCredentialsForBind("0.0.0.0:8080")
		if err != nil {
			t.Fatalf("ensureCredentialsForBind(0.0.0.0) err=%v", err)
		}
		if u1 != "admin" || len(p1) != 16 {
			t.Fatalf("expected admin + 16-char password, got user=%q pass=%q", u1, p1)
		}
		// Credentials must be live in the environment for the auth middleware.
		if os.Getenv("HOURGLASS_AUTH_USER") != "admin" || os.Getenv("HOURGLASS_AUTH_PASS") != p1 {
			t.Fatalf("generated credentials not exported to env: user=%q pass=%q", os.Getenv("HOURGLASS_AUTH_USER"), os.Getenv("HOURGLASS_AUTH_PASS"))
		}
		// Persisted file must exist with the right perms.
		path := filepath.Join(os.Getenv("HOME"), ".hourglass", autoCredentialsFile)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("credentials file not written: %v", err)
		}
		if !strings.Contains(string(data), "HOURGLASS_AUTH_PASS="+p1) {
			t.Fatalf("credentials file missing password: %s", data)
		}

		// A second call must reuse the saved password (stable login).
		u2, p2, err := ensureCredentialsForBind("0.0.0.0:8080")
		if err != nil {
			t.Fatalf("second ensureCredentialsForBind err=%v", err)
		}
		if u2 != u1 || p2 != p1 {
			t.Fatalf("credentials changed across restarts: (%s,%s) -> (%s,%s)", u1, p1, u2, p2)
		}
	})

	t.Run("explicit creds win", func(t *testing.T) {
		t.Setenv("HOURGLASS_AUTH_USER", "bob")
		t.Setenv("HOURGLASS_AUTH_PASS", "secret")
		t.Setenv("HOME", t.TempDir())
		u, p, err := ensureCredentialsForBind("0.0.0.0:8080")
		if err != nil {
			t.Fatalf("ensureCredentialsForBind err=%v", err)
		}
		if u != "bob" || p != "secret" {
			t.Fatalf("explicit creds not honored: user=%q pass=%q", u, p)
		}
		if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".hourglass", autoCredentialsFile)); err == nil {
			t.Fatal("should not write credentials file when explicit creds are set")
		}
	})

	t.Run("insecure opt-in skips generation", func(t *testing.T) {
		os.Unsetenv("HOURGLASS_AUTH_USER")
		os.Unsetenv("HOURGLASS_AUTH_PASS")
		t.Setenv("HOURGLASS_ALLOW_INSECURE", "1")
		t.Setenv("HOME", t.TempDir())
		u, p, err := ensureCredentialsForBind("0.0.0.0:8080")
		if err != nil {
			t.Fatalf("ensureCredentialsForBind err=%v", err)
		}
		if u != "" || p != "" {
			t.Fatalf("insecure opt-in should stay auth-free, got user=%q pass=%q", u, p)
		}
	})
}
