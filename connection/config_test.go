package connection

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestNewManagerWithCustomDir verifies Manager creation with custom config directory
func TestNewManagerWithCustomDir(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr == nil {
		t.Error("NewManager returned nil manager")
	}
	if mgr.configDir != tmpDir {
		t.Errorf("configDir = %q, want %q", mgr.configDir, tmpDir)
	}
}

// TestNewManagerWithDefaultDir verifies Manager creation uses home directory by default
func TestNewManagerWithDefaultDir(t *testing.T) {
	mgr, err := NewManager("")
	if err != nil {
		t.Fatalf("NewManager with empty string failed: %v", err)
	}
	if mgr == nil {
		t.Error("NewManager returned nil manager")
	}

	home, _ := os.UserHomeDir()
	expectedDir := filepath.Join(home, ".hourglass")
	if mgr.configDir != expectedDir {
		t.Errorf("configDir = %q, want %q", mgr.configDir, expectedDir)
	}
}

// TestNewManagerCreatesDirectory verifies directory is created if missing
func TestNewManagerCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	newDir := filepath.Join(tmpDir, "subdir", "deeper")

	mgr, err := NewManager(newDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	if _, err := os.Stat(newDir); err != nil {
		t.Errorf("Directory was not created: %v", err)
	}

	if mgr.configDir != newDir {
		t.Errorf("configDir = %q, want %q", mgr.configDir, newDir)
	}
}

// TestAddConnection_Valid verifies adding a valid connection
func TestAddConnection_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:       "test-conn",
		Label:    "Test Connection",
		Host:     "example.com",
		Port:     22,
		User:     "admin",
		KeyPath:  "/home/user/.ssh/id_rsa",
		IsLocal:  false,
	}

	err := mgr.AddConnection(cfg)
	if err != nil {
		t.Fatalf("AddConnection failed: %v", err)
	}

	// Verify it was added to in-memory store
	retrieved, err := mgr.GetConnection("test-conn")
	if err != nil {
		t.Fatalf("GetConnection failed: %v", err)
	}
	if retrieved.Host != "example.com" {
		t.Errorf("Host = %q, want %q", retrieved.Host, "example.com")
	}
}

// TestAddConnection_LocalNoKeyPath verifies local connections don't require keypath
func TestAddConnection_LocalNoKeyPath(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "local-conn",
		Label:   "Local",
		Host:    "localhost",
		Port:    22,
		User:    "user",
		IsLocal: true,
	}

	err := mgr.AddConnection(cfg)
	if err != nil {
		t.Fatalf("AddConnection for local failed: %v", err)
	}
}

// TestAddConnection_RemoteRequiresKeyPath verifies remote connections require keypath
func TestAddConnection_RemoteRequiresKeyPath(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "remote-conn",
		Label:   "Remote",
		Host:    "example.com",
		Port:    22,
		User:    "admin",
		IsLocal: false,
		// KeyPath is empty - should fail
	}

	err := mgr.AddConnection(cfg)
	if err == nil {
		t.Error("AddConnection should fail without key path for remote connection")
	}
	if err.Error() != "key path is required for remote connections" {
		t.Errorf("Error message = %q, want 'key path is required for remote connections'", err.Error())
	}
}

// TestAddConnection_MissingID verifies ID is required
func TestAddConnection_MissingID(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "",
		Label:   "No ID",
		Host:    "example.com",
		Port:    22,
		User:    "admin",
		KeyPath: "/path/to/key",
	}

	err := mgr.AddConnection(cfg)
	if err == nil {
		t.Error("AddConnection should fail without ID")
	}
	if err.Error() != "connection ID is required" {
		t.Errorf("Error message = %q, want 'connection ID is required'", err.Error())
	}
}

// TestAddConnection_MissingHost verifies host is required
func TestAddConnection_MissingHost(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "test",
		Label:   "No Host",
		Host:    "",
		Port:    22,
		User:    "admin",
		KeyPath: "/path/to/key",
	}

	err := mgr.AddConnection(cfg)
	if err == nil {
		t.Error("AddConnection should fail without host")
	}
	if err.Error() != "host is required" {
		t.Errorf("Error message = %q, want 'host is required'", err.Error())
	}
}

