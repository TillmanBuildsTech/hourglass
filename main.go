package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/TillmanBuildsTech/hourglass/connection"
	"github.com/TillmanBuildsTech/hourglass/cron"
	"github.com/TillmanBuildsTech/hourglass/mcp"
	sshclient "github.com/TillmanBuildsTech/hourglass/ssh"
)

//go:embed ui
var uiFS embed.FS

//go:embed VERSION
var versionFS embed.FS

// version is bumped in the VERSION file with every PR.
func version() string {
	b, err := versionFS.ReadFile("VERSION")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}

type APIError struct {
	Error string `json:"error"`
}

type Entry struct {
	Schedule   string `json:"Schedule"`
	Command    string `json:"Command"`
	Comment    string `json:"Comment,omitempty"`
	Inactive   bool   `json:"Inactive,omitempty"`
	LastRun    *int64 `json:"LastRun,omitempty"`
	LastStatus string `json:"LastStatus,omitempty"`
	LastCode   int    `json:"LastCode,omitempty"`
}

type DeleteRequest struct {
	Index int `json:"index"`
}

var cronManager *cron.Manager
var connManager *connection.Manager

func main() {
	godotenv.Load()

	showVersion := flag.Bool("version", false, "print the Hourglass version and exit")
	mcpMode := flag.Bool("mcp", false, "run as an MCP (Model Context Protocol) stdio server for AI agent integration, instead of the web UI")
	installCA := flag.Bool("install-ca", false, "generate the local TLS CA (if needed) and install it into the OS trust store, then exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("hourglass v%s\n", version())
		return
	}

	var err error
	connManager, err = connection.NewManager("")
	if err != nil {
		log.Fatalf("Failed to initialize connection manager: %v", err)
	}

	// newLocalExecutor returns the executor for local (non-remote) operation.
	// Normally the system crontab; HOURGLASS_CRONTAB_FILE redirects it to a
	// plain file for isolated E2E/integration testing (see cron.FileExecutor).
	cronManager = cron.NewManagerWithExecutor(newLocalExecutor())
	cronManager.StartHistoryRefresh()

	// Restore whichever connection was active when Hourglass last exited -
	// cron.NewManager() defaults to a LocalExecutor, so without this the
	// UI would show the saved remote connection as "current" (it reads
	// that straight from connections.json) while every /api/cron request
	// silently served the local machine's crontab instead.
	//
	// Unlike switchToRemoteConnection (used by the interactive "Connect"
	// button), the executor is adopted unconditionally here rather than
	// only after a successful reachability check: at boot the network may
	// not be up yet, and if it isn't, subsequent SSH calls should fail
	// loudly through the normal error path rather than silently keeping
	// the local executor while the UI claims to be on the remote host.
	if cfg := connManager.GetActive(); cfg != nil && !cfg.IsLocal {
		client, err := sshclient.NewClient(cfg.Host, cfg.Port, cfg.User, cfg.KeyPath)
		if err != nil {
			log.Printf("failed to restore saved connection %q: %v", cfg.ID, err)
		} else {
			cronManager.SetExecutor(client)
			if err := client.Connect(); err != nil {
				log.Printf("saved connection %q is not currently reachable: %v", cfg.ID, err)
			}
		}
	}

	if *mcpMode {
		if err := mcp.NewServer(cronManager, version()).Serve(os.Stdin, os.Stdout); err != nil {
			log.Fatalf("MCP server failed: %v", err)
		}
		return
	}

	if *installCA {
		if err := runInstallCA(); err != nil {
			log.Fatalf("CA install failed: %v", err)
		}
		fmt.Println("Hourglass local CA is installed and trusted. Restart Hourglass to serve HTTPS.")
		return
	}

	distFS, _ := fs.Sub(uiFS, "ui/dist")
	// .webmanifest isn't in Go's built-in MIME table; Chrome requires a JSON
	// manifest MIME type or it ignores the web app manifest.
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
	http.Handle("/dist/", http.StripPrefix("/dist/", http.FileServer(http.FS(distFS))))
	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/api/version", handleVersion)
	http.HandleFunc("/api/auth/login", handleAuthLogin)
	http.HandleFunc("/api/auth/me", handleAuthMe)
	http.HandleFunc("/api/auth/logout", handleAuthLogout)
	http.HandleFunc("/api/cron", handleCron)
	http.HandleFunc("/api/cron/update", handleUpdateCron)
	http.HandleFunc("/api/cron/execute", handleExecuteCron)
	http.HandleFunc("/api/cron/toggle", handleToggleCron)
	http.HandleFunc("/api/connections", handleConnections)
	http.HandleFunc("/api/connections/active", handleConnectionActive)
	http.HandleFunc("/api/connections/test", handleConnectionTest)
	http.HandleFunc("/api/logs", handleLogs)

	addr := os.Getenv("HOURGLASS_BIND")
	if addr == "" {
		// Default is LAN-reachable so hourglass.local works out of the box
		// from any device (the Home Assistant model). Loopback stays
		// available via an explicit HOURGLASS_BIND=127.0.0.1:8080. Credentials
		// are auto-generated + printed on first run when exposed (see
		// ensureCredentialsForBind below), so the default is never an open
		// server.
		addr = "0.0.0.0:8080"
	}

	// LAN-exposed instances must be protected: if no credentials are
	// configured, generate + persist a random password (printed below) so the
	// default install is usable AND secure. Loopback binds stay auth-free.
	authUser, authPass, err := ensureCredentialsForBind(addr)
	if err != nil {
		log.Fatalf("Refusing to start: %v", err)
	}

	if err := enforceBindSecurity(addr); err != nil {
		log.Fatalf("Refusing to start: %v", err)
	}

	// Local HTTPS: generates a per-machine root CA on first run and
	// installs it into the OS trust store so https://hourglass.local
	// shows a valid lock (see tls.go — public CAs can't issue for .local).
	// Returns nil when TLS is off, or serveTLS=false to fall back to HTTP.
	tlsSetup := setupTLS()
	secure := tlsSetup != nil && tlsSetup.serveTLS
	if tlsSetup != nil {
		// The root CA cert is public material — expose it so other devices
		// can fetch and trust it to get a valid lock from their browsers too.
		http.HandleFunc("/ca.pem", handleCAPEM(tlsSetup.caFile))
	}
	startMDNS(addr, secure)

	scheme := "http"
	if secure {
		scheme = "https"
	}
	log.Printf("Starting Hourglass v%s on %s (%s)", version(), addr, scheme)
	if authUser != "" {
		log.Printf("Login: %s / %s (saved in ~/.hourglass/auth.env)", authUser, authPass)
		log.Printf("Reachable on your LAN at %s://hourglass.local:%s", scheme, portOf(addr))
	}
	handler := authMiddleware(http.DefaultServeMux)
	if secure {
		// Serve HTTPS on addr; plain-HTTP requests on the same port get a
		// 308 redirect to https:// (so http://localhost:8080 just works
		// instead of being dropped by the TLS listener).
		if err := serveTLSWithRedirect(addr, tlsSetup.certFile, tlsSetup.keyFile, handler); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
		return
	}
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(toJSON(map[string]string{"version": version(), "goos": runtime.GOOS})))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	content, err := uiFS.ReadFile("ui/index.html")
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

