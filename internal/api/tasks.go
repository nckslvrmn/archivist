package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/nsilverman/archivist/internal/models"
)

// listTasks handles GET /api/v1/tasks
func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	tasks := s.config.GetTasks()

	// Enrich with stats
	var enrichedTasks []map[string]interface{}
	for _, task := range tasks {
		taskMap := map[string]interface{}{
			"id":               task.ID,
			"name":             task.Name,
			"description":      task.Description,
			"source_path":      task.SourcePath,
			"backend_ids":      task.BackendIDs,
			"schedule":         task.Schedule,
			"archive_options":  task.ArchiveOptions,
			"retention_policy": task.RetentionPolicy,
			"enabled":          task.Enabled,
			"created_at":       task.CreatedAt,
			"updated_at":       task.UpdatedAt,
			"last_run":         task.LastRun,
			"next_run":         task.NextRun,
		}

		// Add stats
		stats, err := s.db.GetTaskStats(task.ID)
		if err == nil {
			taskMap["stats"] = stats
		}

		enrichedTasks = append(enrichedTasks, taskMap)
	}

	s.success(w, enrichedTasks)
}

// getTask handles GET /api/v1/tasks/{id}
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	task, err := s.config.GetTask(id)
	if err != nil {
		s.error(w, "NOT_FOUND", "Task not found", http.StatusNotFound)
		return
	}

	s.success(w, task)
}

// createTask handles POST /api/v1/tasks
func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var task models.Task
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		s.error(w, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if task.Name == "" {
		s.error(w, "VALIDATION_ERROR", "Task name is required", http.StatusBadRequest)
		return
	}
	if task.SourcePath == "" {
		s.error(w, "VALIDATION_ERROR", "Source path is required", http.StatusBadRequest)
		return
	}
	if len(task.BackendIDs) == 0 {
		s.error(w, "VALIDATION_ERROR", "At least one backend is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if task.ArchiveOptions.Format == "" {
		task.ArchiveOptions.Format = "tar.gz"
	}
	if task.ArchiveOptions.Compression == "" {
		task.ArchiveOptions.Compression = "gzip"
	}
	if !task.ArchiveOptions.UseTimestamp && task.ArchiveOptions.NamePattern == "" {
		task.ArchiveOptions.UseTimestamp = true // Default to timestamped backups
	}

	// Add task
	if err := s.config.AddTask(&task); err != nil {
		s.error(w, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	// Schedule task if enabled
	if task.Enabled && task.Schedule.Type != "manual" {
		if err := s.scheduler.ScheduleTask(task.ID); err != nil {
			log.Printf("Warning: failed to schedule task %s: %v", task.ID, err)
		}
	}

	s.success(w, task)
}

// updateTask handles PUT /api/v1/tasks/{id}
func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var task models.Task
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		s.error(w, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}

	// Update task
	if err := s.config.UpdateTask(id, &task); err != nil {
		s.error(w, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	// Reschedule task
	if err := s.scheduler.ScheduleTask(id); err != nil {
		log.Printf("Warning: failed to reschedule task %s: %v", id, err)
	}

	s.success(w, task)
}

// deleteTask handles DELETE /api/v1/tasks/{id}
func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Check if task is running
	if s.executor.IsRunning(id) {
		s.error(w, "TASK_RUNNING", "Cannot delete a running task", http.StatusConflict)
		return
	}

	// Unschedule task
	s.scheduler.UnscheduleTask(id)

	// Delete task
	if err := s.config.DeleteTask(id); err != nil {
		s.error(w, "NOT_FOUND", "Task not found", http.StatusNotFound)
		return
	}

	s.success(w, map[string]string{"message": "Task deleted successfully"})
}

// executeTask handles POST /api/v1/tasks/{id}/execute?dry_run=true&backend_ids=id1,id2
func (s *Server) executeTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Check for dry_run query parameter
	dryRun := r.URL.Query().Get("dry_run") == "true"

	if dryRun {
		// Parse optional backend_ids parameter
		var backendIDs []string
		if backendIDsParam := r.URL.Query().Get("backend_ids"); backendIDsParam != "" {
			backendIDs = strings.Split(backendIDsParam, ",")
			// Trim spaces from each ID
			for i, id := range backendIDs {
				backendIDs[i] = strings.TrimSpace(id)
			}
		}

		// Execute dry run
		result, err := s.executor.ExecuteDryRun(id, backendIDs)
		if err != nil {
			s.error(w, "DRY_RUN_ERROR", err.Error(), http.StatusInternalServerError)
			return
		}
		s.success(w, result)
	} else {
		// Normal execution
		executionID, err := s.executor.Execute(id)
		if err != nil {
			s.error(w, "EXECUTION_ERROR", err.Error(), http.StatusInternalServerError)
			return
		}

		s.success(w, map[string]interface{}{
			"execution_id": executionID,
			"status":       "running",
		})
	}
}

// enableTask handles POST /api/v1/tasks/{id}/enable
func (s *Server) enableTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	task, err := s.config.GetTask(id)
	if err != nil {
		s.error(w, "NOT_FOUND", "Task not found", http.StatusNotFound)
		return
	}

	task.Enabled = true
	if err := s.config.UpdateTask(id, task); err != nil {
		s.error(w, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	// Schedule task if not manual
	if task.Schedule.Type != "manual" {
		if err := s.scheduler.ScheduleTask(id); err != nil {
			log.Printf("Warning: failed to schedule task %s: %v", id, err)
		}
	}

	s.success(w, map[string]interface{}{
		"id":      id,
		"enabled": true,
	})
}

// disableTask handles POST /api/v1/tasks/{id}/disable
func (s *Server) disableTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	task, err := s.config.GetTask(id)
	if err != nil {
		s.error(w, "NOT_FOUND", "Task not found", http.StatusNotFound)
		return
	}

	task.Enabled = false
	if err := s.config.UpdateTask(id, task); err != nil {
		s.error(w, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	// Unschedule task
	s.scheduler.UnscheduleTask(id)

	s.success(w, map[string]interface{}{
		"id":      id,
		"enabled": false,
	})
}
