package main

// Local HTTPS via a per-machine root CA (the mkcert model).
//
// Public CAs (Let's Encrypt et al.) cannot issue certificates for
// hourglass.local: .local is reserved by RFC 6762 for mDNS and does not
// exist in the public DNS, so no ACME challenge can validate it, and the
// CA/Browser Forum Baseline Requirements forbid public certificates for
// internal names (modern browsers distrust them outright). The only way
// every person who downloads Hourglass gets a warning-free
// https://hourglass.local is a root CA generated on their own machine
// and installed into that machine's trust store. That is exactly what
// this file does, mirroring how mkcert and Tailscale (for *.ts.net)
// handle local TLS.
//
// Files live in ~/.hourglass/tls/:
//
//	ca.pem + ca-key.pem      per-machine root CA (10 years, P-256)
//	hourglass.pem + key      leaf cert for <name>.local (825 days)
//
// HOURGLASS_TLS controls behavior:
//
//	"auto" (default)  serve HTTPS when the CA was installed into the OS
//	                  trust store; otherwise fall back to plain HTTP and
//	                  log instructions (never surprise the user with an
//	                  untrusted-cert warning page)
//	"1"  ("on")       force HTTPS even if the CA isn't trusted yet (for
//	                  admins who install trust out-of-band)
//	"0"  ("off")      plain HTTP, exactly like pre-0.11 builds
//
// The CA install is best-effort and idempotent: Linux writes to
// /usr/local/share/ca-certificates and runs update-ca-certificates,
// macOS uses `security add-trusted-cert` (system keychain when root,
// login keychain otherwise), Windows uses `certutil -addstore Root`.
// Firefox is handled separately because it ships its own trust store:
// its `security.enterprise_roots.enabled` pref is flipped in every
// profile so it trusts the OS store too (the same trick mkcert uses).

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	caCommonName  = "Hourglass Local CA"
	leafLifetime  = 825  // days, same ballpark as mkcert
	caLifetime    = 3650 // days (10 years)
	leafRenewEdge = 30   // days: renew the leaf when this close to expiry
)

type tlsSetup struct {
	certFile  string // leaf cert (PEM, leaf then CA chain)
	keyFile   string // leaf private key (PEM, 0600)
	caFile    string // root CA cert (PEM)
	serveTLS  bool   // serve HTTPS with these files
	caTrusted bool   // the CA is in the OS/browser trust store
}

// tlsMode parses HOURGLASS_TLS into "auto", "force", or "off".
func tlsMode() string {
	switch strings.ToLower(os.Getenv("HOURGLASS_TLS")) {
	case "0", "off", "false", "no":
		return "off"
	case "1", "on", "true", "yes", "force":
		return "force"
	default:
		return "auto"
	}
}

// tlsHosts returns the DNS names the leaf cert must cover: the mDNS name
// (HOURGLASS_MDNS_NAME, default "hourglass") as <name>.local, plus
// localhost — every way the UI is normally opened.
func tlsHosts() []string {
	name := os.Getenv("HOURGLASS_MDNS_NAME")
	if name == "" {
		name = "hourglass"
	}
	return []string{name + ".local", "localhost"}
}

func tlsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".hourglass", "tls"), nil
}

// setupTLS prepares local HTTPS. It returns nil when TLS is disabled.
// In "auto" mode it returns serveTLS=false when the CA couldn't be
// installed (falling back to HTTP); in "force" mode it serves HTTPS
// regardless of trust so an admin can install trust separately.
func setupTLS() *tlsSetup {
	if tlsMode() == "off" {
		return nil
	}
	dir, err := tlsDir()
	if err != nil {
		log.Printf("TLS: cannot resolve home directory: %v", err)
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("TLS: cannot create %s: %v", dir, err)
		return nil
	}

	setup := &tlsSetup{
		certFile: filepath.Join(dir, "hourglass.pem"),
		keyFile:  filepath.Join(dir, "hourglass-key.pem"),
		caFile:   filepath.Join(dir, "ca.pem"),
	}
	if err := ensureTLSCerts(dir, tlsHosts()); err != nil {
		log.Printf("TLS: certificate setup failed: %v", err)
		return nil
	}

	installed, err := installCA(setup.caFile)
	if err != nil {
		log.Printf("TLS: could not install the local CA into the OS trust store: %v", err)
		log.Printf("TLS: to enable HTTPS anyway run 'hourglass -install-ca' as root/admin, then restart (or set HOURGLASS_TLS=1 and trust %s manually)", setup.caFile)
	} else if installed {
		setup.caTrusted = true
	}

	if tlsMode() == "force" {
		setup.serveTLS = true
		return setup
	}
	if setup.caTrusted {
		setup.serveTLS = true
		log.Printf("TLS: local CA installed — https://%s will show a valid lock in browsers", tlsHosts()[0])
	} else {
		log.Printf("TLS: serving plain HTTP because the local CA is not trusted by this OS (see messages above). " +
			"Run 'hourglass -install-ca' as root/admin once to enable warning-free HTTPS, or set HOURGLASS_TLS=1 to force HTTPS anyway.")
	}
	return setup
}

