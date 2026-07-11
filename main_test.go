package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TillmanBuildsTech/hourglass/cron"
)

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
	cronManager = cron.NewManager()

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
	cronManager = cron.NewManager()

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
	cronManager = cron.NewManager()

	req := httptest.NewRequest("POST", "/api/cron", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlePostCron(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestHandlePostCronInvalidSchedule(t *testing.T) {
	cronManager = cron.NewManager()

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
	cronManager = cron.NewManager()

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
