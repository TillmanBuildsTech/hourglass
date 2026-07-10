package cron

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Manager struct {
	cache *HistoryCache
}

func NewManager() *Manager {
	return &Manager{
		cache: NewHistoryCache(30 * time.Second),
	}
}

func (m *Manager) ReadCrontab() (string, error) {
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

func (m *Manager) WriteCrontab(entries []Entry) error {
	text := StringifyCrontab(entries)

	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(text)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to write crontab: %w", err)
	}

	return nil
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