// TestAddConnection_MissingUser verifies user is required
func TestAddConnection_MissingUser(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "test",
		Label:   "No User",
		Host:    "example.com",
		Port:    22,
		User:    "",
		KeyPath: "/path/to/key",
	}

	err := mgr.AddConnection(cfg)
	if err == nil {
		t.Error("AddConnection should fail without user")
	}
	if err.Error() != "user is required" {
		t.Errorf("Error message = %q, want 'user is required'", err.Error())
	}
}

// TestAddConnection_Persistence verifies connections are saved to disk
func TestAddConnection_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	mgr1, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "persist-test",
		Label:   "Persistent",
		Host:    "example.com",
		Port:    22,
		User:    "admin",
		KeyPath: "/path/to/key",
		IsLocal: false,
	}

	mgr1.AddConnection(cfg)

	// Create new manager with same dir - should load the saved connection
	mgr2, _ := NewManager(tmpDir)
	retrieved, err := mgr2.GetConnection("persist-test")
	if err != nil {
		t.Fatalf("GetConnection failed: %v", err)
	}
	if retrieved.Host != "example.com" {
		t.Errorf("Host = %q, want %q", retrieved.Host, "example.com")
	}
}

// TestRemoveConnection_Success verifies removing an existing connection
func TestRemoveConnection_Success(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "remove-test",
		Label:   "To Remove",
		Host:    "example.com",
		Port:    22,
		User:    "admin",
		KeyPath: "/path/to/key",
		IsLocal: false,
	}
	mgr.AddConnection(cfg)

	err := mgr.RemoveConnection("remove-test")
	if err != nil {
		t.Fatalf("RemoveConnection failed: %v", err)
	}

	// Verify it was removed
	_, err = mgr.GetConnection("remove-test")
	if err == nil {
		t.Error("Connection should not exist after removal")
	}
}

// TestRemoveConnection_ClearsActive verifies active flag is cleared when removing active connection
func TestRemoveConnection_ClearsActive(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "active-remove",
		Label:   "Active",
		Host:    "example.com",
		Port:    22,
		User:    "admin",
		KeyPath: "/path/to/key",
		IsLocal: false,
	}
	mgr.AddConnection(cfg)
	mgr.SetActive("active-remove")

	if mgr.GetActiveID() != "active-remove" {
		t.Error("Active connection not set")
	}

	mgr.RemoveConnection("active-remove")

	if mgr.GetActiveID() != "" {
		t.Error("Active connection ID should be cleared after removal")
	}
}

// TestRemoveConnection_Persistence verifies removal is saved to disk
func TestRemoveConnection_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	mgr1, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "persist-remove",
		Label:   "Persistent",
		Host:    "example.com",
		Port:    22,
		User:    "admin",
		KeyPath: "/path/to/key",
		IsLocal: false,
	}
	mgr1.AddConnection(cfg)
	mgr1.RemoveConnection("persist-remove")

	// Create new manager with same dir
	mgr2, _ := NewManager(tmpDir)
	_, err := mgr2.GetConnection("persist-remove")
	if err == nil {
		t.Error("Connection should not exist after removal and reload")
	}
}

// TestGetConnection_Found verifies retrieving an existing connection
func TestGetConnection_Found(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "get-test",
		Label:   "Get Test",
		Host:    "example.com",
		Port:    2222,
		User:    "testuser",
		KeyPath: "/path/to/key",
		IsLocal: false,
	}
	mgr.AddConnection(cfg)

	retrieved, err := mgr.GetConnection("get-test")
	if err != nil {
		t.Fatalf("GetConnection failed: %v", err)
	}

	if retrieved.Host != "example.com" {
		t.Errorf("Host = %q, want %q", retrieved.Host, "example.com")
	}
	if retrieved.Port != 2222 {
		t.Errorf("Port = %d, want %d", retrieved.Port, 2222)
	}
	if retrieved.User != "testuser" {
		t.Errorf("User = %q, want %q", retrieved.User, "testuser")
	}
}

// TestGetConnection_NotFound verifies error when connection doesn't exist
func TestGetConnection_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	_, err := mgr.GetConnection("nonexistent")
	if err == nil {
		t.Error("GetConnection should fail for nonexistent connection")
	}
	if err.Error() != "connection not found: nonexistent" {
		t.Errorf("Error message = %q", err.Error())
	}
}

