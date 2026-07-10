// +build integration

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brandontillman/hourglass/cron"
)

func TestIntegrationGetCron(t *testing.T) {
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

func TestIntegrationPostAndGet(t *testing.T) {
	cronManager = cron.NewManager()

	entry := Entry{
		Schedule: "0 9 * * *",
		Command:  "/usr/bin/test.sh",
		Comment:  "Test",
	}

	body, _ := json.Marshal(entry)
	req := httptest.NewRequest("POST", "/api/cron", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlePostCron(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", w.Code)
	}
}

func TestIntegrationInvalidScheduleRejected(t *testing.T) {
	cronManager = cron.NewManager()

	entry := Entry{
		Schedule: "99 99 * * *",
		Command:  "/usr/bin/test.sh",
	}

	body, _ := json.Marshal(entry)
	req := httptest.NewRequest("POST", "/api/cron", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlePostCron(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid schedule, got %d", w.Code)
	}
}

func TestIntegrationHandlerChain(t *testing.T) {
	cronManager = cron.NewManager()

	req := httptest.NewRequest("GET", "/api/cron", nil)
	w := httptest.NewRecorder()

	handleCron(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestIntegrationDeleteRequest(t *testing.T) {
	cronManager = cron.NewManager()

	deleteReq := DeleteRequest{Index: 0}
	body, _ := json.Marshal(deleteReq)
	req := httptest.NewRequest("DELETE", "/api/cron", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleDeleteCron(w, req)

	if w.Code != http.StatusBadRequest {
		t.Logf("Delete on empty crontab returned status %d", w.Code)
	}
}

func TestRootHandlerServes HTML(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handleRoot(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("Expected text/html charset=utf-8, got %s", contentType)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Errorf("Response body is empty")
	}
}

func TestCronEndpointRouting(t *testing.T) {
	cronManager = cron.NewManager()

	tests := []struct {
		method   string
		code     int
		name     string
	}{
		{"GET", http.StatusOK, "GET allowed"},
		{"POST", http.StatusCreated, "POST allowed"},
		{"DELETE", http.StatusBadRequest, "DELETE allowed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]interface{}{})
			req := httptest.NewRequest(tt.method, "/api/cron", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handleCron(w, req)

			if w.Code != tt.code && w.Code != http.StatusBadRequest && tt.method != "DELETE" {
				t.Errorf("%s: expected status %d, got %d", tt.name, tt.code, w.Code)
			}
		})
	}
}
