package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nsilverman/archivist/internal/models"
)

// Manager manages application configuration
type Manager struct {
	configPath string
	rootDir    string
	config     *models.Config
	mu         sync.RWMutex
}

// NewManager creates a new configuration manager
func NewManager(configPath string, rootDir string) (*Manager, error) {
	// Ensure the config directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	return &Manager{
		configPath: configPath,
		rootDir:    rootDir,
	}, nil
}

// Load loads the configuration from disk
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}

	var config models.Config
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	// Validate configuration
	if err := m.validate(&config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	m.config = &config
	return nil
}

// Save saves the configuration to disk
func (m *Manager) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.saveInternal()
}

// saveInternal saves without locking (must be called with lock held)
func (m *Manager) saveInternal() error {
	// Marshal with indentation for readability
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal configuration: %w", err)
	}

	// Write atomically by writing to a temp file and renaming
	tempPath := m.configPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write configuration: %w", err)
	}

	if err := os.Rename(tempPath, m.configPath); err != nil {
		if removeErr := os.Remove(tempPath); removeErr != nil {
			log.Printf("Warning: failed to remove temp file: %v", removeErr)
		}
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	return nil
}

// CreateDefault creates a default configuration with default paths
func (m *Manager) CreateDefault() error {
	return m.CreateDefaultWithPaths("/data/temp", "/data/sources")
}

// CreateDefaultWithPaths creates a default configuration with specified paths
func (m *Manager) CreateDefaultWithPaths(tempDir, sourcesDir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create default configuration
	m.config = &models.Config{
		Version:  "1.0",
		Backends: []models.Backend{},
		Tasks:    []models.Task{},
		Settings: models.Settings{
			TempDir:               tempDir,
			SourcesDir:            sourcesDir,
			MaxConcurrentTasks:    3,
			MaxConcurrentBackends: 4,
			LogLevel:              "info",
		},
	}

	return m.saveInternal()
}

// Get returns a deep copy of the current configuration.
//
// The copy must be deep: callers (e.g. the API's credential masking) mutate
// the returned backends in place, and a shallow copy shares the same slice
// backing array and config maps as the live configuration — masked
// placeholders would overwrite real credentials in memory and then be
// persisted by the next Save.
func (m *Manager) Get() *models.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	configCopy := *m.config
	configCopy.Backends = cloneBackends(m.config.Backends)
	configCopy.Tasks = cloneTasks(m.config.Tasks)
	return &configCopy
}

// cloneBackends deep-copies a backend slice, including each Config map.
func cloneBackends(in []models.Backend) []models.Backend {
	out := make([]models.Backend, len(in))
	for i := range in {
		out[i] = cloneBackend(in[i])
	}
	return out
}

// cloneBackend deep-copies a single backend.
func cloneBackend(b models.Backend) models.Backend {
	c := b
	c.Config = deepCopyMap(b.Config)
	if b.LastTest != nil {
		t := *b.LastTest
		c.LastTest = &t
	}
	return c
}

// cloneTasks deep-copies a task slice.
func cloneTasks(in []models.Task) []models.Task {
	out := make([]models.Task, len(in))
	for i := range in {
		out[i] = cloneTask(in[i])
	}
	return out
}

// cloneTask deep-copies a single task.
func cloneTask(t models.Task) models.Task {
	c := t
	if t.BackendIDs != nil {
		c.BackendIDs = make([]string, len(t.BackendIDs))
		copy(c.BackendIDs, t.BackendIDs)
	}
	if t.LastRun != nil {
		v := *t.LastRun
		c.LastRun = &v
	}
	if t.NextRun != nil {
		v := *t.NextRun
		c.NextRun = &v
	}
	return c
}

// deepCopyMap copies a JSON-shaped map (strings, numbers, bools, nested maps
// and slices) so the caller cannot reach back into the live configuration.
func deepCopyMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return deepCopyMap(val)
	case []interface{}:
		out := make([]interface{}, len(val))
		for i := range val {
			out[i] = deepCopyValue(val[i])
		}
		return out
	default:
		// Remaining JSON values (string, float64, bool, nil) are immutable.
		return v
	}
}

// GetSettings returns the current settings
func (m *Manager) GetSettings() models.Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Settings
}

// ResolvePath resolves a path relative to the root directory if it's not absolute
func (m *Manager) ResolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(m.rootDir, path)
}

// UpdateSettings updates the settings
func (m *Manager) UpdateSettings(settings models.Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Settings = settings
	return m.saveInternal()
}

// GetBackend returns a backend by ID
func (m *Manager) GetBackend(id string) (*models.Backend, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.config.Backends {
		if m.config.Backends[i].ID == id {
			backend := cloneBackend(m.config.Backends[i])
			return &backend, nil
		}
	}
	return nil, fmt.Errorf("backend not found: %s", id)
}

// GetBackends returns all backends
func (m *Manager) GetBackends() []models.Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return cloneBackends(m.config.Backends)
}

// AddBackend adds a new backend
func (m *Manager) AddBackend(backend *models.Backend) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate ID if not provided
	if backend.ID == "" {
		backend.ID = uuid.New().String()
	}

	// Check for duplicate ID
	for _, b := range m.config.Backends {
		if b.ID == backend.ID {
			return fmt.Errorf("backend with ID %s already exists", backend.ID)
		}
	}

	// Set timestamps
	now := time.Now()
	backend.CreatedAt = now
	backend.UpdatedAt = now

	// Store a deep copy so a caller that keeps hold of the config map cannot
	// mutate the live configuration behind the lock.
	m.config.Backends = append(m.config.Backends, cloneBackend(*backend))
	return m.saveInternal()
}