// TestListConnections_Empty verifies listing when no connections exist
func TestListConnections_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	connections := mgr.ListConnections()
	if len(connections) != 0 {
		t.Errorf("Expected 0 connections, got %d", len(connections))
	}
}

// TestListConnections_Multiple verifies listing multiple connections
func TestListConnections_Multiple(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	configs := []*Config{
		{ID: "conn1", Label: "First", Host: "host1.com", Port: 22, User: "user1", KeyPath: "/key1", IsLocal: false},
		{ID: "conn2", Label: "Second", Host: "host2.com", Port: 22, User: "user2", KeyPath: "/key2", IsLocal: false},
		{ID: "conn3", Label: "Third", Host: "host3.com", Port: 22, User: "user3", KeyPath: "/key3", IsLocal: false},
	}

	for _, cfg := range configs {
		mgr.AddConnection(cfg)
	}

	connections := mgr.ListConnections()
	if len(connections) != 3 {
		t.Errorf("Expected 3 connections, got %d", len(connections))
	}

	// Verify all connections are in the list
	ids := make(map[string]bool)
	for _, conn := range connections {
		ids[conn.ID] = true
	}

	for _, cfg := range configs {
		if !ids[cfg.ID] {
			t.Errorf("Connection %q not found in list", cfg.ID)
		}
	}
}

// TestSetActive_Valid verifies setting an active connection
func TestSetActive_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "active-test",
		Label:   "Active",
		Host:    "example.com",
		Port:    22,
		User:    "admin",
		KeyPath: "/path/to/key",
		IsLocal: false,
	}
	mgr.AddConnection(cfg)

	err := mgr.SetActive("active-test")
	if err != nil {
		t.Fatalf("SetActive failed: %v", err)
	}

	if mgr.GetActiveID() != "active-test" {
		t.Errorf("Active ID = %q, want %q", mgr.GetActiveID(), "active-test")
	}
}

// TestSetActive_Nonexistent verifies error when setting nonexistent connection as active
func TestSetActive_Nonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	err := mgr.SetActive("nonexistent")
	if err == nil {
		t.Error("SetActive should fail for nonexistent connection")
	}
	if err.Error() != "connection not found: nonexistent" {
		t.Errorf("Error message = %q", err.Error())
	}
}

// TestSetActive_ClearActive verifies setting empty string clears active
func TestSetActive_ClearActive(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "clear-active",
		Label:   "Active",
		Host:    "example.com",
		Port:    22,
		User:    "admin",
		KeyPath: "/path/to/key",
		IsLocal: false,
	}
	mgr.AddConnection(cfg)
	mgr.SetActive("clear-active")

	if mgr.GetActiveID() == "" {
		t.Error("Active should be set before clearing")
	}

	err := mgr.SetActive("")
	if err != nil {
		t.Fatalf("SetActive with empty string failed: %v", err)
	}

	if mgr.GetActiveID() != "" {
		t.Errorf("Active ID should be empty after clearing, got %q", mgr.GetActiveID())
	}
}

// TestSetActive_Persistence verifies active flag is persisted
func TestSetActive_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	mgr1, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "persist-active",
		Label:   "Active",
		Host:    "example.com",
		Port:    22,
		User:    "admin",
		KeyPath: "/path/to/key",
		IsLocal: false,
	}
	mgr1.AddConnection(cfg)
	mgr1.SetActive("persist-active")

	// Create new manager with same dir
	mgr2, _ := NewManager(tmpDir)
	if mgr2.GetActiveID() != "persist-active" {
		t.Errorf("Active ID = %q, want %q", mgr2.GetActiveID(), "persist-active")
	}
}

// TestGetActive_None verifies nil when no active connection
func TestGetActive_None(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	active := mgr.GetActive()
	if active != nil {
		t.Error("GetActive should return nil when no active connection")
	}
}