// runInstallCA implements `hourglass -install-ca`: make sure a CA + leaf
// exist, install the CA into the OS trust store, and exit.
func runInstallCA() error {
	dir, err := tlsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := ensureTLSCerts(dir, tlsHosts()); err != nil {
		return err
	}
	caFile := filepath.Join(dir, "ca.pem")
	installed, err := installCA(caFile)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("the CA exists at %s but could not be added to the trust store", caFile)
	}
	return nil
}

// ensureTLSCerts makes sure a valid CA and leaf cert exist in dir,
// reusing whatever is already there. If the CA exists but the leaf is
// missing/expired/missing hosts, only the leaf is regenerated so trust
// in the existing CA is preserved.
func ensureTLSCerts(dir string, hosts []string) error {
	caFile := filepath.Join(dir, "ca.pem")
	caKeyFile := filepath.Join(dir, "ca-key.pem")
	certFile := filepath.Join(dir, "hourglass.pem")
	keyFile := filepath.Join(dir, "hourglass-key.pem")

	ca, caKey, ok := loadCA(caFile, caKeyFile)
	if !ok {
		if err := generateCA(caFile, caKeyFile); err != nil {
			return fmt.Errorf("generate CA: %w", err)
		}
		ca, caKey, ok = loadCA(caFile, caKeyFile)
		if !ok {
			return fmt.Errorf("newly generated CA does not parse")
		}
	}

	if leafValid(certFile, keyFile, ca, hosts) {
		return nil
	}
	if err := generateLeaf(ca, caKey, certFile, keyFile, hosts); err != nil {
		return fmt.Errorf("generate leaf: %w", err)
	}
	return nil
}

func loadCA(caFile, caKeyFile string) (*x509.Certificate, *ecdsa.PrivateKey, bool) {
	certPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, nil, false
	}
	keyPEM, err := os.ReadFile(caKeyFile)
	if err != nil {
		return nil, nil, false
	}
	cert, err := parseCert(certPEM)
	if err != nil {
		return nil, nil, false
	}
	key, err := parseECDSAKey(keyPEM)
	if err != nil {
		return nil, nil, false
	}
	return cert, key, true
}

// leafValid reports whether the existing leaf cert covers all hosts, is
// signed by our CA, isn't near expiry, and its key file matches it.
func leafValid(certFile, keyFile string, ca *x509.Certificate, hosts []string) bool {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return false
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return false
	}
	cert, err := parseCert(certPEM)
	if err != nil {
		return false
	}
	key, err := parseECDSAKey(keyPEM)
	if err != nil {
		return false
	}
	if !cert.PublicKey.(*ecdsa.PublicKey).Equal(&key.PublicKey) {
		return false
	}
	if time.Now().After(cert.NotAfter.AddDate(0, 0, -leafRenewEdge)) {
		return false // too close to (or past) expiry — renew
	}
	for _, h := range hosts {
		if err := cert.VerifyHostname(h); err != nil {
			return false
		}
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return false
	}
	return true
}

func generateCA(caFile, caKeyFile string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: caCommonName, Organization: []string{"Hourglass"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(0, 0, caLifetime),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0, // leaf certs only, no subordinate CAs
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err := writePEM(caFile, "CERTIFICATE", der, 0o644); err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return writePEM(caKeyFile, "EC PRIVATE KEY", keyDER, 0o600)
}

func generateLeaf(ca *x509.Certificate, caKey *ecdsa.PrivateKey, certFile, keyFile string, hosts []string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: hosts[0]},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(0, 0, leafLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     append([]string{}, hosts...),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := writePEM(certFile, "CERTIFICATE", der, 0o644); err != nil {
		return err
	}
	// Append the CA to the leaf file so clients can build the chain even
	// without the root in their store (harmless when they have it).
	chain := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})
	if f, err := os.OpenFile(certFile, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		f.Write(chain)
		f.Close()
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return writePEM(keyFile, "EC PRIVATE KEY", keyDER, 0o600)
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func parseCert(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseECDSAKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if eck, ok := k.(*ecdsa.PrivateKey); ok {
			return eck, nil
		}
	}
	return nil, fmt.Errorf("not an ECDSA private key")
}

func writePEM(path, typ string, der []byte, perm os.FileMode) error {
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), perm)
}

