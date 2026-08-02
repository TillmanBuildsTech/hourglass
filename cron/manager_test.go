package cron

import (
	"fmt"
	"strings"
	"testing"
)

// memExecutor is an in-memory fake of Executor that emulates `crontab -l`
// / `crontab - <<'EOF' ... EOF` well enough to exercise WriteCrontab and
// GetEntries end-to-end without touching a real crontab.
type memExecutor struct {
	crontab string
}

func (e *memExecutor) Execute(command string) (string, error) {
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

func TestWriteCrontabWrapsCommandForHistory(t *testing.T) {
	exec := &memExecutor{}
	m := NewManagerWithExecutor(exec)

	entry := Entry{
		Schedule: "* * * * *",
		Command:  "/bin/true",
		Comment:  "test",
	}

	if err := m.AddEntry(entry); err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}

	if !strings.Contains(exec.crontab, "history.log") {
		t.Errorf("expected the raw crontab entry to log execution history, got: %s", exec.crontab)
	}

	entries, err := m.GetEntries()
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Command != "/bin/true" {
		t.Errorf("Command = %q, want the unwrapped original /bin/true", entries[0].Command)
	}
	if entries[0].Comment != "test" {
		t.Errorf("Comment = %q, want %q", entries[0].Comment, "test")
	}
}

func TestWriteCrontabDoesNotWrapInactiveEntries(t *testing.T) {
	exec := &memExecutor{}
	m := NewManagerWithExecutor(exec)

	entries := []Entry{
		{Schedule: "* * * * *", Command: "/bin/true", Inactive: true},
	}

	if err := m.WriteCrontab(entries); err != nil {
		t.Fatalf("WriteCrontab failed: %v", err)
	}

	if !strings.Contains(exec.crontab, "# * * * * * /bin/true") {
		t.Errorf("expected inactive entry to remain unwrapped, got: %s", exec.crontab)
	}
	if strings.Contains(exec.crontab, "history.log") {
		t.Errorf("inactive entries should not log execution history, got: %s", exec.crontab)
	}
}

func TestUnwrapEntryLeavesUnmarkedEntriesUnchanged(t *testing.T) {
	entry := Entry{Schedule: "* * * * *", Command: "/bin/true", Comment: "plain comment"}

	result := unwrapEntry(entry)
	if result != entry {
		t.Errorf("expected unmarked entry to be unchanged, got %+v", result)
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
