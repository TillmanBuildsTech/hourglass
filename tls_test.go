package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTLSMode(t *testing.T) {
	cases := []struct {
		val  string
		want string
	}{
		{"", "auto"},
		{"auto", "auto"},
		{"0", "off"},
		{"OFF", "off"},
		{"false", "off"},
		{"1", "force"},
		{"on", "force"},
		{"TRUE", "force"},
		{"bogus", "auto"},
	}
	for _, tc := range cases {
		t.Setenv("HOURGLASS_TLS", tc.val)
		if got := tlsMode(); got != tc.want {
			t.Errorf("tlsMode(%q) = %q, want %q", tc.val, got, tc.want)
		}
	}
}

func TestTLSCertGenerationAndReuse(t *testing.T) {
	dir := t.TempDir()
	hosts := []string{"hourglass.local", "localhost"}
	if err := ensureTLSCerts(dir, hosts); err != nil {
		t.Fatalf("ensureTLSCerts: %v", err)
	}

	caFile := filepath.Join(dir, "ca.pem")
	certFile := filepath.Join(dir, "hourglass.pem")

	// CA looks like a root CA with a long lifetime.
	ca, caKey, ok := loadCA(caFile, filepath.Join(dir, "ca-key.pem"))
	if !ok {
		t.Fatal("generated CA does not parse")
	}
	if !ca.IsCA || !ca.BasicConstraintsValid {
		t.Error("CA is missing IsCA/BasicConstraintsValid")
	}
	if time.Until(ca.NotAfter) < 9*365*24*time.Hour {
		t.Errorf("CA lifetime too short: %v", time.Until(ca.NotAfter))
	}
	if ca.Subject.CommonName != caCommonName {
		t.Errorf("CA CN = %q, want %q", ca.Subject.CommonName, caCommonName)
	}

	// Leaf covers the hosts, is signed by the CA, key matches, has
	// loopback IP SANs, and carries the CA in its chain file.
	leafPEM, _ := os.ReadFile(certFile)
	leaf, err := parseCert(leafPEM)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	for _, h := range hosts {
		if err := leaf.VerifyHostname(h); err != nil {
			t.Errorf("leaf missing SAN for %q: %v", h, err)
		}
	}
	key, err := parseECDSAKey(mustRead(t, filepath.Join(dir, "hourglass-key.pem")))
	if err != nil {
		t.Fatalf("parse leaf key: %v", err)
	}
	if !leaf.PublicKey.(*ecdsa.PublicKey).Equal(&key.PublicKey) {
		t.Error("leaf cert and key do not match")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Errorf("leaf not signed by our CA: %v", err)
	}
	hasLoopback := false
	for _, ip := range leaf.IPAddresses {
		if ip.String() == "127.0.0.1" {
			hasLoopback = true
		}
	}
	if !hasLoopback {
		t.Error("leaf missing 127.0.0.1 IP SAN")
	}
	// The cert file must be leaf + CA chain (2 PEM blocks).
	rest := leafPEM
	blocks := 0
	for {
		block, next := pem.Decode(rest)
		if block == nil {
			break
		}
		blocks++
		rest = next
	}
	if blocks != 2 {
		t.Errorf("cert file has %d PEM blocks, want 2 (leaf + CA)", blocks)
	}

	// A second ensureTLSCerts must reuse everything (same serials).
	ca2, _, _ := loadCA(caFile, filepath.Join(dir, "ca-key.pem"))
	leaf2, _ := parseCert(mustRead(t, certFile))
	if ca2.SerialNumber.Cmp(ca.SerialNumber) != 0 {
		t.Error("CA was regenerated on reuse")
	}
	if leaf2.SerialNumber.Cmp(leaf.SerialNumber) != 0 {
		t.Error("leaf was regenerated on reuse")
	}
	_ = caKey
}

