package controllers

import (
	"net/http"

	"github.com/alirogz/goshop/app/models"
)

func (server *Server) Home(w http.ResponseWriter, r *http.Request) {
	ren := userRender()

	user := server.CurrentUser(w, r)

	// Ambil produk + preload gambar untuk home (trending items)
	var products []models.Product
	if err := server.DB.
		Preload("ProductImages"). // <-- ini yang bikin gambar dari CRUD ikut ke-load
		Order("created_at desc"). // urutkan dari yang terbaru
		Limit(8).                 // tampilkan 8 produk
		Find(&products).Error; err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"user":      user,
		"isAdmin":   IsAdminUser(user),
		"products":  products,
		"cartCount": server.GetCartCount(w, r),
	}

	server.InjectNavbarBadges(data, user)
	_ = ren.HTML(w, http.StatusOK, "home", data)

}