// UpdateBackend updates an existing backend
func (m *Manager) UpdateBackend(id string, backend *models.Backend) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.config.Backends {
		if m.config.Backends[i].ID == id {
			// Preserve original ID and creation time
			backend.ID = id
			backend.CreatedAt = m.config.Backends[i].CreatedAt
			backend.UpdatedAt = time.Now()
			m.config.Backends[i] = cloneBackend(*backend)
			return m.saveInternal()
		}
	}
	return fmt.Errorf("backend not found: %s", id)
}

// DeleteBackend deletes a backend
func (m *Manager) DeleteBackend(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if backend is used by any task
	for _, task := range m.config.Tasks {
		for _, backendID := range task.BackendIDs {
			if backendID == id {
				return fmt.Errorf("backend is in use by task: %s", task.Name)
			}
		}
	}

	// Find and remove backend
	for i := range m.config.Backends {
		if m.config.Backends[i].ID == id {
			m.config.Backends = append(m.config.Backends[:i], m.config.Backends[i+1:]...)
			return m.saveInternal()
		}
	}
	return fmt.Errorf("backend not found: %s", id)
}

// GetTask returns a task by ID
func (m *Manager) GetTask(id string) (*models.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.config.Tasks {
		if m.config.Tasks[i].ID == id {
			task := cloneTask(m.config.Tasks[i])
			return &task, nil
		}
	}
	return nil, fmt.Errorf("task not found: %s", id)
}

// GetTasks returns all tasks
func (m *Manager) GetTasks() []models.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return cloneTasks(m.config.Tasks)
}

// AddTask adds a new task
func (m *Manager) AddTask(task *models.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate ID if not provided
	if task.ID == "" {
		task.ID = uuid.New().String()
	}

	// Check for duplicate ID
	for _, t := range m.config.Tasks {
		if t.ID == task.ID {
			return fmt.Errorf("task with ID %s already exists", task.ID)
		}
	}

	// Validate backends exist - build map for O(n) lookup
	backendMap := make(map[string]bool, len(m.config.Backends))
	for _, backend := range m.config.Backends {
		backendMap[backend.ID] = true
	}
	for _, backendID := range task.BackendIDs {
		if !backendMap[backendID] {
			return fmt.Errorf("backend not found: %s", backendID)
		}
	}

	// Set timestamps
	now := time.Now()
	task.CreatedAt = now
	task.UpdatedAt = now

	m.config.Tasks = append(m.config.Tasks, cloneTask(*task))
	return m.saveInternal()
}

// UpdateTask updates an existing task
func (m *Manager) UpdateTask(id string, task *models.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.config.Tasks {
		if m.config.Tasks[i].ID == id {
			// Preserve original ID and creation time
			task.ID = id
			task.CreatedAt = m.config.Tasks[i].CreatedAt
			task.UpdatedAt = time.Now()

			// Validate backends exist - build map for O(n) lookup
			backendMap := make(map[string]bool, len(m.config.Backends))
			for _, backend := range m.config.Backends {
				backendMap[backend.ID] = true
			}
			for _, backendID := range task.BackendIDs {
				if !backendMap[backendID] {
					return fmt.Errorf("backend not found: %s", backendID)
				}
			}

			m.config.Tasks[i] = cloneTask(*task)
			return m.saveInternal()
		}
	}
	return fmt.Errorf("task not found: %s", id)
}

// DeleteTask deletes a task
func (m *Manager) DeleteTask(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.config.Tasks {
		if m.config.Tasks[i].ID == id {
			m.config.Tasks = append(m.config.Tasks[:i], m.config.Tasks[i+1:]...)
			return m.saveInternal()
		}
	}
	return fmt.Errorf("task not found: %s", id)
}

// UpdateTaskSchedule updates the last run and next run times for a task
func (m *Manager) UpdateTaskSchedule(id string, lastRun, nextRun *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.config.Tasks {
		if m.config.Tasks[i].ID == id {
			m.config.Tasks[i].LastRun = lastRun
			m.config.Tasks[i].NextRun = nextRun
			return m.saveInternal()
		}
	}
	return fmt.Errorf("task not found: %s", id)
}

// validate validates the configuration
func (m *Manager) validate(config *models.Config) error {
	if config.Version == "" {
		return fmt.Errorf("version is required")
	}

	// Validate backends
	backendIDs := make(map[string]bool)
	for _, backend := range config.Backends {
		if backend.ID == "" {
			return fmt.Errorf("backend ID is required")
		}
		if backendIDs[backend.ID] {
			return fmt.Errorf("duplicate backend ID: %s", backend.ID)
		}
		backendIDs[backend.ID] = true

		if backend.Type == "" {
			return fmt.Errorf("backend type is required for backend: %s", backend.ID)
		}
		if backend.Name == "" {
			return fmt.Errorf("backend name is required for backend: %s", backend.ID)
		}
	}

	// Validate tasks
	taskIDs := make(map[string]bool)
	for _, task := range config.Tasks {
		if task.ID == "" {
			return fmt.Errorf("task ID is required")
		}
		if taskIDs[task.ID] {
			return fmt.Errorf("duplicate task ID: %s", task.ID)
		}
		taskIDs[task.ID] = true

		if task.Name == "" {
			return fmt.Errorf("task name is required for task: %s", task.ID)
		}
		if task.SourcePath == "" {
			return fmt.Errorf("source path is required for task: %s", task.ID)
		}
		if len(task.BackendIDs) == 0 {
			return fmt.Errorf("at least one backend is required for task: %s", task.ID)
		}

		// Validate backend references
		for _, backendID := range task.BackendIDs {
			if !backendIDs[backendID] {
				return fmt.Errorf("task %s references non-existent backend: %s", task.ID, backendID)
			}
		}
	}

	return nil
}