func handleCron(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "GET":
		handleGetCron(w, r)
	case "POST":
		handlePostCron(w, r)
	case "DELETE":
		handleDeleteCron(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetCron(w http.ResponseWriter, r *http.Request) {
	entries, err := cronManager.GetEntries()
	if err != nil {
		if strings.Contains(err.Error(), "no crontab found") {
			w.Write([]byte("[]"))
			return
		}
		log.Printf("failed to read crontab: %v", err)
		http.Error(w, toJSON(APIError{"Failed to read crontab"}), http.StatusInternalServerError)
		return
	}

	// Jobs installed outside Hourglass (e.g. via the system crontab, or on a
	// host that was never written through this UI) carry no history marker, so
	// they run but never record LastRun/LastStatus. Wrap them on first read so
	// they start reporting — best-effort: if the write fails the read still
	// succeeds and the jobs simply stay untracked.
	if cron.HasUntrackedActive(entries) {
		if err := cronManager.WriteCrontab(entries); err != nil {
			log.Printf("failed to wrap untracked cron jobs for history tracking: %v", err)
		} else {
			log.Printf("wrapped untracked active cron job(s) so they report LastRun/LastStatus")
		}
	}

	apiEntries := make([]Entry, len(entries))
	for i, e := range entries {
		apiEntries[i] = Entry{
			Schedule: e.Schedule,
			Command:  e.Command,
			Comment:  e.Comment,
			Inactive: e.Inactive,
		}

		if exec := cronManager.GetLastExecution(e.Command); exec != nil {
			ts := exec.Timestamp.Unix() * 1000
			apiEntries[i].LastRun = &ts
			apiEntries[i].LastStatus = exec.Status
			apiEntries[i].LastCode = exec.ExitCode
		}
	}

	w.Write([]byte(toJSON(apiEntries)))
}

func handlePostCron(w http.ResponseWriter, r *http.Request) {
	var req Entry
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, toJSON(APIError{"Invalid JSON"}), http.StatusBadRequest)
		return
	}

	entry := cron.Entry{
		Schedule: req.Schedule,
		Command:  req.Command,
		Comment:  req.Comment,
	}

	if err := cronManager.AddEntry(entry); err != nil {
		if strings.Contains(err.Error(), "invalid") {
			http.Error(w, toJSON(APIError{err.Error()}), http.StatusBadRequest)
			return
		}
		log.Printf("failed to add cron entry (schedule=%q command=%q): %v", entry.Schedule, entry.Command, err)
		http.Error(w, toJSON(APIError{"Failed to add job"}), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(toJSON(map[string]string{"status": "ok"})))
}

func handleDeleteCron(w http.ResponseWriter, r *http.Request) {
	var req DeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, toJSON(APIError{"Invalid JSON"}), http.StatusBadRequest)
		return
	}

	if err := cronManager.DeleteEntry(req.Index); err != nil {
		log.Printf("failed to delete cron entry (index=%d): %v", req.Index, err)
		http.Error(w, toJSON(APIError{err.Error()}), http.StatusBadRequest)
		return
	}

	w.Write([]byte(toJSON(map[string]string{"status": "ok"})))
}

