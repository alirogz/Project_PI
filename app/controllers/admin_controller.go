package controllers

import (
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alirogz/goshop/app/models"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gosimple/slug"
	"github.com/shopspring/decimal"
	"github.com/unrolled/render"
)

// GET /admin/orders
func (server *Server) AdminOrdersIndex(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	admin := server.CurrentUser(w, r)
	if !IsAdminUser(admin) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Ambil semua order (sederhana, urut terbaru)
	var orders []models.Order
	if err := server.DB.Order("created_at desc").Preload("OrderCustomer").Find(&orders).Error; err != nil {
		SetFlash(w, r, "error", "Gagal mengambil data order")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	ren := adminRender()
	_ = ren.HTML(w, http.StatusOK, "admin_orders", map[string]interface{}{
		"orders":    orders,
		"user":      admin,
		"isAdmin":   IsAdminUser(admin),
		"cartCount": server.GetCartCount(w, r),
		"success":   GetFlash(w, r, "success"),
		"error":     GetFlash(w, r, "error"),
	})
}

// GET /admin/orders/{id}
func (server *Server) AdminOrdersShow(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	admin := server.CurrentUser(w, r)
	if !IsAdminUser(admin) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	var order models.Order
	if err := server.DB.
		Preload("OrderCustomer").
		Preload("OrderItems").
		Preload("OrderItems.Product").
		Preload("OrderItems.Product.ProductImages").
		Where("id = ?", id).
		First(&order).Error; err != nil {

		SetFlash(w, r, "error", "Order tidak ditemukan")
		http.Redirect(w, r, "/admin/orders", http.StatusSeeOther)
		return
	}

	// isi helper (Name, ProductImageURL, dll)
	server.hydrateOrderDetail(&order)

	// 🔹 hitung total item & total berat (gram) dalam float64
	var totalItems int
	var totalWeight float64

	for _, it := range order.OrderItems {
		totalItems += int(it.Qty)

		// Product.Weight bertipe decimal.Decimal → convert ke float64
		if !it.Product.Weight.IsZero() {
			if w, ok := it.Product.Weight.Float64(); ok {
				totalWeight += w * float64(it.Qty)
			}
		}
	}

	// Kg untuk tampilan
	totalWeightKg := totalWeight / 1000.0

	ren := adminRender()
	_ = ren.HTML(w, http.StatusOK, "admin_order_show", map[string]interface{}{
		"order":         order,
		"user":          admin,
		"isAdmin":       IsAdminUser(admin),
		"cartCount":     server.GetCartCount(w, r),
		"totalItems":    totalItems,
		"totalWeight":   totalWeight,
		"totalWeightKg": totalWeightKg,
		"success":       GetFlash(w, r, "success"),
		"error":         GetFlash(w, r, "error"),
	})
}

// POST /admin/orders/{id}/pay-manual
func (server *Server) AdminPayManual(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	admin := server.CurrentUser(w, r)
	if !IsAdminUser(admin) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	id := mux.Vars(r)["id"]

	orderModel := models.Order{}
	order, err := orderModel.FindByID(server.DB, id)
	if err != nil {
		SetFlash(w, r, "error", "Order tidak ditemukan")
		http.Redirect(w, r, "/admin/orders", http.StatusSeeOther)
		return
	}
	if order.IsPaid() {
		SetFlash(w, r, "success", "Order sudah dibayar sebelumnya")
		http.Redirect(w, r, "/admin/orders/"+order.ID, http.StatusSeeOther)
		return
	}

	// simpan payment manual
	paymentModel := models.Payment{}
	raw := json.RawMessage(`{"note":"manual payment by admin"}`)
	_, err = paymentModel.CreatePayment(server.DB, &models.Payment{
		OrderID:           order.ID,
		Amount:            order.GrandTotal,
		TransactionID:     "ADMIN-MANUAL-" + time.Now().Format("20060102150405"),
		TransactionStatus: "settlement",
		Payload:           &raw,
		PaymentType:       "manual",
	})
	if err != nil {
		SetFlash(w, r, "error", "Gagal membuat payment manual")
		http.Redirect(w, r, "/admin/orders/"+order.ID, http.StatusSeeOther)
		return
	}

	if err := order.MarkAsPaid(server.DB); err != nil {
		SetFlash(w, r, "error", "Gagal menandai lunas")
		http.Redirect(w, r, "/admin/orders/"+order.ID, http.StatusSeeOther)
		return
	}

	SetFlash(w, r, "success", "Order ditandai lunas")
	http.Redirect(w, r, "/admin/orders/"+order.ID, http.StatusSeeOther)
}

func (server *Server) AdminApprovePayment(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) || !IsAdminUser(server.CurrentUser(w, r)) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	var order models.Order
	if err := server.DB.Where("id = ?", id).First(&order).Error; err != nil {
		SetFlash(w, r, "error", "Order tidak ditemukan")
		http.Redirect(w, r, "/admin/orders", http.StatusSeeOther)
		return
	}

	order.PaymentStatus = "PAID"
	if err := server.DB.Save(&order).Error; err != nil {
		SetFlash(w, r, "error", "Gagal mengupdate status pembayaran")
	} else {
		SetFlash(w, r, "success", "Pembayaran berhasil dikonfirmasi.")
	}

	http.Redirect(w, r, "/admin/orders/"+id, http.StatusSeeOther)
}

