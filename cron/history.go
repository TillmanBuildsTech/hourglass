package cron

import (
	"encoding/json"
	"fmt"
	"os/exec"
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
	Message  string `json:"MESSAGE"`
	Priority string `json:"PRIORITY"`
	ExitCode string `json:"EXIT_CODE"`
	CmdLine  string `json:"CMDLINE"`
}

type HistoryCache struct {
	entries    map[string]*Execution
	mu         sync.RWMutex
	lastUpdate time.Time
	ttl        time.Duration
}

func NewHistoryCache(ttl time.Duration) *HistoryCache {
	return &HistoryCache{
		entries: make(map[string]*Execution),
		ttl:     ttl,
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

func (h *HistoryCache) Refresh() error {
	cmd := exec.Command("journalctl", "-u", "cron", "-o", "json", "--since=24h ago")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to query journalctl: %w", err)
	}

	entries := make(map[string]*Execution)

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
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

	return &Execution{
		Timestamp: time.Now(),
		Command:   cmd,
		ExitCode:  exitCode,
		Status:    status,
	}
}

func extractCommand(msg string) string {
	if idx := strings.Index(msg, "CMD ("); idx != -1 {
		start := idx + 5
		if endIdx := strings.Index(msg[start:], ")"); endIdx != -1 {
			return msg[start : start+endIdx]
		}
	}
	return ""
}

func normalizeCommand(cmd string) string {
	return strings.TrimSpace(strings.ToLower(cmd))
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