// handleCAPEM serves the root CA certificate. The CA cert itself is public
// material (only its private key is secret), so any device on the network
// can fetch it — e.g. `curl -k https://hourglass.local:8080/ca.pem` — and
// install it as trusted to get a valid https://hourglass.local from that
// device too. Requires auth when credentials are configured.
func handleCAPEM(caFile string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(caFile)
		if err != nil {
			http.Error(w, "CA file not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Header().Set("Content-Disposition", `inline; filename="hourglass-ca.pem"`)
		w.Write(b)
	}
}

// ── OS trust-store install ──────────────────────────────────────────────

// installCA puts the root CA into the OS/browser trust stores so browsers
// treat https://hourglass.local as valid. Best-effort and idempotent:
// returns true when the CA is trusted (already was, or just installed).
func installCA(caFile string) (bool, error) {
	switch runtime.GOOS {
	case "linux":
		return installCALinux(caFile)
	case "darwin":
		return installCADarwin(caFile)
	case "windows":
		return installCAWindows(caFile)
	default:
		return false, fmt.Errorf("no automatic CA install for %s — trust %s manually", runtime.GOOS, caFile)
	}
}

const linuxCADest = "/usr/local/share/ca-certificates/hourglass-ca.crt"

func installCALinux(caFile string) (bool, error) {
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return false, err
	}
	// Already trusted? The system bundle is updated by update-ca-certificates.
	if bundleContainsCA("/etc/ssl/certs/ca-certificates.crt", caPEM) {
		installFirefoxEnterprisePrefs() // best effort
		return true, nil
	}
	if err := os.WriteFile(linuxCADest, caPEM, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", linuxCADest, err)
	}
	if out, err := exec.Command("update-ca-certificates").CombinedOutput(); err != nil {
		return false, fmt.Errorf("update-ca-certificates: %v: %s", err, strings.TrimSpace(string(out)))
	}
	installFirefoxEnterprisePrefs()
	return true, nil
}

// bundleContainsCA reports whether the PEM bundle at bundlePath contains
// the same certificate as caPEM (compared by raw bytes via cert.Equal).
func bundleContainsCA(bundlePath string, caPEM []byte) bool {
	bundle, err := os.ReadFile(bundlePath)
	if err != nil {
		return false
	}
	our, err := parseCert(caPEM)
	if err != nil {
		return false
	}
	rest := bundle
	for {
		block, next := pem.Decode(rest)
		if block == nil {
			return false
		}
		rest = next
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		if c.Equal(our) {
			return true
		}
	}
}

func installCADarwin(caFile string) (bool, error) {
	if _, err := exec.LookPath("security"); err != nil {
		return false, fmt.Errorf("security tool not found: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	// System keychain when root; otherwise the login keychain (no admin).
	candidates := []string{"/Library/Keychains/System.keychain"}
	if os.Geteuid() != 0 {
		candidates = append(candidates, filepath.Join(home, "Library", "Keychains", "login.keychain-db"))
	}
	for i, store := range candidates {
		if keychainHasCA(store) {
			installFirefoxEnterprisePrefs()
			return true, nil
		}
		if out, err := exec.Command("security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", store, caFile).CombinedOutput(); err != nil {
			if i == len(candidates)-1 {
				return false, fmt.Errorf("security add-trusted-cert: %v: %s", err, strings.TrimSpace(string(out)))
			}
			continue
		}
		installFirefoxEnterprisePrefs()
		return true, nil
	}
	return false, fmt.Errorf("could not install CA into a macOS keychain")
}

func keychainHasCA(store string) bool {
	err := exec.Command("security", "find-certificate", "-c", caCommonName, store).Run()
	return err == nil
}

func installCAWindows(caFile string) (bool, error) {
	if _, err := exec.LookPath("certutil"); err != nil {
		return false, fmt.Errorf("certutil not found: %v", err)
	}
	// `certutil -store Root <name>` exits nonzero when the CA isn't there.
	if err := exec.Command("certutil", "-store", "Root", caCommonName).Run(); err == nil {
		installFirefoxEnterprisePrefs()
		return true, nil
	}
	if out, err := exec.Command("certutil", "-addstore", "-f", "Root", caFile).CombinedOutput(); err != nil {
		return false, fmt.Errorf("certutil -addstore: %v: %s", err, strings.TrimSpace(string(out)))
	}
	installFirefoxEnterprisePrefs()
	return true, nil
}

// ── Firefox: make it trust the OS store ─────────────────────────────────
// Firefox ships its own trust store and ignores system CAs unless
// security.enterprise_roots.enabled is set. Flipping that pref in every
// profile is the same approach mkcert takes (no certutil dependency).
// Best effort: Firefox rewrites prefs.js on exit, so this sticks only
// when the browser is closed; re-running the app re-applies it.

const firefoxEnterprisePref = `user_pref("security.enterprise_roots.enabled", true);`

func installFirefoxEnterprisePrefs() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	var roots []string
	switch runtime.GOOS {
	case "linux":
		roots = []string{filepath.Join(home, ".mozilla", "firefox")}
	case "darwin":
		roots = []string{filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles")}
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			roots = []string{filepath.Join(appdata, "Mozilla", "Firefox", "Profiles")}
		}
	}
	for _, root := range roots {
		prefs, err := filepath.Glob(filepath.Join(root, "*", "prefs.js"))
		if err != nil {
			continue
		}
		for _, p := range prefs {
			setFirefoxEnterprisePref(p)
		}
	}
}

func setFirefoxEnterprisePref(prefsPath string) {
	data, err := os.ReadFile(prefsPath)
	if err != nil {
		return
	}
	if bytes.Contains(data, []byte("security.enterprise_roots.enabled")) {
		return
	}
	f, err := os.OpenFile(prefsPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString("\n" + firefoxEnterprisePref + "\n")
}
