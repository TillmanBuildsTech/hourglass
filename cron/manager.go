package cron

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

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
	text := StringifyCrontab(entries)
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
	cmd := exec.Command("crontab", "-r")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete crontab: %w", err)
	}
	return nil
}
