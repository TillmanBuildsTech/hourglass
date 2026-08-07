package cron

import (
	"encoding/base64"
	"fmt"
	"log"
	"strconv"
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

// Invalidate marks the cache as stale so the next Get forces a re-read
// through the current executor. Called when the active connection changes
// (local <-> SSH-remote), so LastRun/LastStatus from the previous host can
// never be served for the new host's jobs during the up-to-30s window before
// the background ticker refreshes.
func (h *HistoryCache) Invalidate() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastUpdate = time.Time{}
}

// Refresh re-reads the Hourglass-owned execution log through executor (so
// it works against both local and SSH-remote crontabs) and rebuilds the
// cache with the latest execution per command.
func (h *HistoryCache) Refresh(executor Executor) error {
	output, err := executor.Execute(readHistoryLogCommand())
	if err != nil {
		return fmt.Errorf("failed to read history log: %w", err)
	}

	entries := make(map[string]*Execution)

	for _, line := range strings.Split(output, "\n") {
		exec := parseHistoryLine(line)
		if exec == nil {
			continue
		}
		key := normalizeCommand(exec.Command)
		if existing, ok := entries[key]; !ok || exec.Timestamp.After(existing.Timestamp) {
			entries[key] = exec
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = entries
	h.lastUpdate = time.Now()

	return nil
}

// parseHistoryLine parses one line written by the crontab command wrapper
// (see wrapCommandForHistory): "<unix-millis>\t<exit-code>\t<base64(cmd)>".
func parseHistoryLine(line string) *Execution {
	line = strings.TrimRight(line, "\r")
	if line == "" {
		return nil
	}

	fields := strings.Split(line, "\t")
	if len(fields) != 3 {
		return nil
	}

	ms, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return nil
	}

	exitCode, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil
	}

	cmdBytes, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil || len(cmdBytes) == 0 {
		return nil
	}

	status := "success"
	if exitCode != 0 {
		status = "failed"
	}

	return &Execution{
		Timestamp: time.UnixMilli(ms),
		Command:   string(cmdBytes),
		ExitCode:  exitCode,
		Status:    status,
	}
}

func normalizeCommand(cmd string) string {
	return strings.TrimSpace(cmd)
}

func (m *Manager) GetLastExecution(cmd string) *Execution {
	exec := m.cache.Get(cmd)
	if exec == nil {
		if err := m.cache.Refresh(m.executor); err != nil {
			return nil
		}
		exec = m.cache.Get(cmd)
	}
	return exec
}

func (m *Manager) StartHistoryRefresh() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		var lastErr string

		// The ticker's channel is unbuffered beyond one pending tick, so a
		// slow Refresh call (bounded by ssh.Client's own timeout) naturally
		// can't overlap with the next one - the loop just picks up the next
		// tick once it returns.
		for range ticker.C {
			err := m.cache.Refresh(m.executor)

			switch {
			case err != nil && err.Error() != lastErr:
				log.Printf("cron: history refresh failed: %v", err)
				lastErr = err.Error()
			case err == nil && lastErr != "":
				log.Printf("cron: history refresh recovered")
				lastErr = ""
			}
		}
	}()
}
