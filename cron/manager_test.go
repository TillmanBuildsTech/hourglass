package cron

import (
	"fmt"
	"os/exec"
	"path/filepath"
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

func TestLocalExecutorUsesCrontab(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Error("NewManager returned nil")
	}
}

func TestFileExecutorRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crontab.txt")
	e := NewFileExecutor(path)

	// Empty (no file) behaves like "no crontab".
	if _, err := e.Execute("crontab -l"); err == nil {
		t.Error("expected error for empty crontab, got nil")
	}

	// Write via the same heredoc form WriteCrontab produces.
	cmd := "crontab - << 'EOF'\n* * * * * /usr/bin/hello\nEOF"
	if _, err := e.Execute(cmd); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	out, err := e.Execute("crontab -l")
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(out, "/usr/bin/hello") {
		t.Errorf("expected job in read-back, got %q", out)
	}

	// Delete.
	if _, err := e.Execute("crontab -r"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := e.Execute("crontab -l"); err == nil {
		t.Error("expected error after delete, got nil")
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

func TestWrapCommandForHistoryProducesPortableTimestamp(t *testing.T) {
	wrapped := wrapCommandForHistory("/bin/true")

	// Must not depend on GNU-only "date +%s%3N" directly; must instead
	// probe with the digit-checking fallback so it degrades gracefully
	// on BSD date (macOS).
	if !strings.Contains(wrapped, "date +%3N") {
		t.Errorf("expected portable date probe in wrapper, got: %s", wrapped)
	}
	if !strings.Contains(wrapped, `[!0-9]`) {
		t.Errorf("expected digit-check fallback (not an exit-status probe) in wrapper, got: %s", wrapped)
	}

	if !strings.Contains(wrapped, "history.log") {
		t.Errorf("wrapper must log to history.log: %s", wrapped)
	}
}

// TestTimestampExprIsPortable actually executes timestampExpr via sh -c on
// this machine and verifies it produces a plausible millisecond unix
// timestamp (all digits, right length) regardless of which date flavor
// (GNU or BSD) is installed.
func TestTimestampExprIsPortable(t *testing.T) {
	out, err := exec.Command("sh", "-c", "printf '%s' "+timestampExpr).Output()
	if err != nil {
		t.Fatalf("timestampExpr failed to execute: %v", err)
	}

	ms := strings.TrimSpace(string(out))
	if ms == "" {
		t.Fatal("timestampExpr produced empty output")
	}
	for _, r := range ms {
		if r < '0' || r > '9' {
			t.Fatalf("timestampExpr produced non-digit output: %q", ms)
		}
	}
	if len(ms) < 12 || len(ms) > 14 {
		t.Fatalf("timestampExpr output %q does not look like a millisecond unix timestamp", ms)
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

// recordingExecutor captures every executed command, so tests can assert that
// Manager reads/writes go through the current executor (which is exactly what
// makes SSH-remote connections work - the executor is the ssh.Client).
type recordingExecutor struct {
	commands []string
	output   string
	err      error
}

func (r *recordingExecutor) Execute(command string) (string, error) {
	r.commands = append(r.commands, command)
	return r.output, r.err
}

func TestReadHistoryLogRoutesThroughExecutor(t *testing.T) {
	exec := &recordingExecutor{output: "1780000000000	0	abc\n"}
	m := NewManagerWithExecutor(exec)

	content, err := m.ReadHistoryLog()
	if err != nil {
		t.Fatalf("ReadHistoryLog failed: %v", err)
	}
	if content != exec.output {
		t.Errorf("ReadHistoryLog = %q, want %q", content, exec.output)
	}
	if len(exec.commands) != 1 || !strings.Contains(exec.commands[0], "history.log") {
		t.Errorf("ReadHistoryLog should read via the executor, got commands: %v", exec.commands)
	}
}

func TestHistoryLogPathResolvesViaExecutor(t *testing.T) {
	exec := &recordingExecutor{output: "/home/remoteuser"}
	m := NewManagerWithExecutor(exec)

	path := m.HistoryLogPath()
	if path != "/home/remoteuser/.hourglass/history.log" {
		t.Errorf("HistoryLogPath = %q, want %q", path, "/home/remoteuser/.hourglass/history.log")
	}
}

func TestExecuteForHistoryWrapsAndRefreshes(t *testing.T) {
	exec := &recordingExecutor{}
	m := NewManagerWithExecutor(exec)

	if _, err := m.ExecuteForHistory("/usr/bin/backup.sh"); err != nil {
		t.Fatalf("ExecuteForHistory failed: %v", err)
	}

	// The command must be run wrapped (so a record lands in the history log
	// and LastRun/LastStatus populate), and the cache must be re-read via the
	// same executor afterwards.
	var sawWrap, sawRead bool
	for _, c := range exec.commands {
		if strings.Contains(c, "history.log") && strings.Contains(c, "printf") {
			sawWrap = true
		}
		if strings.Contains(c, "cat") && strings.Contains(c, "history.log") {
			sawRead = true
		}
	}
	if !sawWrap {
		t.Errorf("ExecuteForHistory should run the wrapped (logging) command, got commands: %v", exec.commands)
	}
	if !sawRead {
		t.Errorf("ExecuteForHistory should refresh history via the executor, got commands: %v", exec.commands)
	}
}
