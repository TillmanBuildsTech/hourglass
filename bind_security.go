package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// enforceBindSecurity fails closed when Hourglass would be reachable from
// the network without credentials. The web UI can run arbitrary shell
// commands, so an unauthenticated LAN-accessible instance is a remote code
// execution hole.
//
// Rules:
//   - loopback binds (127.0.0.1, localhost, ::1) — always allowed, no auth
//     needed (only the local user can reach it)
//   - non-loopback binds (0.0.0.0, LAN IPs) — require HOURGLASS_AUTH_USER +
//     HOURGLASS_AUTH_PASS, unless HOURGLASS_ALLOW_INSECURE=1 is set
//     explicitly (documented footgun for trusted networks)
func enforceBindSecurity(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid HOURGLASS_BIND %q: %w", addr, err)
	}
	if isLoopbackHost(host) {
		return nil // loopback hostname — local user only
	}
	return requireAuthForExposure(addr)
}

// isLoopbackHost reports whether host is a loopback address or hostname.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func requireAuthForExposure(addr string) error {
	hasCreds := os.Getenv("HOURGLASS_AUTH_USER") != "" && os.Getenv("HOURGLASS_AUTH_PASS") != ""
	if hasCreds {
		return nil
	}
	if os.Getenv("HOURGLASS_ALLOW_INSECURE") == "1" {
		log.Printf("WARNING: serving WITHOUT authentication on %s (HOURGLASS_ALLOW_INSECURE=1) — anyone on the network can run commands as this user", addr)
		return nil
	}
	return fmt.Errorf(
		"HOURGLASS_BIND=%s exposes the web UI on all interfaces, but no credentials are configured. "+
			"Set HOURGLASS_AUTH_USER and HOURGLASS_AUTH_PASS, bind to 127.0.0.1:8080, "+
			"or explicitly opt in to an open server with HOURGLASS_ALLOW_INSECURE=1", addr)
}

// autoCredentialsFile is where auto-generated LAN credentials are persisted
// so the login survives restarts (same pattern as the session key in
// ~/.hourglass/auth.key). Format mirrors /etc/hourglass.env.
const autoCredentialsFile = "auth.env"

// ensureCredentialsForBind provisions credentials for a LAN-exposed instance
// before the server starts (the Home Assistant model: install anywhere, reach
// it at hourglass.local, and a random password is generated + printed on
// first run). It is a no-op for loopback binds and when credentials are
// already configured. Returns the active (user, pass) when auth is enabled —
// "" otherwise — so main can print the login banner.
func ensureCredentialsForBind(addr string) (string, string, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", fmt.Errorf("invalid HOURGLASS_BIND %q: %w", addr, err)
	}
	// Loopback: local user only, auth stays off (matches old default).
	if isLoopbackHost(host) {
		return "", "", nil
	}
	// Explicit opt-in to an open server: auth stays off.
	if os.Getenv("HOURGLASS_ALLOW_INSECURE") == "1" {
		return "", "", nil
	}
	// Already configured: nothing to do.
	if u, p, ok := authCredentials(); ok {
		return u, p, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve home directory for generated credentials: %w", err)
	}
	path := filepath.Join(home, ".hourglass", autoCredentialsFile)

	// Reuse saved credentials if present (login stays stable across restarts).
	if data, err := os.ReadFile(path); err == nil {
		var savedUser, savedPass string
		for _, line := range strings.Split(string(data), "\n") {
			if k, v, ok := strings.Cut(line, "="); ok {
				switch strings.TrimSpace(k) {
				case "HOURGLASS_AUTH_USER":
					savedUser = strings.TrimSpace(v)
				case "HOURGLASS_AUTH_PASS":
					savedPass = strings.TrimSpace(v)
				}
			}
		}
		if savedUser != "" && savedPass != "" {
			os.Setenv("HOURGLASS_AUTH_USER", savedUser)
			os.Setenv("HOURGLASS_AUTH_PASS", savedPass)
			return savedUser, savedPass, nil
		}
	}

	// Generate + persist a random password (16 hex chars, like the .deb/.rpm
	// postinstall's `openssl rand -hex 8`).
	user := "admin"
	pass := randomHex(16)
	content := fmt.Sprintf("HOURGLASS_AUTH_USER=%s\nHOURGLASS_AUTH_PASS=%s\n", user, pass)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", "", fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", "", fmt.Errorf("cannot save generated credentials to %s: %w", path, err)
	}
	os.Setenv("HOURGLASS_AUTH_USER", user)
	os.Setenv("HOURGLASS_AUTH_PASS", pass)
	return user, pass, nil
}

// randomHex returns n random hex characters (n/2 bytes) from crypto/rand.
func randomHex(n int) string {
	b := make([]byte, n/2)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read never fails on supported platforms; fail loudly
		// rather than ship weak credentials.
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// portOf extracts the port from a bind address ("0.0.0.0:8080" -> "8080").
func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "8080"
	}
	return port
}
