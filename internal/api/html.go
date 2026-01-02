package api

import (
	"html/template"
	"log"
	"net/http"
	"path/filepath"
)

// htmlResponse renders an HTML template
func (s *Server) htmlResponse(w http.ResponseWriter, tmplName string, data interface{}) {
	tmplPath := filepath.Join("web", "templates", tmplName)
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		log.Printf("Template parse error for %s: %v", tmplName, err)
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Template execute error for %s: %v", tmplName, err)
		http.Error(w, "Rendering error: "+err.Error(), http.StatusInternalServerError)
	}
}
