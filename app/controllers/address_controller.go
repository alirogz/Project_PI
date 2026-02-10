package controllers

import (
	"net/http"

	"github.com/alirogz/goshop/app/models"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func (server *Server) AddressesIndex(w http.ResponseWriter, r *http.Request) {
	ren := userRender()

	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	var addresses []models.Address
	server.DB.
		Where("user_id = ?", user.ID).
		// is_primary = true ditaruh paling atas
		Order("is_primary DESC, created_at DESC").
		Find(&addresses)

	data := map[string]interface{}{
		"user":      user,
		"isAdmin":   IsAdminUser(user),
		"cartCount": server.GetCartCount(w, r),
		"addresses": addresses,
		"flashes":   GetFlash(w, r, "success"),
	}

	_ = ren.HTML(w, http.StatusOK, "addresses_index", data)
}

func (server *Server) AddressNew(w http.ResponseWriter, r *http.Request) {
	ren := userRender()

	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	data := map[string]interface{}{
		"user":       user,
		"isAdmin":    IsAdminUser(user),
		"cartCount":  server.GetCartCount(w, r),
		"address":    models.Address{}, // kosong, untuk form baru
		"formAction": "/addresses",
		"formTitle":  "Tambah Alamat",
	}

	_ = ren.HTML(w, http.StatusOK, "addresses_form", data)
}

func (server *Server) AddressCreate(w http.ResponseWriter, r *http.Request) {
	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	address := models.Address{
		ID:       uuid.New().String(),
		UserID:   user.ID,
		Name:     r.FormValue("recipient_name"),
		Phone:    r.FormValue("phone"),
		Address1: r.FormValue("address_line"),
		CityID:   r.FormValue("city"),
		PostCode: r.FormValue("postcode"),
	}

	makeDefault := r.FormValue("is_default") == "on"

	// kalau ini alamat pertama user, jadikan primary
	var count int64
	server.DB.Model(&models.Address{}).Where("user_id = ?", user.ID).Count(&count)
	if count == 0 {
		address.IsPrimary = true
	} else if makeDefault {
		// non-primary-kan yang lain
		server.DB.Model(&models.Address{}).
			Where("user_id = ?", user.ID).
			Update("is_primary", false)
		address.IsPrimary = true
	}

	if err := server.DB.Create(&address).Error; err != nil {
		http.Error(w, "gagal menyimpan alamat", http.StatusInternalServerError)
		return
	}

	SetFlash(w, r, "success", "Alamat berhasil ditambahkan")
	http.Redirect(w, r, "/profile#alamat", http.StatusSeeOther)
}

func (server *Server) AddressEdit(w http.ResponseWriter, r *http.Request) {
	ren := userRender()

	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"] // ID bertipe string (UUID), bukan angka

	var address models.Address
	if err := server.DB.Where("id = ? AND user_id = ?", id, user.ID).First(&address).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	data := map[string]interface{}{
		"user":       user,
		"isAdmin":    IsAdminUser(user),
		"cartCount":  server.GetCartCount(w, r),
		"address":    address,
		"formAction": "/addresses/" + id + "/update",
		"formTitle":  "Edit Alamat",
	}

	_ = ren.HTML(w, http.StatusOK, "addresses_form", data)
}

func (server *Server) AddressUpdate(w http.ResponseWriter, r *http.Request) {
	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	var address models.Address
	if err := server.DB.Where("id = ? AND user_id = ?", id, user.ID).First(&address).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	address.Name = r.FormValue("recipient_name")
	address.Phone = r.FormValue("phone")
	address.Address1 = r.FormValue("address_line")
	address.CityID = r.FormValue("city")
	address.PostCode = r.FormValue("postcode")

	makeDefault := r.FormValue("is_default") == "on"

	if makeDefault && !address.IsPrimary {
		// non-primary-kan semua alamat user lain
		server.DB.Model(&models.Address{}).
			Where("user_id = ?", user.ID).
			Update("is_primary", false)
		address.IsPrimary = true
	}

	if err := server.DB.Save(&address).Error; err != nil {
		http.Error(w, "gagal update alamat", http.StatusInternalServerError)
		return
	}

	SetFlash(w, r, "success", "Alamat berhasil diperbarui")
	http.Redirect(w, r, "/profile#alamat", http.StatusSeeOther)
}

func (server *Server) AddressDelete(w http.ResponseWriter, r *http.Request) {
	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	var address models.Address
	if err := server.DB.Where("id = ? AND user_id = ?", id, user.ID).First(&address).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	wasPrimary := address.IsPrimary

	if err := server.DB.Delete(&address).Error; err != nil {
		http.Error(w, "gagal menghapus alamat", http.StatusInternalServerError)
		return
	}

	// kalau yang dihapus itu primary, jadikan salah satu alamat lain sebagai primary (kalau ada)
	if wasPrimary {
		var another models.Address
		if err := server.DB.
			Where("user_id = ?", user.ID).
			First(&another).Error; err == nil {
			another.IsPrimary = true
			server.DB.Save(&another)
		}
	}

	SetFlash(w, r, "success", "Alamat berhasil dihapus")
	http.Redirect(w, r, "/profile#alamat", http.StatusSeeOther)
}

func (server *Server) AddressMakeDefault(w http.ResponseWriter, r *http.Request) {
	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	var address models.Address
	if err := server.DB.Where("id = ? AND user_id = ?", id, user.ID).First(&address).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	// non-primary-kan yang lain
	server.DB.Model(&models.Address{}).
		Where("user_id = ?", user.ID).
		Update("is_primary", false)

	address.IsPrimary = true
	server.DB.Save(&address)

	SetFlash(w, r, "success", "Alamat utama berhasil diubah")
	http.Redirect(w, r, "/profile#alamat", http.StatusSeeOther)
}
