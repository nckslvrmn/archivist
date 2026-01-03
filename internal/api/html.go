package api

import (
	"log"
	"net/http"
)

// htmlResponse renders a cached HTML template
func (s *Server) htmlResponse(w http.ResponseWriter, tmplName string, data interface{}) {
	tmpl, ok := s.templates[tmplName]
	if !ok {
		log.Printf("Template not found: %s", tmplName)
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Template execute error for %s: %v", tmplName, err)
		http.Error(w, "Rendering error: "+err.Error(), http.StatusInternalServerError)
	}
}