func handleUpdateCron(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Index   int    `json:"index"`
		Schedule string `json:"Schedule"`
		Command  string `json:"Command"`
		Comment  string `json:"Comment,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, toJSON(APIError{"Invalid JSON"}), http.StatusBadRequest)
		return
	}

	entry := cron.Entry{
		Schedule: req.Schedule,
		Command:  req.Command,
		Comment:  req.Comment,
	}

	if err := cronManager.UpdateEntry(req.Index, entry); err != nil {
		if strings.Contains(err.Error(), "invalid") {
			http.Error(w, toJSON(APIError{err.Error()}), http.StatusBadRequest)
			return
		}
		log.Printf("failed to update cron entry (index=%d schedule=%q command=%q): %v", req.Index, entry.Schedule, entry.Command, err)
		http.Error(w, toJSON(APIError{err.Error()}), http.StatusInternalServerError)
		return
	}

	w.Write([]byte(toJSON(map[string]string{"status": "ok"})))
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "GET":
		handleGetConnections(w, r)
	case "POST":
		handleCreateConnection(w, r)
	case "DELETE":
		handleDeleteConnection(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetConnections(w http.ResponseWriter, r *http.Request) {
	configs := connManager.ListConnections()
	activeID := connManager.GetActiveID()

	type response struct {
		Connections []*connection.Config `json:"connections"`
		ActiveID    string               `json:"active_id"`
	}

	w.Write([]byte(toJSON(response{
		Connections: configs,
		ActiveID:    activeID,
	})))
}

func handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Label   string `json:"label"`
		Host    string `json:"host"`
		Port    int    `json:"port"`
		User    string `json:"user"`
		KeyPath string `json:"key_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, toJSON(APIError{"Invalid JSON"}), http.StatusBadRequest)
		return
	}

	cfg := &connection.Config{
		ID:      req.ID,
		Label:   req.Label,
		Host:    req.Host,
		Port:    req.Port,
		User:    req.User,
		KeyPath: req.KeyPath,
	}

	if err := connManager.AddConnection(cfg); err != nil {
		http.Error(w, toJSON(APIError{err.Error()}), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(toJSON(map[string]string{"status": "ok"})))
}

func handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, toJSON(APIError{"Invalid JSON"}), http.StatusBadRequest)
		return
	}

	if err := connManager.RemoveConnection(req.ID); err != nil {
		http.Error(w, toJSON(APIError{err.Error()}), http.StatusBadRequest)
		return
	}

	w.Write([]byte(toJSON(map[string]string{"status": "ok"})))
}

func handleConnectionActive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "GET":
		handleGetActiveConnection(w, r)
	case "POST":
		handleSetActiveConnection(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetActiveConnection(w http.ResponseWriter, r *http.Request) {
	cfg := connManager.GetActive()
	if cfg == nil {
		w.Write([]byte("null"))
		return
	}
	w.Write([]byte(toJSON(cfg)))
}

func handleSetActiveConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, toJSON(APIError{"Invalid JSON"}), http.StatusBadRequest)
		return
	}

	// Remember the previous active connection so a failed remote switch can
	// roll back; otherwise connections.json would claim the remote is active
	// while the executor still serves the previous host (and the UI, keeping
	// its stale list, would show the wrong host's jobs with no way to tell).
	previous := connManager.GetActiveID()

	if err := connManager.SetActive(req.ID); err != nil {
		http.Error(w, toJSON(APIError{err.Error()}), http.StatusBadRequest)
		return
	}

	cfg := connManager.GetActive()
	if cfg != nil && !cfg.IsLocal {
		if err := switchToRemoteConnection(cfg); err != nil {
			_ = connManager.SetActive(previous)
			http.Error(w, toJSON(APIError{err.Error()}), http.StatusInternalServerError)
			return
		}
	} else {
		cronManager.SetExecutor(newLocalExecutor())
	}

	w.Write([]byte(toJSON(map[string]string{"status": "ok"})))
}

func handleConnectionTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Host    string `json:"host"`
		Port    int    `json:"port"`
		User    string `json:"user"`
		KeyPath string `json:"key_path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, toJSON(APIError{"Invalid JSON"}), http.StatusBadRequest)
		return
	}

	if err := sshclient.TestConnection(req.Host, req.Port, req.User, req.KeyPath); err != nil {
		http.Error(w, toJSON(APIError{err.Error()}), http.StatusBadRequest)
		return
	}

	w.Write([]byte(toJSON(map[string]string{"status": "ok"})))
}

func handleExecuteCron(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Index int `json:"index"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, toJSON(APIError{"Invalid JSON"}), http.StatusBadRequest)
		return
	}

	entries, err := cronManager.GetEntries()
	if err != nil {
		http.Error(w, toJSON(APIError{"Failed to get jobs"}), http.StatusInternalServerError)
		return
	}

	if req.Index < 0 || req.Index >= len(entries) {
		http.Error(w, toJSON(APIError{"Invalid job index"}), http.StatusBadRequest)
		return
	}

	job := entries[req.Index]
	// ExecuteForHistory runs the job through the history wrapper so the
	// execution is recorded in the (local or remote) history log and
	// LastRun/LastStatus update immediately, instead of running it raw
	// (which never wrote a record).
	output, err := cronManager.ExecuteForHistory(job.Command)
	if err != nil {
		log.Printf("failed to execute cron job (index=%d command=%q): %v", req.Index, job.Command, err)
		http.Error(w, toJSON(APIError{"Failed to execute: " + err.Error()}), http.StatusInternalServerError)
		return
	}

	w.Write([]byte(toJSON(map[string]string{"status": "ok", "output": output})))
}

