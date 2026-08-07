package cron

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// historyLogPathExpr is a shell expression (evaluated on whichever host
// actually runs the job - local or SSH-remote) that resolves to the file
// Hourglass appends execution records to. It is resolved at execution
// time (via $HOME, which cron always sets from the crontab owner's
// passwd entry) rather than by Hourglass itself, so remote crontabs log
// to the remote host's home directory, not the Hourglass process's.
const historyLogPathExpr = `"$HOME/.hourglass/history.log"`

const hgMarkerPrefix = "[[hg:"
const hgMarkerSuffix = "]]"

// timestampExpr resolves to a millisecond unix timestamp on both GNU date
// (Linux) and BSD date (macOS). GNU date supports "%3N" (3-digit
// milliseconds); BSD date does not recognize the width-truncated form and
// echoes it back with non-digit characters (e.g. "3N"), so the digit check
// below is used to detect this rather than probing exit status - some BSD
// date builds accept bare "%N" (nanoseconds) without erroring, which would
// otherwise make an exit-status probe misidentify them as GNU date. Each
// case pattern has a leading "(" - without it, some /bin/sh implementations
// fail to parse a "case" inside a "$(...)" command substitution because the
// pattern's closing ")" confuses paren-matching.
const timestampExpr = `$(ms=$(date +%3N 2>/dev/null); case "$ms" in (""|*[!0-9]*) printf '%s000' "$(date +%s)" ;; (*) printf '%s%s' "$(date +%s)" "$ms" ;; esac)`

// wrapCommandForHistory wraps command so that, whenever cron runs it, the
// wrapper appends a "<unix-millis>\t<exit-code>\t<base64(command)>" record
// to the Hourglass history log and then exits with the original command's
// exit code. Cron itself never reports exit codes to syslog, so this is
// the only reliable way to populate LastRun/LastStatus/LastCode.
func wrapCommandForHistory(command string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(command))
	return fmt.Sprintf(
		`{ %s ; }; __hg_ec=$?; printf '%%s\t%%s\t%%s\n' %s "$__hg_ec" %s >> %s 2>/dev/null; exit $__hg_ec`,
		command, timestampExpr, shellQuote(encoded), historyLogPathExpr,
	)
}

func ensureHistoryLogDirCommand() string {
	return fmt.Sprintf(`mkdir -p "$(dirname %s)" 2>/dev/null || true`, historyLogPathExpr)
}

func readHistoryLogCommand() string {
	return fmt.Sprintf(`cat %s 2>/dev/null || true`, historyLogPathExpr)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func hgMarker(command string) string {
	return hgMarkerPrefix + base64.StdEncoding.EncodeToString([]byte(command)) + hgMarkerSuffix
}

// extractHgMarker returns the base64 payload of the Hourglass history marker
// embedded in a comment ([[hg:<base64>]]), if present.
func extractHgMarker(comment string) (string, bool) {
	start := strings.Index(comment, hgMarkerPrefix)
	if start == -1 {
		return "", false
	}
	rest := comment[start+len(hgMarkerPrefix):]
	end := strings.Index(rest, hgMarkerSuffix)
	if end == -1 {
		return "", false
	}
	return rest[:end], true
}

// unwrapEntry restores the original Command (and strips the history
// marker from Comment) for an entry previously wrapped by WriteCrontab.
// Entries without the marker - e.g. ones added outside Hourglass - are
// returned unchanged.
func unwrapEntry(e Entry) Entry {
	encoded, ok := extractHgMarker(e.Comment)
	if !ok {
		return e
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return e
	}

	start := strings.Index(e.Comment, hgMarkerPrefix)
	e.Command = string(decoded)
	e.Comment = strings.TrimSpace(e.Comment[:start] + e.Comment[start+len(hgMarkerPrefix)+len(encoded)+len(hgMarkerSuffix):])
	return e
}

type Executor interface {
	Execute(command string) (string, error)
}

type LocalExecutor struct{}

func (e *LocalExecutor) Execute(command string) (string, error) {
	if command == "crontab -l" {
		cmd := exec.Command("crontab", "-l")
		output, err := cmd.Output()
		if err != nil {
			if strings.Contains(err.Error(), "exit status 1") {
				return "", fmt.Errorf("no crontab found or permission denied")
			}
			return "", fmt.Errorf("failed to read crontab: %w", err)
		}
		return string(output), nil
	}

	if strings.HasPrefix(command, "crontab -") {
		cmd := exec.Command("sh", "-c", command)
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to write crontab: %w", err)
		}
		return "", nil
	}

	cmd := exec.Command("sh", "-c", command)
	output, err := cmd.Output()
	return string(output), err
}

