package main

import (
	"bytes"
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
