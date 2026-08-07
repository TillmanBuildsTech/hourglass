package cron

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeHistoryExecutor returns a fixed output for the history log "cat"
// command issued by Refresh, ignoring all other commands.
type fakeHistoryExecutor struct {
	output string
	err    error
}

func (f *fakeHistoryExecutor) Execute(command string) (string, error) {
	return f.output, f.err
}

func encodeCmd(cmd string) string {
	return base64.StdEncoding.EncodeToString([]byte(cmd))
}

func historyLine(ts time.Time, exitCode int, cmd string) string {
	return fmt.Sprintf("%d\t%d\t%s", ts.UnixMilli(), exitCode, encodeCmd(cmd))
}

func TestHistoryCacheGet(t *testing.T) {
	cache := NewHistoryCache(1 * time.Second)

	if result := cache.Get("test"); result != nil {
		t.Errorf("Expected nil for empty cache, got %v", result)
	}
}

func TestHistoryCacheTTL(t *testing.T) {
	cache := NewHistoryCache(10 * time.Millisecond)

	cache.mu.Lock()
	cache.entries["test"] = &Execution{
		Command:  "test",
		ExitCode: 0,
		Status:   "success",
	}
	cache.lastUpdate = time.Now()
	cache.mu.Unlock()

	if result := cache.Get("test"); result == nil {
		t.Errorf("Expected execution, got nil")
	}

	time.Sleep(20 * time.Millisecond)

	if result := cache.Get("test"); result != nil {
		t.Errorf("Expected nil after TTL expiration, got %v", result)
	}
}

func TestHistoryCacheInvalidate(t *testing.T) {
	cache := NewHistoryCache(1 * time.Hour)

	if err := cache.Refresh(&fakeHistoryExecutor{output: historyLine(time.Now(), 0, "/usr/bin/job") + "\n"}); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if exec := cache.Get("/usr/bin/job"); exec == nil {
		t.Fatal("expected execution after Refresh")
	}

	cache.Invalidate()

	// After Invalidate the cache must behave as empty so the next Get forces
	// a re-read through the current executor (which may be a different host).
	if exec := cache.Get("/usr/bin/job"); exec != nil {
		t.Fatalf("expected nil after Invalidate, got %+v", exec)
	}
}

func TestNormalizeCommand(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/usr/bin/backup.sh", "/usr/bin/backup.sh"},
		{"  /usr/bin/backup.sh  ", "/usr/bin/backup.sh"},
		{"/usr/bin/backup.sh --flag", "/usr/bin/backup.sh --flag"},
	}

	for _, tt := range tests {
		result := normalizeCommand(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeCommand(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestParseHistoryLine(t *testing.T) {
	ts := time.UnixMilli(1753680000000)

	tests := []struct {
		name       string
		line       string
		wantNil    bool
		wantCmd    string
		wantCode   int
		wantStatus string
	}{
		{
			name:       "success",
			line:       historyLine(ts, 0, "/usr/bin/test.sh"),
			wantCmd:    "/usr/bin/test.sh",
			wantCode:   0,
			wantStatus: "success",
		},
		{
			name:       "failure",
			line:       historyLine(ts, 1, "/usr/bin/failed.sh"),
			wantCmd:    "/usr/bin/failed.sh",
			wantCode:   1,
			wantStatus: "failed",
		},
		{
			name:    "empty line",
			line:    "",
			wantNil: true,
		},
		{
			name:    "malformed - too few fields",
			line:    "12345\t0",
			wantNil: true,
		},
		{
			name:    "malformed - bad timestamp",
			line:    "not-a-number\t0\t" + encodeCmd("x"),
			wantNil: true,
		},
		{
			name:    "malformed - bad base64",
			line:    "12345\t0\t!!!not-base64!!!",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseHistoryLine(tt.line)
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatalf("expected a result, got nil")
			}
			if result.Command != tt.wantCmd {
				t.Errorf("Command = %q, want %q", result.Command, tt.wantCmd)
			}
			if result.ExitCode != tt.wantCode {
				t.Errorf("ExitCode = %d, want %d", result.ExitCode, tt.wantCode)
			}
			if result.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", result.Status, tt.wantStatus)
			}
			if !result.Timestamp.Equal(ts) {
				t.Errorf("Timestamp = %v, want %v", result.Timestamp, ts)
			}
		})
	}
}

func TestNewHistoryCache(t *testing.T) {
	cache := NewHistoryCache(1 * time.Minute)
	if cache == nil {
		t.Errorf("NewHistoryCache returned nil")
	}
	if cache.ttl != 1*time.Minute {
		t.Errorf("TTL not set correctly: got %v, want 1m", cache.ttl)
	}
}

func TestExecutionStructure(t *testing.T) {
	exec := &Execution{
		Timestamp: time.Now(),
		Command:   "/usr/bin/test.sh",
		ExitCode:  0,
		Status:    "success",
	}

	if exec.Command != "/usr/bin/test.sh" {
		t.Errorf("Command not set correctly")
	}

	if exec.ExitCode != 0 {
		t.Errorf("ExitCode not set correctly")
	}

	if exec.Status != "success" {
		t.Errorf("Status not set correctly")
	}
}

func TestHistoryCacheRefreshFromExecutor(t *testing.T) {
	older := time.UnixMilli(1753680000000)
	newer := older.Add(1 * time.Minute)

	output := strings.Join([]string{
		historyLine(older, 1, "/usr/bin/backup.sh"),
		historyLine(newer, 0, "/usr/bin/backup.sh"),
		historyLine(older, 0, "/usr/bin/other.sh"),
		"",
	}, "\n")

	cache := NewHistoryCache(1 * time.Minute)
	executor := &fakeHistoryExecutor{output: output}

	if err := cache.Refresh(executor); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	result := cache.Get("/usr/bin/backup.sh")
	if result == nil {
		t.Fatalf("expected an execution for backup.sh")
	}
	if result.ExitCode != 0 || result.Status != "success" {
		t.Errorf("expected the newer (successful) record to win, got %+v", result)
	}
	if !result.Timestamp.Equal(newer) {
		t.Errorf("Timestamp = %v, want %v", result.Timestamp, newer)
	}

	other := cache.Get("/usr/bin/other.sh")
	if other == nil || other.ExitCode != 0 {
		t.Errorf("expected other.sh execution, got %+v", other)
	}

	if cache.Get("/usr/bin/unknown.sh") != nil {
		t.Errorf("expected nil for command with no history")
	}
}

func TestHistoryCacheRefreshError(t *testing.T) {
	cache := NewHistoryCache(1 * time.Minute)
	executor := &fakeHistoryExecutor{err: fmt.Errorf("boom")}

	if err := cache.Refresh(executor); err == nil {
		t.Errorf("expected Refresh to propagate the executor error")
	}
}

func TestGetLastExecutionUsesExecutor(t *testing.T) {
	ts := time.UnixMilli(1753680000000)
	m := NewManagerWithExecutor(&fakeHistoryExecutor{
		output: historyLine(ts, 0, "/usr/bin/backup.sh"),
	})

	exec := m.GetLastExecution("/usr/bin/backup.sh")
	if exec == nil {
		t.Fatalf("expected an execution to be found")
	}
	if exec.Status != "success" {
		t.Errorf("Status = %q, want success", exec.Status)
	}

	if m.GetLastExecution("/usr/bin/missing.sh") != nil {
		t.Errorf("expected nil for a command that never ran")
	}
}
