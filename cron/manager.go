package cron

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
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

// unwrapEntry restores the original Command (and strips the history
// marker from Comment) for an entry previously wrapped by WriteCrontab.
// Entries without the marker - e.g. ones added outside Hourglass - are
// returned unchanged.
func unwrapEntry(e Entry) Entry {
	start := strings.Index(e.Comment, hgMarkerPrefix)
	if start == -1 {
		return e
	}
	rest := e.Comment[start+len(hgMarkerPrefix):]
	end := strings.Index(rest, hgMarkerSuffix)
	if end == -1 {
		return e
	}

	encoded := rest[:end]
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return e
	}

	e.Command = string(decoded)
	e.Comment = strings.TrimSpace(e.Comment[:start] + rest[end+len(hgMarkerSuffix):])
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

type Manager struct {
	executor Executor
	cache    *HistoryCache
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

func (m *Manager) SetExecutor(executor Executor) {
	m.executor = executor
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

	for i := range entries {
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