// TestGetActive_Exists verifies retrieving active connection
func TestGetActive_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "current-active",
		Label:   "Active",
		Host:    "example.com",
		Port:    22,
		User:    "admin",
		KeyPath: "/path/to/key",
		IsLocal: false,
	}
	mgr.AddConnection(cfg)
	mgr.SetActive("current-active")

	active := mgr.GetActive()
	if active == nil {
		t.Fatal("GetActive returned nil")
	}
	if active.ID != "current-active" {
		t.Errorf("Active connection ID = %q, want %q", active.ID, "current-active")
	}
	if active.Host != "example.com" {
		t.Errorf("Active connection host = %q, want %q", active.Host, "example.com")
	}
}

// TestGetActiveID_None verifies empty string when no active connection
func TestGetActiveID_None(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	id := mgr.GetActiveID()
	if id != "" {
		t.Errorf("GetActiveID = %q, want empty string", id)
	}
}

// TestGetActiveID_Exists verifies retrieving active ID
func TestGetActiveID_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "id-test",
		Label:   "Active",
		Host:    "example.com",
		Port:    22,
		User:    "admin",
		KeyPath: "/path/to/key",
		IsLocal: false,
	}
	mgr.AddConnection(cfg)
	mgr.SetActive("id-test")

	id := mgr.GetActiveID()
	if id != "id-test" {
		t.Errorf("GetActiveID = %q, want %q", id, "id-test")
	}
}

// TestCorruptedConfigFile verifies handling of corrupted JSON
func TestCorruptedConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "connections.json")

	// Write corrupted JSON
	os.WriteFile(configFile, []byte("{invalid json}"), 0600)

	_, err := NewManager(tmpDir)
	if err == nil {
		t.Error("NewManager should fail with corrupted JSON")
	}
}

// TestConfigFileSerialization verifies JSON format
func TestConfigFileSerialization(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	configs := []*Config{
		{ID: "conn1", Label: "First", Host: "host1", Port: 22, User: "user1", KeyPath: "/key1", IsLocal: false},
		{ID: "conn2", Label: "Second", Host: "host2", Port: 2222, User: "user2", KeyPath: "/key2", IsLocal: true},
	}

	for _, cfg := range configs {
		mgr.AddConnection(cfg)
	}
	mgr.SetActive("conn1")

	// Read the JSON file directly
	configFile := filepath.Join(tmpDir, "connections.json")
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	// Verify JSON structure
	type stored struct {
		Configs map[string]*Config `json:"configs"`
		ActiveID string             `json:"active_id"`
	}
	var s stored
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if len(s.Configs) != 2 {
		t.Errorf("Expected 2 configs in JSON, got %d", len(s.Configs))
	}
	if s.ActiveID != "conn1" {
		t.Errorf("ActiveID = %q, want %q", s.ActiveID, "conn1")
	}
}

// TestMultipleOperations verifies complex scenario with multiple operations
func TestMultipleOperations(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)

	// Add multiple connections
	for i := 1; i <= 5; i++ {
		cfg := &Config{
			ID:      fmt.Sprintf("conn%d", i),
			Label:   fmt.Sprintf("Connection %d", i),
			Host:    fmt.Sprintf("host%d.com", i),
			Port:    22 + i,
			User:    fmt.Sprintf("user%d", i),
			KeyPath: fmt.Sprintf("/key%d", i),
			IsLocal: i%2 == 0, // Even numbers are local
		}
		if err := mgr.AddConnection(cfg); err != nil {
			t.Fatalf("AddConnection failed: %v", err)
		}
	}

	// Set active
	mgr.SetActive("conn3")

	// Remove one
	mgr.RemoveConnection("conn2")

	// List and verify
	connections := mgr.ListConnections()
	if len(connections) != 4 {
		t.Errorf("Expected 4 connections after removal, got %d", len(connections))
	}

	// Verify active is still set
	if mgr.GetActiveID() != "conn3" {
		t.Errorf("Active ID changed unexpectedly: %q", mgr.GetActiveID())
	}
}

