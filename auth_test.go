package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMaybeAuthNoCredsConfigured(t *testing.T) {
	os.Unsetenv("HOURGLASS_AUTH_USER")
	os.Unsetenv("HOURGLASS_AUTH_PASS")

	var called bool
	h := maybeAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !called {
		t.Fatalf("no-auth passthrough broken: code=%d called=%v", rec.Code, called)
	}
}

func TestMaybeAuthRequiresCredentials(t *testing.T) {
	os.Setenv("HOURGLASS_AUTH_USER", "admin")
	os.Setenv("HOURGLASS_AUTH_PASS", "s3cret")
	defer func() {
		os.Unsetenv("HOURGLASS_AUTH_USER")
		os.Unsetenv("HOURGLASS_AUTH_PASS")
	}()

	var called bool
	h := maybeAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// No credentials → 401
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without creds, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("expected WWW-Authenticate Basic header, got %q", rec.Header().Get("WWW-Authenticate"))
	}
	if called {
		t.Fatal("handler called without credentials")
	}

	// Wrong credentials → 401
	req.SetBasicAuth("admin", "wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong creds, got %d", rec.Code)
	}

	// Correct credentials → 200
	req.SetBasicAuth("admin", "s3cret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("expected 200 with correct creds, got %d (called=%v)", rec.Code, called)
	}
}

func TestEnforceBindSecurity(t *testing.T) {
	cases := []struct {
		name    string
		bind    string
		user    string
		pass    string
		insecure string
		wantErr bool
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
				os.Setenv("HOURGLASS_AUTH_USER", tc.user)
			}
			if tc.pass != "" {
				os.Setenv("HOURGLASS_AUTH_PASS", tc.pass)
			}
			if tc.insecure != "" {
				os.Setenv("HOURGLASS_ALLOW_INSECURE", tc.insecure)
			}
			err := enforceBindSecurity(tc.bind)
			if (err != nil) != tc.wantErr {
				t.Fatalf("enforceBindSecurity(%q) err=%v, wantErr=%v", tc.bind, err, tc.wantErr)
			}
		})
	}
}
