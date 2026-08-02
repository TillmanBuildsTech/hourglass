package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/TillmanBuildsTech/hourglass/connection"
	"github.com/TillmanBuildsTech/hourglass/cron"
	sshclient "github.com/TillmanBuildsTech/hourglass/ssh"
)

//go:embed ui/index.html
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
	showVersion := flag.Bool("version", false, "print the Hourglass version and exit")
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

	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/api/version", handleVersion)
	http.HandleFunc("/api/cron", handleCron)
	http.HandleFunc("/api/connections", handleConnections)
	http.HandleFunc("/api/connections/active", handleConnectionActive)
	http.HandleFunc("/api/connections/test", handleConnectionTest)

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
	w.Write([]byte(toJSON(map[string]string{"version": version()})))
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
		http.Error(w, toJSON(APIError{"Failed to read crontab"}), http.StatusInternalServerError)
		return
	}

	apiEntries := make([]Entry, len(entries))
	for i, e := range entries {
		apiEntries[i] = Entry{
			Schedule: e.Schedule,
			Command:  e.Command,
			Comment:  e.Comment,
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
		http.Error(w, toJSON(APIError{err.Error()}), http.StatusBadRequest)
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
