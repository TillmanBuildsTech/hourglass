package cron

import (
	"testing"
	"time"
)

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

func TestNormalizeCommand(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/usr/bin/backup.sh", "/usr/bin/backup.sh"},
		{"  /usr/bin/backup.sh  ", "/usr/bin/backup.sh"},
		{"/USR/BIN/BACKUP.SH", "/usr/bin/backup.sh"},
		{"COMMAND", "command"},
	}

	for _, tt := range tests {
		result := normalizeCommand(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeCommand(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestExtractCommand(t *testing.T) {
	tests := []struct {
		message  string
		expected string
	}{
		{"CMD (/usr/bin/backup.sh)", "/usr/bin/backup.sh"},
		{"root CMD (/usr/bin/backup.sh)", "/usr/bin/backup.sh"},
		{"CRON[1234]: CMD (/bin/test)", "/bin/test"},
		{"No command here", ""},
		{"CMD (/incomplete", ""},
	}

	for _, tt := range tests {
		result := extractCommand(tt.message)
		if result != tt.expected {
			t.Errorf("extractCommand(%q) = %q, want %q", tt.message, result, tt.expected)
		}
	}
}

func TestParseJournalEntry(t *testing.T) {
	tests := []struct {
		entry     JournalEntry
		wantCmd   bool
		wantCode  int
		wantStatus string
	}{
		{
			JournalEntry{
				Message:  "CMD (/usr/bin/test.sh)",
				Priority: "6",
				ExitCode: "0",
			},
			true,
			0,
			"success",
		},
		{
			JournalEntry{
				Message:  "CMD (/usr/bin/failed.sh)",
				Priority: "3",
				ExitCode: "1",
			},
			true,
			1,
			"failed",
		},
		{
			JournalEntry{
				Message:  "Not a CRON entry",
				Priority: "6",
			},
			false,
			0,
			"",
		},
	}

	for i, tt := range tests {
		result := parseJournalEntry(tt.entry)
		if (result != nil) != tt.wantCmd {
			t.Errorf("Test %d: parseJournalEntry expected command=%v, got %v", i, tt.wantCmd, result != nil)
		}

		if tt.wantCmd {
			if result.ExitCode != tt.wantCode {
				t.Errorf("Test %d: exit code = %d, want %d", i, result.ExitCode, tt.wantCode)
			}
			if result.Status != tt.wantStatus {
				t.Errorf("Test %d: status = %q, want %q", i, result.Status, tt.wantStatus)
			}
		}
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
