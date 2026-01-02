package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/nsilverman/archivist/internal/models"
)

// listSources handles GET /api/v1/sources
func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	settings := s.config.GetSettings()
	sourcesDir := s.config.ResolvePath(settings.SourcesDir)

	// Check if sources directory exists
	if _, err := os.Stat(sourcesDir); os.IsNotExist(err) {
		s.success(w, []models.SourceInfo{})
		return
	}

	// Read directory
	entries, err := os.ReadDir(sourcesDir)
	if err != nil {
		s.error(w, "INTERNAL_ERROR", "Failed to read sources directory", http.StatusInternalServerError)
		return
	}

	var sources []models.SourceInfo
	for _, entry := range entries {
		fullPath := filepath.Join(sourcesDir, entry.Name())
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		source := models.SourceInfo{
			Path:       fullPath,
			Name:       entry.Name(),
			Accessible: true,
		}

		// Check if it's a symlink
		if entry.Type()&os.ModeSymlink != 0 {
			source.Type = "symlink"
			target, err := os.Readlink(fullPath)
			if err == nil {
				source.Target = target
				// Check if target is accessible
				if _, err := os.Stat(target); err != nil {
					source.Accessible = false
				}
			}
		} else if info.IsDir() {
			source.Type = "directory"
		}

		// Calculate size (simplified - just immediate files)
		if info.IsDir() {
			size, count := calculateDirSize(fullPath)
			source.Size = size
			source.FileCount = count
		}

		sources = append(sources, source)
	}

	s.success(w, sources)
}

// getConfig handles GET /api/v1/config
func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	config := s.config.Get()

	// Mask sensitive fields in backends
	for i := range config.Backends {
		config.Backends[i].Config = maskSensitiveFields(config.Backends[i].Config)
	}

	s.success(w, map[string]interface{}{
		"version":  config.Version,
		"settings": config.Settings,
	})
}

// updateSettings handles PUT /api/v1/config/settings
func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var settings models.Settings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		s.error(w, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.config.UpdateSettings(settings); err != nil {
		s.error(w, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	s.success(w, map[string]interface{}{
		"settings": settings,
	})
}

// calculateDirSize calculates the total size of files in a directory (non-recursive)
func calculateDirSize(path string) (size int64, count int) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, 0
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			info, err := entry.Info()
			if err == nil {
				size += info.Size()
				count++
			}
		}
	}

	return size, count
}