func handleToggleCron(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Index int `json:"index"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, toJSON(APIError{"Invalid JSON"}), http.StatusBadRequest)
		return
	}

	entries, err := cronManager.GetEntries()
	if err != nil {
		http.Error(w, toJSON(APIError{"Failed to get jobs"}), http.StatusInternalServerError)
		return
	}

	if req.Index < 0 || req.Index >= len(entries) {
		http.Error(w, toJSON(APIError{"Invalid job index"}), http.StatusBadRequest)
		return
	}

	entries[req.Index].Inactive = !entries[req.Index].Inactive
	if err := cronManager.WriteCrontab(entries); err != nil {
		log.Printf("failed to toggle cron entry (index=%d): %v", req.Index, err)
		http.Error(w, toJSON(APIError{err.Error()}), http.StatusInternalServerError)
		return
	}

	w.Write([]byte(toJSON(map[string]string{"status": "ok"})))
}

func switchToRemoteConnection(cfg *connection.Config) error {
	client, err := sshclient.NewClient(cfg.Host, cfg.Port, cfg.User, cfg.KeyPath)
	if err != nil {
		return err
	}

	if err := client.Connect(); err != nil {
		return err
	}

	cronManager.SetExecutor(client)
	return nil
}

// newLocalExecutor returns the local (non-remote) executor. Under normal
// operation that is LocalExecutor (the system crontab). When
// HOURGLASS_CRONTAB_FILE is set it returns a file-backed executor so isolated
// E2E/integration runs can add/edit/delete/toggle jobs without touching real
// cron jobs. When HOURGLASS_CRONTAB_USER is set, the local executor manages
// that user's crontab (`crontab -u <user>`) instead of the process user's —
// e.g. a root-run service (launchd/systemd) on macOS that should show the
// logged-in user's jobs rather than root's usually-empty crontab.
func newLocalExecutor() cron.Executor {
	if file := os.Getenv("HOURGLASS_CRONTAB_FILE"); file != "" {
		return cron.NewFileExecutor(file)
	}
	le := &cron.LocalExecutor{}
	if user := os.Getenv("HOURGLASS_CRONTAB_USER"); user != "" {
		le.User = user
	}
	return le
}

// logEntryJSON is one decoded, human-readable execution record returned by
// GET /api/logs. The raw history log stores commands as base64 (so the shell
// wrapper never has to quote arbitrary command text); the UI renders these
// decoded fields instead of the opaque "<millis>	<exit>	<base64(cmd)>" lines.
type logEntryJSON struct {
	Timestamp string `json:"timestamp"`
	ExitCode  int    `json:"exitCode"`
	Status    string `json:"status"`
	Command   string `json:"command"`
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read through the current executor so the logs reflect whichever host
	// is connected (local or SSH-remote), not always the host process's own
	// machine like the old os.ReadFile path did.
	content, err := cronManager.ReadHistoryLog()
	if err != nil {
		log.Printf("failed to read history log: %v", err)
		http.Error(w, toJSON(APIError{"Failed to read log file"}), http.StatusInternalServerError)
		return
	}

	logPath := cronManager.HistoryLogPath()

	// Decode the raw records into human-readable entries, newest first.
	// "content" is still included so callers that want the raw log (or a
	// fallback when parsing yields nothing) have it.
	execs := cron.ParseHistoryLog(content)
	entries := make([]logEntryJSON, 0, len(execs))
	for _, e := range execs {
		entries = append(entries, logEntryJSON{
			Timestamp: e.Timestamp.Format(time.RFC3339),
			ExitCode:  e.ExitCode,
			Status:    e.Status,
			Command:   e.Command,
		})
	}

	w.Write([]byte(toJSON(map[string]interface{}{
		"path":    logPath,
		"content": content,
		"entries": entries,
	})))
}
