package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/nsilverman/archivist/internal/backend"
	"github.com/nsilverman/archivist/internal/models"
)

// listBackends handles GET /api/v1/backends
func (s *Server) listBackends(w http.ResponseWriter, r *http.Request) {
	backends := s.config.GetBackends()

	// Mask sensitive fields
	for i := range backends {
		backends[i].Config = maskSensitiveFields(backends[i].Config)
	}

	s.success(w, backends)
}

// getBackend handles GET /api/v1/backends/{id}
func (s *Server) getBackend(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	backend, err := s.config.GetBackend(id)
	if err != nil {
		s.error(w, "NOT_FOUND", "Backend not found", http.StatusNotFound)
		return
	}

	// Mask sensitive fields
	backend.Config = maskSensitiveFields(backend.Config)

	s.success(w, backend)
}

// createBackend handles POST /api/v1/backends
func (s *Server) createBackend(w http.ResponseWriter, r *http.Request) {
	var backendData models.Backend
	if err := json.NewDecoder(r.Body).Decode(&backendData); err != nil {
		s.error(w, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if backendData.Type == "" {
		s.error(w, "VALIDATION_ERROR", "Backend type is required", http.StatusBadRequest)
		return
	}
	if backendData.Name == "" {
		s.error(w, "VALIDATION_ERROR", "Backend name is required", http.StatusBadRequest)
		return
	}

	// Add backend
	if err := s.config.AddBackend(&backendData); err != nil {
		s.error(w, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	// Mask sensitive fields in response
	backendData.Config = maskSensitiveFields(backendData.Config)

	s.success(w, backendData)
}

// updateBackend handles PUT /api/v1/backends/{id}
func (s *Server) updateBackend(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var backendData models.Backend
	if err := json.NewDecoder(r.Body).Decode(&backendData); err != nil {
		s.error(w, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get existing backend to preserve masked sensitive fields
	existing, err := s.config.GetBackend(id)
	if err != nil {
		s.error(w, "NOT_FOUND", "Backend not found", http.StatusNotFound)
		return
	}

	// Merge config, preserving original values for masked fields
	backendData.Config = unmaskSensitiveFields(backendData.Config, existing.Config)

	// Update backend
	if err := s.config.UpdateBackend(id, &backendData); err != nil {
		s.error(w, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	// Mask sensitive fields in response
	backendData.Config = maskSensitiveFields(backendData.Config)

	s.success(w, backendData)
}

// deleteBackend handles DELETE /api/v1/backends/{id}
func (s *Server) deleteBackend(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if err := s.config.DeleteBackend(id); err != nil {
		if err.Error() == "backend is in use by task: " {
			s.error(w, "BACKEND_IN_USE", err.Error(), http.StatusConflict)
		} else {
			s.error(w, "NOT_FOUND", "Backend not found", http.StatusNotFound)
		}
		return
	}

	s.success(w, map[string]string{"message": "Backend deleted successfully"})
}

// testBackend handles POST /api/v1/backends/{id}/test
func (s *Server) testBackend(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	backendCfg, err := s.config.GetBackend(id)
	if err != nil {
		s.error(w, "NOT_FOUND", "Backend not found", http.StatusNotFound)
		return
	}

	// Create backend instance
	start := time.Now()
	backendInstance, err := backend.Factory(backendCfg, s.config)
	if err != nil {
		s.error(w, "CONNECTION_FAILED", err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := backendInstance.Close(); err != nil {
			log.Printf("Error closing backend instance: %v", err)
		}
	}()

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := backendInstance.Test(); err != nil {
		s.error(w, "CONNECTION_FAILED", err.Error(), http.StatusInternalServerError)
		return
	}

	latency := time.Since(start).Milliseconds()

	// Get storage usage
	usage, _ := backendInstance.GetUsage(ctx)

	// Update backend test status
	now := time.Now()
	backendCfg.LastTest = &now
	backendCfg.LastTestStatus = "success"
	if err := s.config.UpdateBackend(id, backendCfg); err != nil {
		log.Printf("Warning: failed to update backend test status: %v", err)
	}

	result := map[string]interface{}{
		"status":     "success",
		"message":    "Connection successful",
		"latency_ms": latency,
	}

	if usage != nil {
		result["storage_usage"] = usage
	}

	s.success(w, result)
}

// maskSensitiveFields masks sensitive configuration values
func maskSensitiveFields(config map[string]interface{}) map[string]interface{} {
	masked := make(map[string]interface{})
	for k, v := range config {
		switch k {
		case "access_key_id", "secret_access_key", "account_key", "application_key", "credentials_json", "refresh_token", "sas_token", "connection_string":
			if str, ok := v.(string); ok && len(str) > 0 {
				// Show first 3 chars if available, otherwise just ***
				if len(str) > 4 {
					masked[k] = str[:3] + "***"
				} else {
					masked[k] = "***"
				}
			} else {
				masked[k] = ""
			}
		default:
			masked[k] = v
		}
	}
	return masked
}

// unmaskSensitiveFields restores original sensitive values if the new value is masked
func unmaskSensitiveFields(newConfig, oldConfig map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{})

	// Copy all new config values
	for k, v := range newConfig {
		merged[k] = v
	}

	// Restore original values for sensitive fields if they appear to be masked
	sensitiveFields := []string{"access_key_id", "secret_access_key", "account_key", "application_key", "credentials_json", "refresh_token", "sas_token", "connection_string"}
	for _, field := range sensitiveFields {
		if newVal, exists := newConfig[field]; exists {
			if newStr, ok := newVal.(string); ok {
				// If the new value looks like a masked value (empty or contains ***), use the original
				if newStr == "" || newStr == "***" || (len(newStr) > 3 && newStr[len(newStr)-3:] == "***") {
					if oldVal, oldExists := oldConfig[field]; oldExists {
						merged[field] = oldVal
					}
				}
			}
		}
	}

	return merged
}
