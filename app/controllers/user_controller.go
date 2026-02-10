package controllers

import (
	"net/http"

	"github.com/alirogz/goshop/app/models"
	"github.com/google/uuid"
)

func (server *Server) Login(w http.ResponseWriter, r *http.Request) {
	ren := userRender()

	data := map[string]interface{}{
		"user":      nil,
		"isAdmin":   false,
		"cartCount": server.GetCartCount(w, r),
		"error":     GetFlash(w, r, "error"),
	}

	_ = ren.HTML(w, http.StatusOK, "login", data)
}

func (server *Server) Register(w http.ResponseWriter, r *http.Request) {
	ren := userRender()

	data := map[string]interface{}{
		"user":      nil,
		"isAdmin":   false,
		"cartCount": server.GetCartCount(w, r),
		"error":     GetFlash(w, r, "error"),
	}

	_ = ren.HTML(w, http.StatusOK, "register", data)
}

func (server *Server) DoLogin(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	userModel := models.User{}
	user, err := userModel.FindByEmail(server.DB, email)
	if err != nil {
		SetFlash(w, r, "error", "email or password invalid")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !ComparePassword(password, user.Password) {
		SetFlash(w, r, "error", "email or password invalid")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	session, _ := store.Get(r, sessionUser)
	session.Values["id"] = user.ID
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (server *Server) DoRegister(w http.ResponseWriter, r *http.Request) {
	firstName := r.FormValue("first_name")
	lastName := r.FormValue("last_name")
	email := r.FormValue("email")
	password := r.FormValue("password")

	if firstName == "" || lastName == "" || email == "" || password == "" {
		SetFlash(w, r, "error", "First name, last name, email and password are required!")
		http.Redirect(w, r, "/register", http.StatusSeeOther)
		return
	}

	userModel := models.User{}
	existUser, _ := userModel.FindByEmail(server.DB, email)
	if existUser != nil {
		SetFlash(w, r, "error", "Sorry, email already registered")
		http.Redirect(w, r, "/register", http.StatusSeeOther)
		return
	}

	hashedPassword, _ := MakePassword(password)
	params := &models.User{
		ID:        uuid.New().String(),
		FirstName: firstName,
		LastName:  lastName,
		Email:     email,
		Password:  hashedPassword,
	}

	user, err := userModel.CreateUser(server.DB, params)
	if err != nil {
		SetFlash(w, r, "error", "Sorry, registration failed")
		http.Redirect(w, r, "/register", http.StatusSeeOther)
		return
	}

	session, _ := store.Get(r, sessionUser)
	session.Values["id"] = user.ID
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (server *Server) Logout(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, sessionUser)

	session.Values["id"] = nil
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (server *Server) ProfileIndex(w http.ResponseWriter, r *http.Request) {
	ren := userRender()

	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// ambil semua alamat user (buat ditampilkan di tab "Alamat Saya")
	var addresses []models.Address
	server.DB.
		Where("user_id = ?", user.ID).
		Order("is_primary DESC, created_at DESC").
		Find(&addresses)

	// ambil alamat utama (optional)
	var address models.Address
	server.DB.
		Where("user_id = ? AND is_primary = ?", user.ID, true).
		First(&address)

	data := map[string]interface{}{
		"user":      user,
		"isAdmin":   IsAdminUser(user),
		"cartCount": server.GetCartCount(w, r),

		// untuk tab alamat
		"addresses": addresses,

		// kalau template profile kamu masih pakai "Address" (alamat utama)
		"Address": address,

		// flash messages
		"flashes": GetFlash(w, r, "success"),
		"errors":  GetFlash(w, r, "error"),
	}

	_ = ren.HTML(w, http.StatusOK, "profile", data)
}

func (server *Server) ProfileUpdate(w http.ResponseWriter, r *http.Request) {
	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	user.FirstName = r.FormValue("first_name")
	user.LastName = r.FormValue("last_name")

	if err := server.DB.Save(user).Error; err != nil {
		http.Error(w, "gagal update profil", http.StatusInternalServerError)
		return
	}

	// Set flash sukses pakai helper global
	SetFlash(w, r, "success", "Profil berhasil diperbarui!")

	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

func (server *Server) ProfilePasswordForm(w http.ResponseWriter, r *http.Request) {
	ren := userRender()

	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	data := map[string]interface{}{
		"user":      user,
		"isAdmin":   IsAdminUser(user),
		"cartCount": server.GetCartCount(w, r),
		"error":     GetFlash(w, r, "error"),   // pesan error (kalau ada)
		"flashes":   GetFlash(w, r, "success"), // pesan sukses (kalau ada)
	}

	_ = ren.HTML(w, http.StatusOK, "profile_password", data)
}

func (server *Server) ProfilePasswordUpdate(w http.ResponseWriter, r *http.Request) {
	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("new_password_confirmation")

	// validasi basic
	if currentPassword == "" || newPassword == "" || confirmPassword == "" {
		SetFlash(w, r, "error", "Semua field wajib diisi.")
		http.Redirect(w, r, "/profile#password", http.StatusSeeOther)
		return
	}

	if newPassword != confirmPassword {
		SetFlash(w, r, "error", "Konfirmasi password baru tidak sama.")
		http.Redirect(w, r, "/profile#password", http.StatusSeeOther)
		return
	}

	// cek password lama
	if !ComparePassword(currentPassword, user.Password) {
		SetFlash(w, r, "error", "Password lama salah.")
		http.Redirect(w, r, "/profile#password", http.StatusSeeOther)
		return
	}

	// hash password baru
	hashed, err := MakePassword(newPassword)
	if err != nil {
		SetFlash(w, r, "error", "Gagal memproses password baru.")
		http.Redirect(w, r, "/profile#password", http.StatusSeeOther)
		return
	}

	user.Password = hashed
	if err := server.DB.Save(user).Error; err != nil {
		SetFlash(w, r, "error", "Gagal menyimpan password baru.")
		http.Redirect(w, r, "/profile#password", http.StatusSeeOther)
		return
	}

	SetFlash(w, r, "success", "Password berhasil diubah.")
	http.Redirect(w, r, "/profile#password", http.StatusSeeOther)
}
