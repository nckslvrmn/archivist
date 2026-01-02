package api

import (
	"net/http"

	"github.com/gorilla/mux"
)

// listBackendsHTML handles GET /api/v1/backends/html
func (s *Server) listBackendsHTML(w http.ResponseWriter, r *http.Request) {
	backends := s.config.GetBackends()
	s.htmlResponse(w, "backends_list.html", backends)
}

// createBackendFormHTML handles GET /api/v1/backends/form/create
func (s *Server) createBackendFormHTML(w http.ResponseWriter, r *http.Request) {
	s.htmlResponse(w, "backend_form_create.html", nil)
}

// editBackendFormHTML handles GET /api/v1/backends/form/edit/{id}
func (s *Server) editBackendFormHTML(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	backend, err := s.config.GetBackend(id)
	if err != nil {
		http.Error(w, "Backend not found", http.StatusNotFound)
		return
	}

	s.htmlResponse(w, "backend_form_edit.html", backend)
}
