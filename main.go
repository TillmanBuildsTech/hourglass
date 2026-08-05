package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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

	cronManager = cron.NewManager()
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

	distFS, _ := fs.Sub(uiFS, "ui/dist")
	http.Handle("/dist/", http.StripPrefix("/dist/", http.FileServer(http.FS(distFS))))
	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/api/version", handleVersion)
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
		addr = "127.0.0.1:8080"
	}

	log.Printf("Starting Hourglass v%s on %s", version(), addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
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

	if err := connManager.SetActive(req.ID); err != nil {
		http.Error(w, toJSON(APIError{err.Error()}), http.StatusBadRequest)
		return
	}

	cfg := connManager.GetActive()
	if cfg != nil && !cfg.IsLocal {
		if err := switchToRemoteConnection(cfg); err != nil {
			http.Error(w, toJSON(APIError{err.Error()}), http.StatusInternalServerError)
			return
		}
	} else {
		cronManager.SetExecutor(&cron.LocalExecutor{})
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
	output, err := cronManager.ExecuteCommand(job.Command)
	if err != nil {
		log.Printf("failed to execute cron job (index=%d command=%q): %v", req.Index, job.Command, err)
		http.Error(w, toJSON(APIError{"Failed to execute: " + err.Error()}), http.StatusInternalServerError)
		return
	}

	// Refresh history immediately so the next GET /api/cron picks
	// up the new execution result without waiting for the background
	// 30-second ticker.
	cronManager.RefreshHistory()

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

func handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, toJSON(APIError{"Failed to resolve home directory"}), http.StatusInternalServerError)
		return
	}

	logPath := filepath.Join(home, ".hourglass", "history.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.Write([]byte(toJSON(map[string]string{"content": "", "path": logPath})))
			return
		}
		http.Error(w, toJSON(APIError{"Failed to read log file: " + err.Error()}), http.StatusInternalServerError)
		return
	}

	w.Write([]byte(toJSON(map[string]string{"content": string(content), "path": logPath})))
}
