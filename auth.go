package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Session-based authentication for the web UI.
//
// When HOURGLASS_AUTH_USER and HOURGLASS_AUTH_PASS are both set, Hourglass
// requires a signed session cookie (issued by /api/auth/login) for every
// /api/* request. The HTML shell (/), static assets (/dist/*), and the auth
// endpoints themselves stay reachable so the frontend can render its login
// view and then call the API — this replaces the browser's native Basic Auth
// dialog with an in-app login screen.
//
// HTTP Basic Auth is still accepted on API routes as a fallback so scripts
// and curl one-liners keep working unchanged (the frontend never uses it).
//
// Session tokens are HMAC-SHA256 signed with a key persisted at
// ~/.hourglass/auth.key (generated on first use), so logins survive service
// restarts. If the key file can't be created, a random in-memory key is used
// and sessions invalidate on restart.

const (
	sessionCookie      = "hg_session"
	sessionDuration    = 7 * 24 * time.Hour
	sessionKeyFileName = "auth.key"
)

// revokedSessions holds tokens invalidated by logout until their natural
// expiry, so a replayed cookie can't re-authenticate after logout. Stateless
// HMAC tokens can't be invalidated otherwise. Entries self-prune on access.
var revokedSessions = struct {
	sync.Mutex
	m map[string]time.Time // token -> when it expires (safe to forget after)
}{m: make(map[string]time.Time)}

func revokeSession(token string) {
	revokedSessions.Lock()
	revokedSessions.m[token] = time.Now().Add(sessionDuration)
	revokedSessions.Unlock()
}

func isRevoked(token string) bool {
	revokedSessions.Lock()
	defer revokedSessions.Unlock()
	exp, ok := revokedSessions.m[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(revokedSessions.m, token)
		return false
	}
	return true
}

// authCredentials returns the configured web UI credentials. Auth is only
// considered enabled when BOTH user and pass are set.
func authCredentials() (user, pass string, enabled bool) {
	user = os.Getenv("HOURGLASS_AUTH_USER")
	pass = os.Getenv("HOURGLASS_AUTH_PASS")
	return user, pass, user != "" && pass != ""
}

// sessionKey loads the persistent HMAC key from ~/.hourglass/auth.key,
// generating it on first use. On any failure it falls back to a random
// in-memory key so auth still works (sessions just don't survive restarts).
func sessionKey() []byte {
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".hourglass", sessionKeyFileName)
		if b, err := os.ReadFile(p); err == nil && len(b) >= 32 {
			return b[:32]
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err == nil {
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err == nil {
				if err := os.WriteFile(p, key, 0o600); err != nil {
					log.Printf("auth: cannot persist session key at %s: %v (sessions will not survive restarts)", p, err)
				}
			}
			return key
		}
		log.Printf("auth: cannot persist session key: %v (sessions will not survive restarts)", err)
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return key
}

// signSession builds an HMAC-signed session token for the given user valid
// until exp: base64url(user\nexp) + "." + base64url(HMAC(payload)).
func signSession(user string, exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(user + "\n" + strconv.FormatInt(exp.Unix(), 10)))
	mac := hmac.New(sha256.New, sessionKey())
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verifySession checks the signature and expiry of a session token and
// returns the authenticated username. Tampered or expired tokens fail closed.
func verifySession(token string) (string, bool) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	mac := hmac.New(sha256.New, sessionKey())
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[1])) != 1 {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	user, expStr, ok := strings.Cut(string(raw), "\n")
	if !ok {
		return "", false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp || isRevoked(token) {
		return "", false
	}
	return user, true
}

// authenticatedUser resolves the current user from a valid session cookie or,
// as a fallback, valid Basic Auth credentials.
func authenticatedUser(r *http.Request) (string, bool) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if u, ok := verifySession(c.Value); ok {
			return u, true
		}
	}
	user, pass, _ := authCredentials()
	u, p, ok := r.BasicAuth()
	if ok &&
		subtle.ConstantTimeCompare([]byte(u), []byte(user)) == 1 &&
		subtle.ConstantTimeCompare([]byte(p), []byte(pass)) == 1 {
		return u, true
	}
	return "", false
}

// authMiddleware gates API endpoints behind authentication when credentials
// are configured. The HTML shell, static assets, the auth endpoints, and
// /api/version stay public so the frontend can render the login view and the
// browser never sees a native Basic Auth prompt. When auth is disabled
// entirely the middleware passes everything through.
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, enabled := authCredentials()
		if !enabled {
			next.ServeHTTP(w, r)
			return
		}
		path := r.URL.Path
		if path == "/" || strings.HasPrefix(path, "/dist/") ||
			strings.HasPrefix(path, "/api/auth/") || path == "/api/version" {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := authenticatedUser(r); !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(toJSON(APIError{"Authentication required"})))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── Auth endpoints ─────────────────────────────────────────────────────────

type authResponse struct {
	OK   bool   `json:"ok"`
	User string `json:"user,omitempty"`
}

func handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user, pass, enabled := authCredentials()
	if !enabled {
		http.Error(w, toJSON(APIError{"Authentication is not enabled"}), http.StatusForbidden)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, toJSON(APIError{"Invalid JSON"}), http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Username), []byte(user)) != 1 ||
		subtle.ConstantTimeCompare([]byte(req.Password), []byte(pass)) != 1 {
		http.Error(w, toJSON(APIError{"Invalid username or password"}), http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    signSession(user, time.Now().Add(sessionDuration)),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
	w.Write([]byte(toJSON(authResponse{OK: true, User: user})))
}

func handleAuthMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _, enabled := authCredentials()
	if !enabled {
		w.Write([]byte(toJSON(authResponse{OK: true, User: ""})))
		return
	}
	if u, ok := authenticatedUser(r); ok {
		w.Write([]byte(toJSON(authResponse{OK: true, User: u})))
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(toJSON(APIError{"Authentication required"})))
}

func handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Revoke the token server-side so a replayed cookie can't re-authenticate,
	// then clear the browser's cookie.
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		revokeSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	w.Write([]byte(toJSON(authResponse{OK: true})))
}
