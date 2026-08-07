package main

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// startRedirectServer boots serveTLSWithRedirectListener on an ephemeral
// port with a freshly generated cert, and returns its base URL and the app
// handler it wrapped.
func startRedirectServer(t *testing.T, app http.Handler) (base string, ln net.Listener) {
	t.Helper()
	dir := t.TempDir()
	if err := ensureTLSCerts(dir, []string{"hourglass.local", "localhost"}); err != nil {
		t.Fatalf("ensureTLSCerts: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go serveTLSWithRedirectListener(ln,
		filepath.Join(dir, "hourglass.pem"), filepath.Join(dir, "hourglass-key.pem"), app)
	return "http://" + ln.Addr().String(), ln
}

// noRedirectClient returns an HTTP client that does not follow redirects,
// so tests can assert the 308 response itself.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func TestRedirectListenerRedirectsPlainHTTP(t *testing.T) {
	// App handler that must never be reached over plain HTTP.
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("app handler reached over plain HTTP")
		w.WriteHeader(http.StatusInternalServerError)
	})
	base, ln := startRedirectServer(t, app)
	defer ln.Close()

	resp, err := noRedirectClient().Get(base + "/api/cron")
	if err != nil {
		t.Fatalf("plain HTTP GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("status = %d, want 308", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	want := strings.Replace(base, "http://", "https://", 1) + "/api/cron"
	if loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
}

func TestRedirectListenerPassesTLS(t *testing.T) {
	got := false
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			t.Error("app handler got a non-TLS request")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		got = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("secure"))
	})
	_, ln := startRedirectServer(t, app)
	defer ln.Close()

	u, _ := url.Parse("https://" + ln.Addr().String() + "/api/cron")
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	resp, err := client.Get(u.String())
	if err != nil {
		t.Fatalf("TLS GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !got {
		t.Error("app handler was not invoked over TLS")
	}
}

func TestRedirectListenerAuthRunsAfterRedirect(t *testing.T) {
	// Plain HTTP must get the redirect BEFORE the app's auth middleware —
	// i.e. no 401 from a gated app; the redirect handler wraps the app.
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized) // what authMiddleware would do
	})
	base, ln := startRedirectServer(t, app)
	defer ln.Close()

	resp, err := noRedirectClient().Get(base + "/api/cron")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("plain HTTP status = %d, want 308 (redirect must precede auth)", resp.StatusCode)
	}
}

// TestBonjourStubFallsBack ensures the non-cgo stub reports "not
// registered" so startMDNS uses the multicast responder. This file compiles
// wherever bonjour_stub.go does (its build tags), so on darwin+cgo builds
// tryBonjourRegister is the real implementation and this test is excluded —
// matching the platform matrix.
func TestBonjourStubFallsBack(t *testing.T) {
	if tryBonjourRegister("hourglass", net.ParseIP("192.168.1.241"), 8080, true) {
		t.Fatal("stub tryBonjourRegister must return false (fall back to multicast)")
	}
}
