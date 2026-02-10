package controllers

import (
	"net/http"
	"strconv"

	"github.com/alirogz/goshop/app/models"
	"github.com/gorilla/mux"
)

func (server *Server) Products(w http.ResponseWriter, r *http.Request) {
	// Pakai renderer untuk user yang sudah punya FuncMap (formatRupiah, dll)
	ren := userRender()

	// --- PAGINATION ---
	q := r.URL.Query()
	page, err := strconv.Atoi(q.Get("page"))
	if err != nil || page <= 0 {
		page = 1
	}

	perPage := 9

	productModel := models.Product{}
	products, totalRows, err := productModel.GetProducts(server.DB, perPage, page)
	if err != nil {
		http.Error(w, "Gagal mengambil data produk", http.StatusInternalServerError)
		return
	}

	pagination, _ := GetPaginationLinks(server.AppConfig, PaginationParams{
		Path:        "products",
		TotalRows:   int32(totalRows),
		PerPage:     int32(perPage),
		CurrentPage: int32(page),
	})

	user := server.CurrentUser(w, r)

	data := map[string]interface{}{
		"products":   products,
		"pagination": pagination,
		"user":       user,
		"isAdmin":    IsAdminUser(user),
		"cartCount":  server.GetCartCount(w, r),
	}
	server.InjectNavbarBadges(data, user)
	_ = ren.HTML(w, http.StatusOK, "products", data)

}

func (server *Server) GetProductBySlug(w http.ResponseWriter, r *http.Request) {
	ren := userRender() // ← PAKAI INI

	vars := mux.Vars(r)
	slugStr := vars["slug"]

	productModel := models.Product{}
	product, err := productModel.FindBySlug(server.DB, slugStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	user := server.CurrentUser(w, r)

	data := map[string]interface{}{
		"product":   product,
		"user":      user,
		"isAdmin":   IsAdminUser(user),
		"cartCount": server.GetCartCount(w, r),
	}
	server.InjectNavbarBadges(data, user)
	_ = ren.HTML(w, http.StatusOK, "product", data)

}
