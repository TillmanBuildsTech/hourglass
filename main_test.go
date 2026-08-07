package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TillmanBuildsTech/hourglass/cron"
)

// fakeCrontabExecutor is an in-memory cron.Executor so handler tests never
// touch the real system crontab (cron.NewManager() would run the actual
// `crontab` binary against whatever machine runs `go test`).
type fakeCrontabExecutor struct {
	crontab string
}

func (e *fakeCrontabExecutor) Execute(command string) (string, error) {
	if command == "crontab -l" {
		if e.crontab == "" {
			return "", fmt.Errorf("no crontab found or permission denied")
		}
		return e.crontab, nil
	}

	if strings.HasPrefix(command, "crontab - << 'EOF'") {
		start := strings.Index(command, "\n")
		end := strings.LastIndex(command, "\nEOF")
		if start == -1 || end == -1 || end < start {
			return "", fmt.Errorf("malformed heredoc")
		}
		e.crontab = command[start+1 : end]
		return "", nil
	}

	return "", nil
}

func newTestCronManager() *cron.Manager {
	return cron.NewManagerWithExecutor(&fakeCrontabExecutor{})
}

// fakeLogExecutor serves the history-log "cat" command from an in-memory
// string (so handler tests can exercise decoded log rendering without
// touching a real $HOME/.hourglass/history.log).
type fakeLogExecutor struct {
	history string
}

func (e *fakeLogExecutor) Execute(command string) (string, error) {
	if strings.Contains(command, "history.log") {
		return e.history, nil
	}
	if command == `printf '%s' "$HOME"` {
		return "/home/test", nil
	}
	return "", nil
}

func TestHandleLogsReturnsDecodedEntries(t *testing.T) {
	// The exact shape a real history.log holds: "<millis>	<exit>	<base64(cmd)>",
	// oldest first on disk (the UI must flip to newest first and decode).
	older := fmt.Sprintf("%d	1	%s", 1754333759000, base64.StdEncoding.EncodeToString([]byte("/usr/bin/failing.sh")))
	newer := fmt.Sprintf("%d	0	%s", 1786137367285, base64.StdEncoding.EncodeToString([]byte("/root/.hermes/scripts/tbt_enrich.sh >> /var/log/tbt/enrich.log 2>&1")))
	cronManager = cron.NewManagerWithExecutor(&fakeLogExecutor{history: older + "\n" + newer + "\n"})

	req := httptest.NewRequest("GET", "/api/logs", nil)
	w := httptest.NewRecorder()

	handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Entries []struct {
			Timestamp string `json:"timestamp"`
			ExitCode  int    `json:"exitCode"`
			Status    string `json:"status"`
			Command   string `json:"command"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Path != "/home/test/.hourglass/history.log" {
		t.Errorf("Path = %q, want %q", resp.Path, "/home/test/.hourglass/history.log")
	}
	if !strings.Contains(resp.Content, older) || !strings.Contains(resp.Content, newer) {
		t.Errorf("raw content should still be included, got %q", resp.Content)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(resp.Entries))
	}

	// Newest first, with the base64 command decoded.
	first := resp.Entries[0]
	if first.Command != "/root/.hermes/scripts/tbt_enrich.sh >> /var/log/tbt/enrich.log 2>&1" {
		t.Errorf("first entry command = %q, want the decoded enrich command", first.Command)
	}
	if first.ExitCode != 0 || first.Status != "success" {
		t.Errorf("first entry = %+v, want success", first)
	}
	if first.Timestamp == "" || !strings.Contains(first.Timestamp, "T") {
		t.Errorf("first entry timestamp = %q, want RFC3339", first.Timestamp)
	}

	second := resp.Entries[1]
	if second.Command != "/usr/bin/failing.sh" || second.ExitCode != 1 || second.Status != "failed" {
		t.Errorf("second entry = %+v, want the failed record", second)
	}
}

func TestHandleLogsEmpty(t *testing.T) {
	cronManager = cron.NewManagerWithExecutor(&fakeLogExecutor{history: ""})

	req := httptest.NewRequest("GET", "/api/logs", nil)
	w := httptest.NewRecorder()

	handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Entries []struct {
			Command string `json:"command"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Entries) != 0 {
		t.Errorf("expected 0 entries for an empty log, got %+v", resp.Entries)
	}
	if resp.Path == "" {
		t.Errorf("expected a non-empty log path")
	}
}

func TestHandleLogsMethodNotAllowed(t *testing.T) {
	cronManager = cron.NewManagerWithExecutor(&fakeLogExecutor{history: ""})

	req := httptest.NewRequest("POST", "/api/logs", nil)
	w := httptest.NewRecorder()

	handleLogs(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestVersion(t *testing.T) {
	v := version()
	if v == "" || v == "unknown" {
		t.Errorf("expected a non-empty embedded version, got %q", v)
	}
	if strings.ContainsAny(v, " \n\t") {
		t.Errorf("expected version to be trimmed, got %q", v)
	}
}

func TestHandleVersion(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/version", nil)
	w := httptest.NewRecorder()

	handleVersion(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["version"] != version() {
		t.Errorf("version = %q, want %q", resp["version"], version())
	}
}

func TestHandleRoot(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handleRoot(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "Hourglass") {
		t.Errorf("Response missing 'Hourglass' content")
	}

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Expected text/html content type, got %s", ct)
	}
}

func TestHandleRootNotFound(t *testing.T) {
	req := httptest.NewRequest("GET", "/notfound", nil)
	w := httptest.NewRecorder()

	handleRoot(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestHandleGetCronEmpty(t *testing.T) {
	cronManager = newTestCronManager()

	req := httptest.NewRequest("GET", "/api/cron", nil)
	w := httptest.NewRecorder()

	handleGetCron(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var entries []Entry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
}

func TestHandlePostCronValid(t *testing.T) {
	cronManager = newTestCronManager()

	body := Entry{
		Schedule: "0 9 * * *",
		Command:  "/usr/bin/test.sh",
		Comment:  "Test job",
	}

	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/cron", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlePostCron(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", w.Code)
	}
}

func TestHandlePostCronInvalidJSON(t *testing.T) {
	cronManager = newTestCronManager()

	req := httptest.NewRequest("POST", "/api/cron", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlePostCron(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestHandlePostCronInvalidSchedule(t *testing.T) {
	cronManager = newTestCronManager()

	body := Entry{
		Schedule: "60 9 * * *",
		Command:  "/usr/bin/test.sh",
	}

	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/cron", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlePostCron(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	var errResp APIError
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp.Error == "" {
		t.Errorf("Expected error message in response")
	}
}

func TestHandleDeleteCronInvalidJSON(t *testing.T) {
	cronManager = newTestCronManager()

	req := httptest.NewRequest("DELETE", "/api/cron", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleDeleteCron(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestCronMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("PUT", "/api/cron", nil)
	w := httptest.NewRecorder()

	handleCron(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestToJSON(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
	}{
		{APIError{"test error"}, `{"error":"test error"}`},
		{map[string]string{"status": "ok"}, `{"status":"ok"}`},
	}

	for _, tt := range tests {
		result := toJSON(tt.input)
		if result != tt.expected {
			t.Errorf("toJSON(%v) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