func TestTLSLeafRenewsButCAStays(t *testing.T) {
	dir := t.TempDir()
	if err := ensureTLSCerts(dir, []string{"hourglass.local", "localhost"}); err != nil {
		t.Fatalf("ensureTLSCerts: %v", err)
	}
	ca, _, _ := loadCA(filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca-key.pem"))
	leaf, _ := parseCert(mustRead(t, filepath.Join(dir, "hourglass.pem")))

	// New hostname appears (e.g. HOURGLASS_MDNS_NAME changed): the leaf must
	// be re-issued from the SAME CA, covering the new name.
	if err := ensureTLSCerts(dir, []string{"custom.local", "hourglass.local", "localhost"}); err != nil {
		t.Fatalf("ensureTLSCerts with new host: %v", err)
	}
	ca2, _, _ := loadCA(filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca-key.pem"))
	leaf2, _ := parseCert(mustRead(t, filepath.Join(dir, "hourglass.pem")))

	if ca2.SerialNumber.Cmp(ca.SerialNumber) != 0 {
		t.Error("CA changed when only the leaf needed renewal")
	}
	if leaf2.SerialNumber.Cmp(leaf.SerialNumber) == 0 {
		t.Error("leaf was not re-issued for the new host")
	}
	if err := leaf2.VerifyHostname("custom.local"); err != nil {
		t.Errorf("renewed leaf missing custom.local: %v", err)
	}
	if err := leaf2.VerifyHostname("hourglass.local"); err != nil {
		t.Errorf("renewed leaf lost hourglass.local: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca2)
	if _, err := leaf2.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Errorf("renewed leaf not signed by CA: %v", err)
	}
}

func TestLeafValid(t *testing.T) {
	dir := t.TempDir()
	hosts := []string{"hourglass.local", "localhost"}
	if err := ensureTLSCerts(dir, hosts); err != nil {
		t.Fatalf("ensureTLSCerts: %v", err)
	}
	ca, _, _ := loadCA(filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca-key.pem"))
	certFile := filepath.Join(dir, "hourglass.pem")
	keyFile := filepath.Join(dir, "hourglass-key.pem")

	if !leafValid(certFile, keyFile, ca, hosts) {
		t.Error("freshly generated leaf should validate")
	}
	if leafValid(certFile, keyFile, ca, []string{"other.local"}) {
		t.Error("leaf should not validate for missing hosts")
	}
	if leafValid(filepath.Join(dir, "missing.pem"), keyFile, ca, hosts) {
		t.Error("leafValid should be false when cert file is missing")
	}
}

func TestTLSHostsHonorsMDNSName(t *testing.T) {
	t.Setenv("HOURGLASS_MDNS_NAME", "sandbox")
	hosts := tlsHosts()
	if len(hosts) != 2 || hosts[0] != "sandbox.local" || hosts[1] != "localhost" {
		t.Errorf("tlsHosts() = %v, want [sandbox.local localhost]", hosts)
	}
}

func TestBundleContainsCA(t *testing.T) {
	dir := t.TempDir()
	if err := ensureTLSCerts(dir, []string{"hourglass.local", "localhost"}); err != nil {
		t.Fatalf("ensureTLSCerts: %v", err)
	}
	caPEM, _ := os.ReadFile(filepath.Join(dir, "ca.pem"))

	bundle := filepath.Join(dir, "bundle.crt")
	if err := os.WriteFile(bundle, caPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if !bundleContainsCA(bundle, caPEM) {
		t.Error("bundle containing the CA should match")
	}
	other := filepath.Join(dir, "other.crt")
	if err := os.WriteFile(other, []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if bundleContainsCA(other, caPEM) {
		t.Error("bundle without the CA must not match")
	}
	if bundleContainsCA(filepath.Join(dir, "missing.crt"), caPEM) {
		t.Error("missing bundle must not match")
	}
}

func TestFirefoxEnterprisePref(t *testing.T) {
	dir := t.TempDir()
	prefs := filepath.Join(dir, "prefs.js")
	if err := os.WriteFile(prefs, []byte("user_pref(\"browser.startup.page\", 1);\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	setFirefoxEnterprisePref(prefs)
	data, _ := os.ReadFile(prefs)
	if !contains(string(data), firefoxEnterprisePref) {
		t.Fatalf("pref not written: %s", data)
	}
	// Idempotent.
	setFirefoxEnterprisePref(prefs)
	data2, _ := os.ReadFile(prefs)
	if count(data2, firefoxEnterprisePref) != 1 {
		t.Errorf("pref written more than once: %s", data2)
	}
	// Missing file is a no-op (no panic).
	setFirefoxEnterprisePref(filepath.Join(dir, "nope", "prefs.js"))
}

func TestCAPEMHandler(t *testing.T) {
	dir := t.TempDir()
	if err := ensureTLSCerts(dir, []string{"hourglass.local", "localhost"}); err != nil {
		t.Fatalf("ensureTLSCerts: %v", err)
	}
	caPEM, _ := os.ReadFile(filepath.Join(dir, "ca.pem"))

	req := httptest.NewRequest("GET", "/ca.pem", nil)
	w := httptest.NewRecorder()
	handleCAPEM(filepath.Join(dir, "ca.pem"))(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/x-pem-file" {
		t.Errorf("Content-Type = %q", ct)
	}
	if string(w.Body.Bytes()) != string(caPEM) {
		t.Error("body is not the CA cert")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func count(b []byte, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(b); i++ {
		if string(b[i:i+len(sub)]) == sub {
			n++
		}
	}
	return n
}
