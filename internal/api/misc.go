package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/nsilverman/archivist/internal/models"
)

// listSources handles GET /api/v1/sources
// Query params: ?path=/relative/path - to browse subdirectories
func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	settings := s.config.GetSettings()
	sourcesDir := s.config.ResolvePath(settings.SourcesDir)

	// Get optional path parameter for browsing subdirectories
	subPath := r.URL.Query().Get("path")

	// Build the target directory path
	var targetDir string
	if subPath != "" {
		// Security: ensure the path doesn't escape the sources directory
		cleanPath := filepath.Clean(subPath)
		if filepath.IsAbs(cleanPath) || filepath.HasPrefix(cleanPath, "..") {
			s.error(w, "VALIDATION_ERROR", "Invalid path", http.StatusBadRequest)
			return
		}
		targetDir = filepath.Join(sourcesDir, cleanPath)
	} else {
		targetDir = sourcesDir
	}

	// Check if target directory exists
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		s.success(w, map[string]interface{}{
			"path":    subPath,
			"entries": []models.SourceInfo{},
		})
		return
	}

	// Read directory
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		s.error(w, "INTERNAL_ERROR", "Failed to read directory", http.StatusInternalServerError)
		return
	}

	var sources []models.SourceInfo
	for _, entry := range entries {
		fullPath := filepath.Join(targetDir, entry.Name())
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		// Build relative path from sources directory
		relPath, err := filepath.Rel(sourcesDir, fullPath)
		if err != nil {
			relPath = entry.Name()
		}

		source := models.SourceInfo{
			Path:       filepath.Join(sourcesDir, relPath), // Full absolute path
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
		} else {
			source.Type = "file"
			source.Size = info.Size()
		}

		// Calculate size (simplified - just immediate files)
		if info.IsDir() {
			size, count := calculateDirSize(fullPath)
			source.Size = size
			source.FileCount = count
		}

		sources = append(sources, source)
	}

	s.success(w, map[string]interface{}{
		"path":    subPath,
		"entries": sources,
	})
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
