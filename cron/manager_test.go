package cron

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"os/user"
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

// TestLocalExecutorUserTargetsCrontab verifies that a LocalExecutor with User
// set rewrites crontab invocations to target that user via `crontab -u
// <user>` — the mechanism behind HOURGLASS_CRONTAB_USER (e.g. a root-run
// instance on macOS that should manage the logged-in user's crontab instead
// of root's empty one). A fake `crontab` script on PATH records the argv it
// receives so no real system crontab is touched.
func TestLocalExecutorUserTargetsCrontab(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.log")
	script := filepath.Join(dir, "crontab")
	// Fake crontab: append argv to argsFile, exit 0.
	content := "#!/bin/sh\necho \"$@\" >> \"" + argsFile + "\"\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake crontab: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	readArgs := func() []string {
		data, err := os.ReadFile(argsFile)
		if err != nil {
			return nil
		}
		return strings.Fields(string(data))
	}

	e := &LocalExecutor{User: "alice"}

	// Read: must become `crontab -u alice -l`.
	if _, err := e.Execute("crontab -l"); err != nil {
		t.Fatalf("Execute(crontab -l) failed: %v", err)
	}
	if got := readArgs(); len(got) != 3 || got[0] != "-u" || got[1] != "alice" || got[2] != "-l" {
		t.Errorf("crontab -l args = %v, want [-u alice -l]", got)
	}

	// Write: heredoc form must become `crontab -u alice - << 'EOF'...`.
	os.Remove(argsFile)
	if _, err := e.Execute("crontab - << 'EOF'\n* * * * * /bin/true\nEOF"); err != nil {
		t.Fatalf("Execute(write) failed: %v", err)
	}
	if got := readArgs(); len(got) != 3 || got[0] != "-u" || got[1] != "alice" || got[2] != "-" {
		t.Errorf("crontab write args = %v, want [-u alice -]", got)
	}

	// Delete: must become `crontab -u alice -r`.
	os.Remove(argsFile)
	if _, err := e.Execute("crontab -r"); err != nil {
		t.Fatalf("Execute(crontab -r) failed: %v", err)
	}
	if got := readArgs(); len(got) != 3 || got[0] != "-u" || got[1] != "alice" || got[2] != "-r" {
		t.Errorf("crontab -r args = %v, want [-u alice -r]", got)
	}

	// Without a User, commands pass through untouched.
	os.Remove(argsFile)
	plain := &LocalExecutor{}
	if _, err := plain.Execute("crontab -l"); err != nil {
		t.Fatalf("Execute(crontab -l) plain failed: %v", err)
	}
	if got := readArgs(); len(got) != 1 || got[0] != "-l" {
		t.Errorf("plain crontab -l args = %v, want [-l]", got)
	}
}

