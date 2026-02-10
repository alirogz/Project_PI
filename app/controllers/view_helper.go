package controllers

import (
	"html/template"
	"net/http"
)

// Render HTML template sederhana dari folder /templates
func (s *Server) Render(w http.ResponseWriter, r *http.Request, tmpl string, data interface{}) {
	// tmpl di sini misalnya: "admin_dashboard.html"
	t, err := template.ParseFiles("templates/" + tmpl)
	if err != nil {
		http.Error(w, "error load template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := t.Execute(w, data); err != nil {
		http.Error(w, "error render template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}