func (server *Server) AdminRejectPayment(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) || !IsAdminUser(server.CurrentUser(w, r)) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	var order models.Order
	if err := server.DB.Where("id = ?", id).First(&order).Error; err != nil {
		SetFlash(w, r, "error", "Order tidak ditemukan")
		http.Redirect(w, r, "/admin/orders", http.StatusSeeOther)
		return
	}

	order.PaymentStatus = "REJECTED"
	if err := server.DB.Save(&order).Error; err != nil {
		SetFlash(w, r, "error", "Gagal mengupdate status pembayaran")
	} else {
		SetFlash(w, r, "success", "Pembayaran ditolak.")
	}

	http.Redirect(w, r, "/admin/orders/"+id, http.StatusSeeOther)
}

// POST /admin/orders/{id}/status  (values: shipped|completed|cancel)
func (server *Server) AdminUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		SetFlash(w, r, "error", "Silakan login terlebih dahulu.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user := server.CurrentUser(w, r)
	if !IsAdminUser(user) {
		SetFlash(w, r, "error", "Anda tidak memiliki akses.")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]

	if err := r.ParseForm(); err != nil {
		SetFlash(w, r, "error", "Gagal membaca status.")
		http.Redirect(w, r, "/admin/orders/"+id, http.StatusSeeOther)
		return
	}

	statusStr := strings.ToLower(r.FormValue("status"))

	var newStatus int
	switch statusStr {
	case "pending":
		newStatus = 0
	case "processing":
		newStatus = 1
	case "shipped":
		newStatus = 2
	case "completed":
		newStatus = 3
	default:
		SetFlash(w, r, "error", "Status tidak valid.")
		http.Redirect(w, r, "/admin/orders/"+id, http.StatusSeeOther)
		return
	}

	// Pastikan order ada
	var order models.Order
	if err := server.DB.Where("id = ?", id).First(&order).Error; err != nil {
		log.Println("AdminUpdateStatus: gagal menemukan order:", err)
		SetFlash(w, r, "error", "Pesanan tidak ditemukan.")
		http.Redirect(w, r, "/admin/orders", http.StatusSeeOther)
		return
	}

	// Update hanya kolom status (lebih aman daripada Save seluruh struct)
	if err := server.DB.Model(&order).Update("status", newStatus).Error; err != nil {
		log.Println("AdminUpdateStatus: gagal update status:", err)
		SetFlash(w, r, "error", "Gagal menyimpan status.")
		http.Redirect(w, r, "/admin/orders/"+id, http.StatusSeeOther)
		return
	}

	SetFlash(w, r, "success", "Status pesanan berhasil diperbarui.")
	http.Redirect(w, r, "/admin/orders/"+id, http.StatusSeeOther)
}

// ========== ADMIN PRODUCTS ==========

// GET /admin/products
func (server *Server) AdminProductsIndex(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	admin := server.CurrentUser(w, r)
	if !IsAdminUser(admin) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	var products []models.Product
	if err := server.DB.Order("created_at desc").Find(&products).Error; err != nil {
		SetFlash(w, r, "error", "Gagal mengambil data produk: "+err.Error())
	}

	ren := adminRender()
	_ = ren.HTML(w, http.StatusOK, "admin_products", map[string]interface{}{
		"products":  products,
		"user":      admin,
		"cartCount": server.GetCartCount(w, r),
		"isAdmin":   IsAdminUser(admin),
		"success":   GetFlash(w, r, "success"),
		"error":     GetFlash(w, r, "error"),
	})
}