// FileExecutor is an Executor that reads and writes crontab text from a single
// file instead of the system crontab. It is used for isolated testing (e.g.
// Playwright E2E against a scratch instance) via the HOURGLASS_CRONTAB_FILE
// env var: real cron jobs are never touched, while shell commands (history
// logging, job execution) still run normally. Production does not set that
// env var, so it keeps using LocalExecutor.
type FileExecutor struct {
	Path string
	mu   sync.Mutex
}

func NewFileExecutor(path string) *FileExecutor {
	return &FileExecutor{Path: path}
}

func (e *FileExecutor) Execute(command string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	data, err := os.ReadFile(e.Path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to read crontab file: %w", err)
	}
	if os.IsNotExist(err) {
		data = nil
	}
	current := string(data)

	switch {
	case command == "crontab -l":
		if strings.TrimSpace(current) == "" {
			return "", fmt.Errorf("no crontab found or permission denied")
		}
		return current, nil
	case command == "crontab -r":
		if err := os.Remove(e.Path); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to delete crontab: %w", err)
		}
		return "", nil
	case strings.HasPrefix(command, "crontab -"):
		// Heredoc form produced by WriteCrontab:
		//   crontab - << 'EOF'\n<text>\nEOF
		start := strings.Index(command, "\n")
		end := strings.LastIndex(command, "\nEOF")
		if start == -1 || end == -1 || end < start {
			return "", fmt.Errorf("failed to write crontab: malformed heredoc")
		}
		text := command[start+1 : end]
		if err := os.WriteFile(e.Path, []byte(text), 0600); err != nil {
			return "", fmt.Errorf("failed to write crontab: %w", err)
		}
		return "", nil
	default:
		cmd := exec.Command("sh", "-c", command)
		output, err := cmd.Output()
		return string(output), err
	}
}

type Manager struct {
	executor Executor
	cache    *HistoryCache
	// preserved holds non-entry crontab lines (env assignments like PATH=...,
	// standalone comments, blank lines) captured on the last read, so a write
	// round-trip re-emits them instead of silently dropping them. Populated by
	// GetEntries, consumed by WriteCrontab.
	preserved []string
}

func NewManager() *Manager {
	return &Manager{
		executor: &LocalExecutor{},
		cache:    NewHistoryCache(30 * time.Second),
	}
}

func NewManagerWithExecutor(executor Executor) *Manager {
	return &Manager{
		executor: executor,
		cache:    NewHistoryCache(30 * time.Second),
	}
}

// SetExecutor swaps the host that reads/writes the crontab and history log.
// The history cache is invalidated so the next lookup re-reads through the
// new executor; without this, switching between local and a remote connection
// could keep serving the previous host's LastRun/LastStatus for up to 30s
// (until the background ticker refreshes).
func (m *Manager) SetExecutor(executor Executor) {
	m.executor = executor
	m.cache.Invalidate()
}

func (m *Manager) ReadCrontab() (string, error) {
	return m.executor.Execute("crontab -l")
}

func (m *Manager) WriteCrontab(entries []Entry) error {
	m.executor.Execute(ensureHistoryLogDirCommand())

	wrapped := make([]Entry, len(entries))
	for i, e := range entries {
		wrapped[i] = e
		if !e.Inactive {
			wrapped[i].Command = wrapCommandForHistory(e.Command)
			if e.Comment != "" {
				wrapped[i].Comment = e.Comment + " " + hgMarker(e.Command)
			} else {
				wrapped[i].Comment = hgMarker(e.Command)
			}
		}
	}

	text := StringifyCrontab(wrapped)
	// Re-emit non-entry lines (PATH=..., header comments) captured on the last
	// read so rewriting never drops them.
	if len(m.preserved) > 0 && text != "" {
		text = strings.Join(m.preserved, "\n") + "\n" + text
	} else if len(m.preserved) > 0 {
		text = strings.Join(m.preserved, "\n")
	}
	// Drop trailing blank lines left by the file's final newline so repeated
	// read/write round-trips don't accumulate empty lines.
	text = strings.TrimRight(text, "\n")
	cmd := fmt.Sprintf("crontab - << 'EOF'\n%s\nEOF", text)
	_, err := m.executor.Execute(cmd)
	return err
}

