package cron

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Execution struct {
	Timestamp time.Time
	Command   string
	ExitCode  int
	Status    string
}

type JournalEntry struct {
	Message            string `json:"MESSAGE"`
	Priority           string `json:"PRIORITY"`
	ExitCode           string `json:"EXIT_CODE"`
	CmdLine            string `json:"CMDLINE"`
	RealtimeTimestamp  string `json:"__REALTIME_TIMESTAMP"`
}

type HistoryCache struct {
	entries    map[string]*Execution
	mu         sync.RWMutex
	lastUpdate time.Time
	ttl        time.Duration
	executor   Executor
}

func NewHistoryCache(ttl time.Duration) *HistoryCache {
	return &HistoryCache{
		entries:  make(map[string]*Execution),
		ttl:      ttl,
		executor: &LocalExecutor{},
	}
}

func (h *HistoryCache) Get(command string) *Execution {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if time.Since(h.lastUpdate) > h.ttl {
		return nil
	}

	return h.entries[normalizeCommand(command)]
}

func (h *HistoryCache) SetExecutor(executor Executor) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.executor = executor
}

func (h *HistoryCache) Refresh() error {
	output, err := h.executor.Execute("journalctl -u cron -o json --since=24h\\ ago")
	if err != nil || output == "" {
		// Fallback to syslog if journalctl doesn't work
		output, _ = h.executor.Execute("grep 'CRON' /var/log/syslog | tail -100")
		if output == "" {
			return nil
		}
		return h.parseSyslog(output)
	}

	entries := make(map[string]*Execution)

	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		var je JournalEntry
		if err := json.Unmarshal([]byte(line), &je); err != nil {
			continue
		}

		exec := parseJournalEntry(je)
		if exec != nil {
			cmd := normalizeCommand(exec.Command)
			if _, exists := entries[cmd]; !exists || exec.Timestamp.After(entries[cmd].Timestamp) {
				entries[cmd] = exec
			}
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = entries
	h.lastUpdate = time.Now()

	return nil
}

func parseJournalEntry(je JournalEntry) *Execution {
	if !strings.Contains(je.Message, "CMD") && !strings.Contains(je.Message, "CRON") {
		return nil
	}

	cmd := extractCommand(je.Message)
	if cmd == "" {
		return nil
	}

	exitCode := 0
	if je.ExitCode != "" && je.ExitCode != "0" {
		fmt.Sscanf(je.ExitCode, "%d", &exitCode)
	}

	status := "success"
	if exitCode != 0 {
		status = "failed"
	}

	ts := time.Now()
	if je.RealtimeTimestamp != "" {
		var micros int64
		if _, err := fmt.Sscanf(je.RealtimeTimestamp, "%d", &micros); err == nil {
			ts = time.UnixMicro(micros)
		}
	}

	return &Execution{
		Timestamp: ts,
		Command:   cmd,
		ExitCode:  exitCode,
		Status:    status,
	}
}

func extractCommand(msg string) string {
	if idx := strings.Index(msg, "CMD ("); idx != -1 {
		start := idx + 5
		if endIdx := strings.Index(msg[start:], ")"); endIdx != -1 {
			cmd := msg[start : start+endIdx]
			// Strip trailing shell comments (# outside quotes)
			if hashIdx := strings.LastIndex(cmd, "#"); hashIdx != -1 {
				// Check if # is outside quotes by counting quotes before it
				beforeHash := cmd[:hashIdx]
				if (strings.Count(beforeHash, `"`) % 2) == 0 {
					cmd = strings.TrimSpace(beforeHash)
				}
			}
			return cmd
		}
	}
	return ""
}

func normalizeCommand(cmd string) string {
	return strings.TrimSpace(strings.ToLower(cmd))
}

func (h *HistoryCache) parseSyslog(content string) error {
	entries := make(map[string]*Execution)

	lines := strings.Split(strings.TrimSpace(content), "\n")
	for _, line := range lines {
		if line == "" || !strings.Contains(line, "CRON") {
			continue
		}

		exec := parseSyslogLine(line)
		if exec != nil {
			cmd := normalizeCommand(exec.Command)
			if _, exists := entries[cmd]; !exists || exec.Timestamp.After(entries[cmd].Timestamp) {
				entries[cmd] = exec
			}
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = entries
	h.lastUpdate = time.Now()

	return nil
}

func parseSyslogLine(line string) *Execution {
	// Parse syslog format: "Jul 10 01:00:01 hostname CRON[1234]: (root) CMD (command)"
	cmd := extractCommand(line)
	if cmd == "" {
		return nil
	}

	// Extract timestamp from syslog (first 15 chars: "Jul 10 01:00:01")
	ts := time.Now()
	if len(line) >= 15 {
		timeStr := line[:15]
		if parsed, err := time.Parse("Jan _2 15:04:05", timeStr); err == nil {
			// Set year to current year
			now := time.Now()
			ts = parsed.AddDate(now.Year()-parsed.Year(), 0, 0)
		}
	}

	return &Execution{
		Timestamp: ts,
		Command:   cmd,
		ExitCode:  0,
		Status:    "success",
	}
}

func (m *Manager) GetLastExecution(cmd string) *Execution {
	exec := m.cache.Get(cmd)
	if exec == nil {
		if err := m.cache.Refresh(); err != nil {
			return nil
		}
		exec = m.cache.Get(cmd)
	}
	return exec
}

func (m *Manager) StartHistoryRefresh() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			m.cache.Refresh()
		}
	}()
}
