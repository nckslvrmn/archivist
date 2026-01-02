package api

import (
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/nsilverman/archivist/internal/models"
)

// listTasksHTML handles GET /api/v1/tasks/html
func (s *Server) listTasksHTML(w http.ResponseWriter, r *http.Request) {
	tasks := s.config.GetTasks()

	// Enrich with stats
	type TaskWithStats struct {
		Task  interface{}
		Stats *models.TaskStats
	}

	var enrichedTasks []TaskWithStats
	for _, task := range tasks {
		stats, err := s.db.GetTaskStats(task.ID)
		if err != nil {
			log.Printf("Error getting stats for task %s: %v", task.ID, err)
			// If there's an error getting stats, create an empty stats object
			stats = &models.TaskStats{}
		} else {
			log.Printf("Stats for task %s: Total=%d, Success=%d, Failure=%d",
				task.ID, stats.TotalExecutions, stats.SuccessCount, stats.FailureCount)
		}
		enrichedTasks = append(enrichedTasks, TaskWithStats{
			Task:  task,
			Stats: stats,
		})
	}

	s.htmlResponse(w, "tasks_list.html", enrichedTasks)
}

// createTaskFormHTML handles GET /api/v1/tasks/form/create
func (s *Server) createTaskFormHTML(w http.ResponseWriter, r *http.Request) {
	backends := s.config.GetBackends()

	data := map[string]interface{}{
		"Backends": backends,
	}

	s.htmlResponse(w, "task_form_create.html", data)
}

// editTaskFormHTML handles GET /api/v1/tasks/form/edit/{id}
func (s *Server) editTaskFormHTML(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	task, err := s.config.GetTask(id)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	backends := s.config.GetBackends()

	data := map[string]interface{}{
		"Task":     task,
		"Backends": backends,
	}

	s.htmlResponse(w, "task_form_edit.html", data)
}

// dryRunTaskHTML handles POST /api/v1/tasks/{id}/dry-run
func (s *Server) dryRunTaskHTML(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Execute dry run using the executor (uses nil for backendIDs to use all task backends)
	result, err := s.executor.ExecuteDryRun(id, nil)
	if err != nil {
		http.Error(w, "Dry run failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.htmlResponse(w, "task_dry_run.html", result)
}