func (m *Manager) GetEntries() ([]Entry, error) {
	text, err := m.ReadCrontab()
	if err != nil {
		return nil, err
	}

	entries, err := ParseCrontab(text)
	if err != nil {
		return nil, fmt.Errorf("failed to parse crontab: %w", err)
	}

	m.preserved = PreserveNonEntryLines(text)

	for i := range entries {
		// Set Tracked from the RAW comment (with the marker still intact);
		// unwrapEntry below strips the marker, which would otherwise make
		// every job look untracked.
		entries[i].Tracked = HasHgMarker(entries[i].Comment)
		entries[i] = unwrapEntry(entries[i])
	}

	return entries, nil
}

func (m *Manager) AddEntry(entry Entry) error {
	if err := ValidateSchedule(entry.Schedule); err != nil {
		return fmt.Errorf("invalid schedule: %w", err)
	}

	entries, err := m.GetEntries()
	if err != nil {
		if strings.Contains(err.Error(), "no crontab found") {
			entries = []Entry{}
		} else {
			return err
		}
	}

	entries = append(entries, entry)

	return m.WriteCrontab(entries)
}

func (m *Manager) DeleteEntry(index int) error {
	entries, err := m.GetEntries()
	if err != nil {
		return err
	}

	if index < 0 || index >= len(entries) {
		return fmt.Errorf("entry index out of range")
	}

	entries = append(entries[:index], entries[index+1:]...)

	if len(entries) == 0 {
		return m.deleteCrontab()
	}

	return m.WriteCrontab(entries)
}

func (m *Manager) UpdateEntry(index int, entry Entry) error {
	if err := ValidateSchedule(entry.Schedule); err != nil {
		return fmt.Errorf("invalid schedule: %w", err)
	}

	entries, err := m.GetEntries()
	if err != nil {
		return err
	}

	if index < 0 || index >= len(entries) {
		return fmt.Errorf("entry index out of range")
	}

	entries[index] = entry

	return m.WriteCrontab(entries)
}

func (m *Manager) deleteCrontab() error {
	if _, err := m.executor.Execute("crontab -r"); err != nil {
		return fmt.Errorf("failed to delete crontab: %w", err)
	}
	return nil
}

func (m *Manager) ExecuteCommand(command string) (string, error) {
	return m.executor.Execute(command)
}

// ExecuteForHistory runs command via the current executor wrapped so its
// result is recorded in the Hourglass execution log, then refreshes the
// history cache immediately. It is used by the "Run now" action so a manual
// execution shows up in LastRun/LastStatus without waiting for the 30-second
// ticker. Because command is wrapped (not run raw), the record is written on
// both success and failure (with the real exit code).
func (m *Manager) ExecuteForHistory(command string) (string, error) {
	m.executor.Execute(ensureHistoryLogDirCommand())
	output, err := m.executor.Execute(wrapCommandForHistory(command))
	m.cache.Refresh(m.executor)
	return output, err
}

// ReadHistoryLog returns the raw contents of the Hourglass execution log,
// read through the current executor so it reflects whichever host actually
// runs the jobs (local or SSH-remote), rather than the Hourglass process's
// own machine.
func (m *Manager) ReadHistoryLog() (string, error) {
	output, err := m.executor.Execute(readHistoryLogCommand())
	if err != nil {
		return "", fmt.Errorf("failed to read history log: %w", err)
	}
	return output, nil
}

// HistoryLogPath resolves the absolute path of the Hourglass execution log
// through the current executor, so the UI paths shown for remote connections
// point at the remote host rather than the Hourglass process's machine.
func (m *Manager) HistoryLogPath() string {
	home, err := m.executor.Execute(`printf '%s' "$HOME"`)
	if err != nil || strings.TrimSpace(home) == "" {
		return "$HOME/.hourglass/history.log"
	}
	return filepath.Join(strings.TrimSpace(home), ".hourglass", "history.log")
}

// RefreshHistory forces the history cache to re-read execution logs
// through the current executor. Call this after executing a job to make
// the new execution visible in the UI without waiting for the 30-second
// background ticker.
func (m *Manager) RefreshHistory() {
	_ = m.cache.Refresh(m.executor)
}
