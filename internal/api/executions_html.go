package api

import (
	"net/http"
)

// listExecutionsHTML handles GET /api/v1/executions/html
func (s *Server) listExecutionsHTML(w http.ResponseWriter, r *http.Request) {
	executions, err := s.db.ListExecutions("", "", 100, 0)
	if err != nil {
		http.Error(w, "Failed to load executions", http.StatusInternalServerError)
		return
	}

	s.htmlResponse(w, "executions_list.html", executions)
}
