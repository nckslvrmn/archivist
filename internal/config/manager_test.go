package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsilverman/archivist/internal/models"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()

	root := t.TempDir()
	m, err := NewManager(filepath.Join(root, "config", "config.json"), root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.CreateDefaultWithPaths(filepath.Join(root, "temp"), filepath.Join(root, "sources")); err != nil {
		t.Fatalf("CreateDefaultWithPaths: %v", err)
	}
	return m
}

func addBackend(t *testing.T, m *Manager, id, secret string) {
	t.Helper()
	b := &models.Backend{
		ID:     id,
		Type:   "s3",
		Name:   id,
		Config: map[string]interface{}{"bucket": "b", "secret_access_key": secret},
	}
	if err := m.AddBackend(b); err != nil {
		t.Fatalf("AddBackend: %v", err)
	}
}

// TestGetReturnsDeepCopy is the regression test for credential loss: the API
// masks secrets by writing into the map returned by Get, and a shallow copy
// shares that map with the live configuration, so masked placeholders would
// replace the real credentials and be persisted by the next save.
func TestGetReturnsDeepCopy(t *testing.T) {
	m := newTestManager(t)
	addBackend(t, m, "b1", "REAL-SECRET")

	cfg := m.Get()
	// Exactly what api.getConfig does.
	cfg.Backends[0].Config = map[string]interface{}{"secret_access_key": "REA***"}

	live, err := m.GetBackend("b1")
	if err != nil {
		t.Fatal(err)
	}
	if got := live.Config["secret_access_key"]; got != "REAL-SECRET" {
		t.Fatalf("live credential = %v, want REAL-SECRET (masking leaked into live config)", got)
	}

	// Mutating a value inside the returned map must not leak either.
	cfg2 := m.Get()
	cfg2.Backends[0].Config["secret_access_key"] = "clobbered"
	live, _ = m.GetBackend("b1")
	if got := live.Config["secret_access_key"]; got != "REAL-SECRET" {
		t.Fatalf("live credential = %v after map write, want REAL-SECRET", got)
	}
}

// TestSaveKeepsCredentialsOnDisk exercises the full path that lost secrets:
// read the config over the API, then trigger a save (as every task run does).
func TestSaveKeepsCredentialsOnDisk(t *testing.T) {
	m := newTestManager(t)
	addBackend(t, m, "b1", "REAL-SECRET")

	cfg := m.Get()
	cfg.Backends[0].Config["secret_access_key"] = "REA***"

	if err := m.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk models.Config
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatal(err)
	}
	if got := onDisk.Backends[0].Config["secret_access_key"]; got != "REAL-SECRET" {
		t.Fatalf("persisted credential = %v, want REAL-SECRET", got)
	}
}

// TestGetTasksDeepCopy: mutating a returned task must not touch live state.
func TestGetTasksDeepCopy(t *testing.T) {
	m := newTestManager(t)
	addBackend(t, m, "b1", "s")

	task := &models.Task{
		ID:         "t1",
		Name:       "task one",
		SourcePath: "sources/x",
		BackendIDs: []string{"b1"},
		Schedule:   models.Schedule{Type: "manual"},
		Enabled:    true,
	}
	if err := m.AddTask(task); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	tasks := m.GetTasks()
	tasks[0].BackendIDs[0] = "hacked"
	tasks[0].Name = "renamed"

	live, err := m.GetTask("t1")
	if err != nil {
		t.Fatal(err)
	}
	if live.BackendIDs[0] != "b1" {
		t.Errorf("live BackendIDs[0] = %s, want b1", live.BackendIDs[0])
	}
	if live.Name != "task one" {
		t.Errorf("live Name = %s, want %q", live.Name, "task one")
	}
}

// TestAddBackendCopiesConfig: the caller's map must not stay wired into the
// live configuration after the add.
func TestAddBackendCopiesConfig(t *testing.T) {
	m := newTestManager(t)

	cfgMap := map[string]interface{}{"secret_access_key": "REAL-SECRET"}
	if err := m.AddBackend(&models.Backend{ID: "b1", Type: "s3", Name: "b1", Config: cfgMap}); err != nil {
		t.Fatal(err)
	}

	cfgMap["secret_access_key"] = "changed-by-caller"

	live, err := m.GetBackend("b1")
	if err != nil {
		t.Fatal(err)
	}
	if got := live.Config["secret_access_key"]; got != "REAL-SECRET" {
		t.Fatalf("live credential = %v, want REAL-SECRET", got)
	}
}

// TestLoadRoundTrip checks a saved config reloads intact.
func TestLoadRoundTrip(t *testing.T) {
	m := newTestManager(t)
	addBackend(t, m, "b1", "REAL-SECRET")

	reloaded, err := NewManager(m.configPath, m.rootDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	b, err := reloaded.GetBackend("b1")
	if err != nil {
		t.Fatal(err)
	}
	if got := b.Config["secret_access_key"]; got != "REAL-SECRET" {
		t.Errorf("reloaded credential = %v, want REAL-SECRET", got)
	}
}