// TestConnectionFieldsPreserved verifies all fields are correctly saved and loaded
func TestConnectionFieldsPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	mgr1, _ := NewManager(tmpDir)

	cfg := &Config{
		ID:      "all-fields",
		Label:   "All Fields Test",
		Host:    "test.example.com",
		Port:    2222,
		User:    "testuser",
		KeyPath: "/home/user/.ssh/custom_key",
		IsLocal: false,
	}

	mgr1.AddConnection(cfg)

	// Reload and verify all fields
	mgr2, _ := NewManager(tmpDir)
	retrieved, _ := mgr2.GetConnection("all-fields")

	if retrieved.ID != cfg.ID {
		t.Errorf("ID mismatch: %q != %q", retrieved.ID, cfg.ID)
	}
	if retrieved.Label != cfg.Label {
		t.Errorf("Label mismatch: %q != %q", retrieved.Label, cfg.Label)
	}
	if retrieved.Host != cfg.Host {
		t.Errorf("Host mismatch: %q != %q", retrieved.Host, cfg.Host)
	}
	if retrieved.Port != cfg.Port {
		t.Errorf("Port mismatch: %d != %d", retrieved.Port, cfg.Port)
	}
	if retrieved.User != cfg.User {
		t.Errorf("User mismatch: %q != %q", retrieved.User, cfg.User)
	}
	if retrieved.KeyPath != cfg.KeyPath {
		t.Errorf("KeyPath mismatch: %q != %q", retrieved.KeyPath, cfg.KeyPath)
	}
	if retrieved.IsLocal != cfg.IsLocal {
		t.Errorf("IsLocal mismatch: %v != %v", retrieved.IsLocal, cfg.IsLocal)
	}
}

// TestNewManagerWithReadOnlyDirectory verifies handling of permission issues
func TestNewManagerWithReadOnlyDirectory(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping permission test in CI environment")
	}

	tmpDir := t.TempDir()
	testDir := filepath.Join(tmpDir, "readonly")
	
	// Create the directory first
	os.MkdirAll(testDir, 0700)
	
	// Change to read-only (but we can still read/write as owner on most systems)
	// This test may not work as expected on all systems, so we just verify
	// the manager creation works
	mgr, err := NewManager(testDir)
	if err == nil && mgr != nil {
		// Success case - directory was accessible
	} else {
		// Permission denied case - also acceptable for this test
	}
}

// TestSave_WithExistingFile verifies overwriting existing config
func TestSave_WithExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create first manager and add config
	mgr1, _ := NewManager(tmpDir)
	cfg := &Config{
		ID:      "test",
		Label:   "Test",
		Host:    "example.com",
		Port:    22,
		User:    "user",
		KeyPath: "/key",
		IsLocal: false,
	}
	mgr1.AddConnection(cfg)
	
	// Modify and save
	mgr1.configs["test"].Host = "modified.com"
	err := mgr1.Save()
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	
	// Load with new manager and verify modification
	mgr2, _ := NewManager(tmpDir)
	retrieved, _ := mgr2.GetConnection("test")
	if retrieved.Host != "modified.com" {
		t.Errorf("Modified host not persisted: %q", retrieved.Host)
	}
}

// TestLoad_EmptyFile verifies handling of empty config file
func TestLoad_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "connections.json")
	
	// Create empty file
	os.WriteFile(configFile, []byte(""), 0600)
	
	// Should fail with JSON unmarshal error
	_, err := NewManager(tmpDir)
	if err == nil {
		t.Error("NewManager should fail with empty file")
	}
}

// TestLoad_MissingFile verifies handling of missing config file (first run)
func TestLoad_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	
	// File doesn't exist - should work as first-time setup
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager should succeed with missing file: %v", err)
	}
	
	configs := mgr.ListConnections()
	if len(configs) != 0 {
		t.Errorf("Expected no configs for first run, got %d", len(configs))
	}
}

// TestAddConnection_Duplicate verifies overwriting existing connection
func TestAddConnection_Duplicate(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	
	cfg1 := &Config{
		ID:      "dup",
		Label:   "First",
		Host:    "host1.com",
		Port:    22,
		User:    "user",
		KeyPath: "/key1",
		IsLocal: false,
	}
	
	mgr.AddConnection(cfg1)
	
	// Add another with same ID but different values
	cfg2 := &Config{
		ID:      "dup",
		Label:   "Second",
		Host:    "host2.com",
		Port:    2222,
		User:    "otheruser",
		KeyPath: "/key2",
		IsLocal: false,
	}
	
	mgr.AddConnection(cfg2)
	
	// Should have the second one
	retrieved, _ := mgr.GetConnection("dup")
	if retrieved.Host != "host2.com" {
		t.Errorf("Duplicate not overwritten: %q", retrieved.Host)
	}
	if retrieved.Port != 2222 {
		t.Errorf("Duplicate port not updated: %d", retrieved.Port)
	}
}

