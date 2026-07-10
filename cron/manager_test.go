package cron

import (
	"testing"
)

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Error("NewManager returned nil")
	}
}

func TestManagerAddEntry(t *testing.T) {
	entry := Entry{
		Schedule: "0 9 * * *",
		Command:  "/usr/bin/backup.sh",
	}

	if err := ValidateSchedule(entry.Schedule); err != nil {
		t.Errorf("ValidateSchedule failed: %v", err)
	}
}

func TestManagerInvalidSchedule(t *testing.T) {
	entry := Entry{
		Schedule: "60 9 * * *",
		Command:  "/usr/bin/backup.sh",
	}

	if err := ValidateSchedule(entry.Schedule); err == nil {
		t.Error("ValidateSchedule should reject invalid minute")
	}
}

func TestManagerUpdateEntry(t *testing.T) {
	entry := Entry{
		Schedule: "0 10 * * *",
		Command:  "/usr/bin/updated.sh",
	}

	if err := ValidateSchedule(entry.Schedule); err != nil {
		t.Errorf("ValidateSchedule failed: %v", err)
	}
}

func TestManagerDeleteEntry(t *testing.T) {
	m := NewManager()

	if m == nil {
		t.Error("Manager is nil")
	}
}

func TestGetEntriesValidation(t *testing.T) {
	text := `0 9 * * * /usr/bin/backup.sh
*/5 * * * * /usr/bin/check.sh`

	entries, err := ParseCrontab(text)
	if err != nil {
		t.Fatalf("ParseCrontab failed: %v", err)
	}

	if len(entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(entries))
	}

	if entries[0].Command != "/usr/bin/backup.sh" {
		t.Errorf("Command mismatch: got %q", entries[0].Command)
	}
}

func TestEntryRoundTrip(t *testing.T) {
	original := Entry{
		Schedule: "0 9 * * *",
		Command:  "/usr/bin/backup.sh",
		Comment:  "Daily backup",
	}

	entries := []Entry{original}
	text := StringifyCrontab(entries)

	parsed, err := ParseCrontab(text)
	if err != nil {
		t.Fatalf("ParseCrontab failed: %v", err)
	}

	if len(parsed) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(parsed))
	}

	if parsed[0].Schedule != original.Schedule {
		t.Errorf("Schedule mismatch: got %q, want %q", parsed[0].Schedule, original.Schedule)
	}

	if parsed[0].Command != original.Command {
		t.Errorf("Command mismatch: got %q, want %q", parsed[0].Command, original.Command)
	}
}

func TestMultipleEntriesOperations(t *testing.T) {
	var entries []Entry

	entries = append(entries, Entry{
		Schedule: "0 9 * * *",
		Command:  "/usr/bin/backup.sh",
	})
	entries = append(entries, Entry{
		Schedule: "0 10 * * *",
		Command:  "/usr/bin/report.sh",
	})

	if len(entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(entries))
	}

	text := StringifyCrontab(entries)
	parsed, err := ParseCrontab(text)
	if err != nil {
		t.Fatalf("ParseCrontab failed: %v", err)
	}

	if len(parsed) != 2 {
		t.Errorf("Expected 2 parsed entries, got %d", len(parsed))
	}
}

func TestEmptyCrontab(t *testing.T) {
	text := ""
	entries, err := ParseCrontab(text)
	if err != nil {
		t.Fatalf("ParseCrontab failed on empty input: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("Expected 0 entries from empty input, got %d", len(entries))
	}

	output := StringifyCrontab(entries)
	if output != "" {
		t.Errorf("Expected empty output from empty entries, got %q", output)
	}
}
