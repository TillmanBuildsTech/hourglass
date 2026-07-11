package connection

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	KeyPath  string `json:"key_path"`
	IsLocal  bool   `json:"is_local"`
}

type Manager struct {
	configDir string
	configs   map[string]*Config
	activeID  string
}

func NewManager(configDir string) (*Manager, error) {
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configDir = filepath.Join(home, ".hourglass")
	}

	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, err
	}

	m := &Manager{
		configDir: configDir,
		configs:   make(map[string]*Config),
	}

	if err := m.load(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Manager) load() error {
	configFile := filepath.Join(m.configDir, "connections.json")

	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet, that's ok
			return nil
		}
		return err
	}

	type stored struct {
		Configs map[string]*Config `json:"configs"`
		ActiveID string             `json:"active_id"`
	}

	var s stored
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	m.configs = s.Configs
	m.activeID = s.ActiveID
	return nil
}

func (m *Manager) Save() error {
	type stored struct {
		Configs map[string]*Config `json:"configs"`
		ActiveID string             `json:"active_id"`
	}

	s := stored{
		Configs: m.configs,
		ActiveID: m.activeID,
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	configFile := filepath.Join(m.configDir, "connections.json")
	return os.WriteFile(configFile, data, 0600)
}

func (m *Manager) AddConnection(cfg *Config) error {
	if cfg.ID == "" {
		return fmt.Errorf("connection ID is required")
	}
	if cfg.Host == "" {
		return fmt.Errorf("host is required")
	}
	if cfg.User == "" {
		return fmt.Errorf("user is required")
	}
	if !cfg.IsLocal && cfg.KeyPath == "" {
		return fmt.Errorf("key path is required for remote connections")
	}

	m.configs[cfg.ID] = cfg
	return m.Save()
}

func (m *Manager) RemoveConnection(id string) error {
	delete(m.configs, id)
	if m.activeID == id {
		m.activeID = ""
	}
	return m.Save()
}

func (m *Manager) GetConnection(id string) (*Config, error) {
	cfg, ok := m.configs[id]
	if !ok {
		return nil, fmt.Errorf("connection not found: %s", id)
	}
	return cfg, nil
}

func (m *Manager) ListConnections() []*Config {
	configs := make([]*Config, 0, len(m.configs))
	for _, cfg := range m.configs {
		configs = append(configs, cfg)
	}
	return configs
}

func (m *Manager) SetActive(id string) error {
	if id != "" {
		if _, ok := m.configs[id]; !ok {
			return fmt.Errorf("connection not found: %s", id)
		}
	}
	m.activeID = id
	return m.Save()
}

func (m *Manager) GetActive() *Config {
	if m.activeID == "" {
		return nil
	}
	cfg, _ := m.configs[m.activeID]
	return cfg
}

func (m *Manager) GetActiveID() string {
	return m.activeID
}