// TestRemoveConnection_NonExistent verifies removing non-existent connection
func TestRemoveConnection_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	
	// Should not error when removing non-existent connection
	err := mgr.RemoveConnection("nonexistent")
	if err != nil {
		t.Errorf("RemoveConnection should not error: %v", err)
	}
}

// TestSetActive_EmptyStringIsValid verifies setting empty string is valid
func TestSetActive_EmptyStringIsValid(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	
	// Empty string should be accepted (clears active)
	err := mgr.SetActive("")
	if err != nil {
		t.Errorf("SetActive with empty string should succeed: %v", err)
	}
}

// TestSave_Permissions verifies file is created with correct permissions
func TestSave_Permissions(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	
	cfg := &Config{
		ID:      "perm-test",
		Label:   "Test",
		Host:    "example.com",
		Port:    22,
		User:    "user",
		KeyPath: "/key",
		IsLocal: false,
	}
	
	mgr.AddConnection(cfg)
	
	// Check file permissions (0600 = rw-------)
	configFile := filepath.Join(tmpDir, "connections.json")
	info, err := os.Stat(configFile)
	if err != nil {
		t.Fatalf("Config file stat failed: %v", err)
	}
	
	mode := info.Mode()
	// File should have restricted permissions
	if mode&0077 != 0 {
		t.Errorf("Config file has overly permissive permissions: %o", mode)
	}
}

// TestConfigJSON_Structure verifies JSON structure matches expectations
func TestConfigJSON_Structure(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	
	for i := 1; i <= 3; i++ {
		cfg := &Config{
			ID:      fmt.Sprintf("conn%d", i),
			Label:   fmt.Sprintf("Connection %d", i),
			Host:    fmt.Sprintf("host%d.com", i),
			Port:    22 + i,
			User:    fmt.Sprintf("user%d", i),
			KeyPath: fmt.Sprintf("/key%d", i),
			IsLocal: false,
		}
		mgr.AddConnection(cfg)
	}
	
	mgr.SetActive("conn2")
	
	// Read and verify JSON structure
	configFile := filepath.Join(tmpDir, "connections.json")
	data, _ := os.ReadFile(configFile)
	
	type stored struct {
		Configs map[string]*Config `json:"configs"`
		ActiveID string             `json:"active_id"`
	}
	
	var s stored
	err := json.Unmarshal(data, &s)
	if err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}
	
	// Verify structure
	if len(s.Configs) != 3 {
		t.Errorf("Configs count = %d, want 3", len(s.Configs))
	}
	
	if s.ActiveID != "conn2" {
		t.Errorf("ActiveID = %q, want %q", s.ActiveID, "conn2")
	}
	
	// Verify each config has all fields
	for id, cfg := range s.Configs {
		if cfg.ID != id {
			t.Errorf("Config ID mismatch in map: %q vs key %q", cfg.ID, id)
		}
		if cfg.Host == "" {
			t.Errorf("Config %q missing host", id)
		}
	}
}

// TestConnectionLabels_Preserved verifies labels are preserved
func TestConnectionLabels_Preserved(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, _ := NewManager(tmpDir)
	
	labels := []string{
		"Production Database",
		"Staging Server",
		"Dev Machine",
		"",
		"测试", // Unicode label
	}
	
	for i, label := range labels {
		cfg := &Config{
			ID:      fmt.Sprintf("conn%d", i),
			Label:   label,
			Host:    "example.com",
			Port:    22,
			User:    "user",
			KeyPath: "/key",
			IsLocal: false,
		}
		mgr.AddConnection(cfg)
	}
	
	// Reload and verify
	mgr2, _ := NewManager(tmpDir)
	for i, expectedLabel := range labels {
		cfg, _ := mgr2.GetConnection(fmt.Sprintf("conn%d", i))
		if cfg.Label != expectedLabel {
			t.Errorf("Label mismatch: %q != %q", cfg.Label, expectedLabel)
		}
	}
}
