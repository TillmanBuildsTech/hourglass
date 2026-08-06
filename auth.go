package main

import (
	"crypto/subtle"
	"net/http"
	"os"
)

// maybeAuth wraps the HTTP mux with Basic Auth when HOURGLASS_AUTH_USER and
// HOURGLASS_AUTH_PASS are both set. The web UI can execute arbitrary shell
// commands (Run now, cron writes), so it must never be reachable from the
// network without credentials — enforceBindSecurity() fails closed on that.
func maybeAuth(next http.Handler) http.Handler {
	user := os.Getenv("HOURGLASS_AUTH_USER")
	pass := os.Getenv("HOURGLASS_AUTH_PASS")
	if user == "" && pass == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(pass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Hourglass"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