// GET /admin/products/new
// GET /admin/products/new
func (server *Server) AdminProductsNew(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	admin := server.CurrentUser(w, r)
	if !IsAdminUser(admin) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	ren := adminRender()
	_ = ren.HTML(w, http.StatusOK, "admin_product_form", map[string]interface{}{
		"product":   models.Product{}, // kosong
		"user":      admin,
		"cartCount": server.GetCartCount(w, r),
		"isAdmin":   IsAdminUser(admin),
		"isEdit":    false, // beda dengan edit
	})
}

// POST /admin/products
// POST /admin/products
func (server *Server) AdminProductsCreate(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	admin := server.CurrentUser(w, r)
	if !IsAdminUser(admin) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// ambil data form teks
	name := r.FormValue("name")
	priceStr := r.FormValue("price")
	stockStr := r.FormValue("stock")
	shortDesc := r.FormValue("short_description")
	desc := r.FormValue("description")

	if name == "" || priceStr == "" || stockStr == "" {
		SetFlash(w, r, "error", "Nama, harga, dan stok wajib diisi")
		http.Redirect(w, r, "/admin/products/new", http.StatusSeeOther)
		return
	}

	price, err := decimal.NewFromString(priceStr)
	if err != nil {
		SetFlash(w, r, "error", "Format harga tidak valid")
		http.Redirect(w, r, "/admin/products/new", http.StatusSeeOther)
		return
	}

	stock, err := strconv.Atoi(stockStr)
	if err != nil {
		SetFlash(w, r, "error", "Format stok tidak valid")
		http.Redirect(w, r, "/admin/products/new", http.StatusSeeOther)
		return
	}

	// -------- UPLOAD GAMBAR --------
	var imageFilename string

	file, header, err := r.FormFile("image")
	if err == nil {
		defer file.Close()

		// pastikan folder upload ada
		if err := os.MkdirAll("public/uploads", 0755); err != nil {
			log.Println("mkdir uploads error:", err)
		} else {
			ext := filepath.Ext(header.Filename)
			imageFilename = uuid.New().String() + ext

			dstPath := filepath.Join("public/uploads", imageFilename)
			dst, err := os.Create(dstPath)
			if err != nil {
				log.Println("create file error:", err)
			} else {
				defer dst.Close()
				if _, err := io.Copy(dst, file); err != nil {
					log.Println("copy file error:", err)
				}
			}
		}
	} else if err != http.ErrMissingFile {
		// kalau error selain "tidak ada file" akan kita log, tapi tidak blokir
		log.Println("FormFile image error:", err)
	}
	// -------- /UPLOAD GAMBAR --------

	now := time.Now()

	product := models.Product{
		ID:               uuid.New().String(),
		UserID:           admin.ID,
		Sku:              slug.Make(name),
		Name:             name,
		Slug:             slug.Make(name),
		Price:            price,
		Stock:            stock,
		ShortDescription: shortDesc,
		Description:      desc,
		Status:           1,
		Image:            imageFilename, // ← ini sekarang PASTI terdefinisi
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := server.DB.Create(&product).Error; err != nil {
		SetFlash(w, r, "error", "Gagal menyimpan produk: "+err.Error())
		http.Redirect(w, r, "/admin/products/new", http.StatusSeeOther)
		return
	}

	SetFlash(w, r, "success", "Produk berhasil dibuat")
	http.Redirect(w, r, "/admin/products", http.StatusSeeOther)
}

// GET /admin/products/{id}/edit
func (server *Server) AdminProductsEdit(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	admin := server.CurrentUser(w, r)
	if !IsAdminUser(admin) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	id := mux.Vars(r)["id"]

	productModel := models.Product{}
	product, err := productModel.FindByID(server.DB, id)
	if err != nil {
		SetFlash(w, r, "error", "Produk tidak ditemukan")
		http.Redirect(w, r, "/admin/products", http.StatusSeeOther)
		return
	}

	ren := adminRender()
	_ = ren.HTML(w, http.StatusOK, "admin_product_form", map[string]interface{}{
		"product":   product,
		"user":      admin,
		"cartCount": server.GetCartCount(w, r),
		"isAdmin":   IsAdminUser(admin),
		"isEdit":    true,
	})
}

// POST /admin/products/{id}
func (server *Server) AdminProductsUpdate(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	admin := server.CurrentUser(w, r)
	if !IsAdminUser(admin) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	id := mux.Vars(r)["id"]

	productModel := models.Product{}
	product, err := productModel.FindByID(server.DB, id)
	if err != nil {
		SetFlash(w, r, "error", "Produk tidak ditemukan")
		http.Redirect(w, r, "/admin/products", http.StatusSeeOther)
		return
	}

	name := r.FormValue("name")
	priceStr := r.FormValue("price")
	stockStr := r.FormValue("stock")
	shortDesc := r.FormValue("short_description")
	desc := r.FormValue("description")

	price, err := decimal.NewFromString(priceStr)
	if err != nil {
		SetFlash(w, r, "error", "Format harga tidak valid")
		http.Redirect(w, r, "/admin/products/"+id+"/edit", http.StatusSeeOther)
		return
	}

	stock, err := strconv.Atoi(stockStr)
	if err != nil {
		SetFlash(w, r, "error", "Format stok tidak valid")
		http.Redirect(w, r, "/admin/products/"+id+"/edit", http.StatusSeeOther)
		return
	}

	product.Name = name
	product.Slug = slug.Make(name)
	product.Sku = slug.Make(name)
	product.Price = price
	product.Stock = stock
	product.ShortDescription = shortDesc
	product.Description = desc
	product.UpdatedAt = time.Now()

	if err := server.DB.Save(product).Error; err != nil {
		SetFlash(w, r, "error", "Gagal mengubah produk: "+err.Error())
		http.Redirect(w, r, "/admin/products/"+id+"/edit", http.StatusSeeOther)
		return
	}

	// --- UPLOAD GAMBAR (OPSIONAL) ---
	file, header, err := r.FormFile("image")
	if err == nil {
		defer file.Close()

		if err := os.MkdirAll("public/uploads", 0755); err != nil {
			log.Println("mkdir uploads error:", err)
		} else {
			ext := filepath.Ext(header.Filename)
			newFilename := uuid.New().String() + ext

			dstPath := filepath.Join("public/uploads", newFilename)
			dst, err := os.Create(dstPath)
			if err != nil {
				log.Println("create file error:", err)
			} else {
				defer dst.Close()
				if _, err := io.Copy(dst, file); err != nil {
					log.Println("copy file error:", err)
				} else {
					// kalau berhasil, update kolom image
					product.Image = newFilename
				}
			}
		}
	} else if err != http.ErrMissingFile {
		log.Println("FormFile image error:", err)
	}

	SetFlash(w, r, "success", "Produk berhasil diubah")
	http.Redirect(w, r, "/admin/products", http.StatusSeeOther)
}

// POST /admin/products/{id}/delete
func (server *Server) AdminProductsDelete(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	admin := server.CurrentUser(w, r)
	if !IsAdminUser(admin) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	id := mux.Vars(r)["id"]

	if err := server.DB.Where("id = ?", id).Delete(&models.Product{}).Error; err != nil {
		SetFlash(w, r, "error", "Gagal menghapus produk: "+err.Error())
	} else {
		SetFlash(w, r, "success", "Produk berhasil dihapus")
	}

	http.Redirect(w, r, "/admin/products", http.StatusSeeOther)
}

// Helper kecil kalau kamu ingin menghitung ulang grand total di admin, contoh bila mau koreksi ongkir
func calcGrand(base decimal.Decimal, shipping decimal.Decimal) decimal.Decimal {
	return base.Add(shipping)
}

func adminRender() *render.Render {
	funcMap := template.FuncMap{
		"formatRupiah": formatRupiah,

		// fungsi-fungsi helper yang juga dipakai di templates (orders, order_detail, dll)
		"lower": strings.ToLower,
		"add": func(a, b int) int {
			return a + b
		},
		"sub": func(a, b int) int {
			return a - b
		},
		"seq": func(from, to int) []int {
			if to < from {
				return []int{}
			}
			s := make([]int, 0, to-from+1)
			for i := from; i <= to; i++ {
				s = append(s, i)
			}
			return s
		},
		"urlValuesWithPage": func(v url.Values, page int) string {
			// clone manual, karena url.Values.Clone belum ada di Go versi lama
			q := url.Values{}
			for key, vals := range v {
				for _, val := range vals {
					q.Add(key, val)
				}
			}
			q.Set("page", strconv.Itoa(page))
			return q.Encode()
		},
	}

	return render.New(render.Options{
		Directory:  "templates",
		Layout:     "layout", // pakai layout utama yang sudah ada
		Extensions: []string{".html", ".tmpl"},
		Funcs:      []template.FuncMap{funcMap},
	})
}
