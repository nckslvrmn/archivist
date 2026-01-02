package api

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

// listExecutions handles GET /api/v1/executions
func (s *Server) listExecutions(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	taskID := r.URL.Query().Get("task_id")
	status := r.URL.Query().Get("status")

	limitStr := r.URL.Query().Get("per_page")
	if limitStr == "" {
		limitStr = "20"
	}
	limit, _ := strconv.Atoi(limitStr)

	pageStr := r.URL.Query().Get("page")
	if pageStr == "" {
		pageStr = "1"
	}
	page, _ := strconv.Atoi(pageStr)
	offset := (page - 1) * limit

	// Query executions
	executions, err := s.db.ListExecutions(taskID, status, limit, offset)
	if err != nil {
		s.error(w, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	s.success(w, executions)
}

// getExecution handles GET /api/v1/executions/{id}
func (s *Server) getExecution(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	execution, err := s.db.GetExecution(id)
	if err != nil {
		s.error(w, "NOT_FOUND", "Execution not found", http.StatusNotFound)
		return
	}

	s.success(w, execution)
}

// cancelExecution handles POST /api/v1/executions/{id}/cancel
func (s *Server) cancelExecution(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if err := s.executor.Cancel(id); err != nil {
		s.error(w, "NOT_FOUND", err.Error(), http.StatusNotFound)
		return
	}

	s.success(w, map[string]interface{}{
		"id":     id,
		"status": "cancelled",
	})
}

// clearHistory handles DELETE /api/v1/executions
func (s *Server) clearHistory(w http.ResponseWriter, r *http.Request) {
	if err := s.db.ClearHistory(); err != nil {
		s.error(w, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	s.success(w, map[string]interface{}{
		"message": "Execution history cleared successfully",
	})
}
