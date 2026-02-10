package controllers

import (
	"net/http"
)

// AdminDashboard: untuk sementara tidak dipakai,
// jadi langsung redirect ke halaman orders admin.
func (s *Server) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	// Pastikan user sudah login
	if !IsLoggedIn(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Pastikan user adalah admin
	user := s.CurrentUser(w, r)
	if !IsAdminUser(user) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Fitur dashboard dimatikan → langsung alihkan ke daftar order
	http.Redirect(w, r, "/admin/orders", http.StatusSeeOther)
}