// TestLocalExecutorUserOverridesHome verifies that when a target user
// resolves to a real home directory, shell commands (history/log reads) run
// with HOME pointed at that user's home rather than the process user's.
func TestLocalExecutorUserOverridesHome(t *testing.T) {
	// Use the current user: `~<user>` always resolves to their home.
	cur, err := user.Current()
	if err != nil {
		t.Skipf("cannot resolve current user: %v", err)
	}
	e := &LocalExecutor{User: cur.Username}
	e.resolveHome()
	if e.home == "" {
		t.Fatal("resolveHome returned empty home for current user")
	}

	env := e.env()
	var gotHome string
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") {
			gotHome = strings.TrimPrefix(kv, "HOME=")
		}
	}
	if gotHome != e.home {
		t.Errorf("env HOME = %q, want %q", gotHome, e.home)
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

// crontabListExecutor emulates the real `crontab -l` / `crontab - << 'EOF'`
// round trip faithfully: a heredoc write leaves a trailing newline in the
// stored crontab (as the real `crontab -` does), and reads return it (as the
// real `crontab -l` output does). This is what makes blank-line accumulation
// reproducible — the file-backed FileExecutor strips the trailing newline on
// write, so it cannot reproduce the bug.
type crontabListExecutor struct {
	crontab string
}

func (e *crontabListExecutor) Execute(command string) (string, error) {
	if command == "crontab -l" {
		if e.crontab == "" {
			return "", fmt.Errorf("no crontab found or permission denied")
		}
		return e.crontab + "\n", nil
	}

	if strings.HasPrefix(command, "crontab - << 'EOF'") {
		start := strings.Index(command, "\n")
		end := strings.LastIndex(command, "\nEOF")
		if start == -1 || end == -1 || end < start {
			return "", fmt.Errorf("malformed heredoc")
		}
		text := command[start+1 : end]
		e.crontab = text + "\n"
		return "", nil
	}

	return "", nil
}

// TestWriteCrontabRoundTripStable guards against blank-line accumulation: the
// trailing newline of `crontab -l` output is captured as a preserved blank
// line and, if re-emitted verbatim, grows the crontab by one blank line on
// every write. The written text must be byte-identical across round trips.
func TestWriteCrontabRoundTripStable(t *testing.T) {
	exec := &crontabListExecutor{crontab: "# header\nPATH=/usr/bin:/bin\n# comment\n\n0 9 * * * /usr/bin/job.sh\n"}
	m := NewManagerWithExecutor(exec)

	roundTrip := func() string {
		entries, err := m.GetEntries()
		if err != nil {
			t.Fatalf("GetEntries: %v", err)
		}
		if len(entries) != 1 || entries[0].Command != "/usr/bin/job.sh" {
			t.Fatalf("unexpected entries: %+v", entries)
		}
		if err := m.WriteCrontab(entries); err != nil {
			t.Fatalf("WriteCrontab: %v", err)
		}
		return exec.crontab
	}

	first := roundTrip()
	for i := 0; i < 3; i++ {
		if got := roundTrip(); got != first {
			t.Fatalf("crontab changed across round trips:\n--- first ---\n%q\n--- after %d more ---\n%q", first, i+1, got)
		}
	}

	if strings.Contains(first, "\n\n\n") {
		t.Errorf("crontab still contains a run of 2+ blank lines: %q", first)
	}
	if strings.HasPrefix(first, "\n") || strings.HasSuffix(first, "\n\n") {
		t.Errorf("crontab has leading/trailing blank lines: %q", first)
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

// TestSetExecutorInvalidatesHistoryCache guards against a stale-cache leak
// across connection swaps: after switching from one host (executor) to
// another, LastRun/LastStatus must never be served from the previous host's
// history log.
func TestSetExecutorInvalidatesHistoryCache(t *testing.T) {
	// Host A's history has an execution for "/usr/bin/job".
	a := &recordingExecutor{
		output: fmt.Sprintf("1780000000000	0	%s\n", base64.StdEncoding.EncodeToString([]byte("/usr/bin/job"))),
	}
	// Host B's history is empty (never ran anything).
	b := &recordingExecutor{output: ""}

	m := NewManagerWithExecutor(a)

	if exec := m.GetLastExecution("/usr/bin/job"); exec == nil {
		t.Fatal("expected execution from host A before the switch")
	}

	m.SetExecutor(b)

	// After the switch the cache must be invalidated and re-read through B;
	// it must NOT return A's execution.
	if exec := m.GetLastExecution("/usr/bin/job"); exec != nil {
		t.Fatalf("after switching executors, got stale execution from previous host: %+v", exec)
	}

	var sawReadThroughB bool
	for _, c := range b.commands {
		if strings.Contains(c, "cat") && strings.Contains(c, "history.log") {
			sawReadThroughB = true
		}
	}
	if !sawReadThroughB {
		t.Errorf("expected history re-read through the new executor, got commands: %v", b.commands)
	}
}

// TestWriteCrontabPreservesEnvLinesAndComments guards against a data-loss bug:
// rewriting a crontab that sets environment variables (e.g. PATH=...) or has
// header comments used to silently drop them, breaking every job that relied
// on the env assignment.
func TestWriteCrontabPreservesEnvLinesAndComments(t *testing.T) {
	seed := `# DO NOT EDIT THIS FILE
# TBT lead-gen pipeline
PATH=/usr/local/bin:/usr/bin:/bin
0 6 * * * /root/tbt/run_ingest.sh >> /var/log/tbt/ingest.log 2>&1
`
	exec := &memExecutor{crontab: seed}
	m := NewManagerWithExecutor(exec)

	entries, err := m.GetEntries()
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	// Add a job, forcing a full crontab rewrite.
	if err := m.AddEntry(Entry{Schedule: "0 9 * * *", Command: "/usr/bin/report.sh"}); err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}

	for _, want := range []string{"# DO NOT EDIT THIS FILE", "# TBT lead-gen pipeline", "PATH=/usr/local/bin:/usr/bin:/bin"} {
		if !strings.Contains(exec.crontab, want) {
			t.Errorf("after rewrite, crontab lost %q; got:\n%s", want, exec.crontab)
		}
	}
	// Both jobs must still be present, and the rewritten active jobs wrapped
	// for history tracking.
	if !strings.Contains(exec.crontab, "/root/tbt/run_ingest.sh") {
		t.Errorf("existing job lost after rewrite; got:\n%s", exec.crontab)
	}
	if !strings.Contains(exec.crontab, "/usr/bin/report.sh") {
		t.Errorf("new job missing after rewrite; got:\n%s", exec.crontab)
	}
	if !strings.Contains(exec.crontab, "history.log") {
		t.Errorf("active jobs should be wrapped for history after rewrite; got:\n%s", exec.crontab)
	}
}

// TestAutoTrackRoundTrip verifies the full auto-tracking cycle the GET
// handler performs: read entries -> detect untracked active job -> rewrite
// (wrapping it, preserving env lines) -> read back shows the original command
// and the marker round-trips cleanly.
func TestAutoTrackRoundTrip(t *testing.T) {
	exec := &memExecutor{crontab: "PATH=/opt/bin:/bin\n0 6 * * * /usr/bin/trackme.sh\n"}
	m := NewManagerWithExecutor(exec)

	entries, err := m.GetEntries()
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if !HasUntrackedActive(entries) {
		t.Fatal("expected the plain job to be detected as untracked")
	}

	if err := m.WriteCrontab(entries); err != nil {
		t.Fatalf("WriteCrontab failed: %v", err)
	}

	again, err := m.GetEntries()
	if err != nil {
		t.Fatalf("second GetEntries failed: %v", err)
	}
	if HasUntrackedActive(again) {
		t.Error("job should be tracked after the rewrite")
	}
	if len(again) != 1 || again[0].Command != "/usr/bin/trackme.sh" {
		t.Errorf("unwrapped command mismatch after round-trip: %+v", again)
	}
	if !strings.Contains(exec.crontab, "PATH=/opt/bin:/bin") {
		t.Errorf("env line lost in round-trip; got:\n%s", exec.crontab)
	}
}
