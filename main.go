package main

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/TillmanBuildsTech/hourglass/cron"
)

//go:embed ui/index.html
var uiFS embed.FS

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

func main() {
	cronManager = cron.NewManager()
	cronManager.StartHistoryRefresh()

	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/api/cron", handleCron)

	addr := os.Getenv("HOURGLASS_BIND")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	log.Printf("Starting Hourglass on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
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
