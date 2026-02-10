package controllers

import (
	"database/sql"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alirogz/goshop/app/consts"
	"github.com/alirogz/goshop/app/models"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type CheckoutRequest struct {
	Cart            *models.Cart
	ShippingFee     *ShippingFee
	ShippingAddress *ShippingAddress
}

type ShippingFee struct {
	Courier     string
	PackageName string
	Fee         float64
}

type ShippingAddress struct {
	FirstName  string
	LastName   string
	CityID     string
	ProvinceID string
	Address1   string
	Address2   string
	Phone      string
	Email      string
	PostCode   string
}

func (server *Server) Checkout(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		SetFlash(w, r, "error", "Anda perlu login!")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user := server.CurrentUser(w, r)

	shippingCost, err := server.getSelectedShippingCost(w, r)
	if err != nil {
		SetFlash(w, r, "error", "Proses checkout gagal: "+err.Error())
		http.Redirect(w, r, "/carts", http.StatusSeeOther)
		return
	}

	cartID := GetShoppingCartID(w, r)
	cart, _ := GetShoppingCart(server.DB, cartID)

	// -------------------------
	// 1. Cek apakah user memilih alamat tersimpan
	// -------------------------
	addressID := r.FormValue("address_id")
	var shippingAddress *ShippingAddress

	if addressID != "" {
		var addr models.Address
		if err := server.DB.
			Where("id = ? AND user_id = ?", addressID, user.ID).
			First(&addr).Error; err == nil {

			// mapping Address -> ShippingAddress
			shippingAddress = &ShippingAddress{
				FirstName:  addr.Name,
				LastName:   "",
				CityID:     addr.CityID,
				ProvinceID: addr.ProvinceID,
				Address1:   addr.Address1,
				Address2:   addr.Address2,
				Phone:      addr.Phone,
				Email:      user.Email,
				PostCode:   addr.PostCode,
			}
		}
	}

	// -------------------------
	// 2. Kalau belum punya shippingAddress (tidak pilih alamat / alamat invalid)
	//    gunakan data dari form manual
	// -------------------------
	if shippingAddress == nil {
		shippingAddress = &ShippingAddress{
			FirstName:  r.FormValue("first_name"),
			LastName:   r.FormValue("last_name"),
			CityID:     r.FormValue("city_id"),
			ProvinceID: r.FormValue("province_id"),
			Address1:   r.FormValue("address1"),
			Address2:   r.FormValue("address2"),
			Phone:      r.FormValue("phone"),
			Email:      r.FormValue("email"),
			PostCode:   r.FormValue("post_code"),
		}

		// opsional: simpan alamat baru ke tabel addresses kalau user minta
		if r.FormValue("save_address") == "on" && shippingAddress.FirstName != "" {
			addr := models.Address{
				ID:         uuid.New().String(),
				UserID:     user.ID,
				Name:       shippingAddress.FirstName,
				CityID:     shippingAddress.CityID,
				ProvinceID: shippingAddress.ProvinceID,
				Address1:   shippingAddress.Address1,
				Address2:   shippingAddress.Address2,
				Phone:      shippingAddress.Phone,
				Email:      shippingAddress.Email,
				PostCode:   shippingAddress.PostCode,
			}

			// kalau belum punya alamat sama sekali, jadikan primary
			var count int64
			server.DB.Model(&models.Address{}).
				Where("user_id = ?", user.ID).
				Count(&count)
			if count == 0 {
				addr.IsPrimary = true
			}

			// error saat simpan alamat baru tidak meng-cancel checkout
			_ = server.DB.Create(&addr).Error
		}
	}

	checkoutRequest := &CheckoutRequest{
		Cart: cart,
		ShippingFee: &ShippingFee{
			Courier:     r.FormValue("courier"),
			PackageName: r.FormValue("shipping_service"),
			Fee:         shippingCost,
		},
		ShippingAddress: shippingAddress,
	}

	order, err := server.SaveOrder(user, checkoutRequest)
	if err != nil {
		// tampilkan pesan error asli agar ketahuan akar masalahnya
		SetFlash(w, r, "error", "Proses checkout gagal: "+err.Error())
		http.Redirect(w, r, "/carts", http.StatusSeeOther)
		return
	}

	cartModel := models.Cart{}
	_ = cartModel.ClearCart(server.DB, cartID)

	SetFlash(w, r, "success", "Data order berhasil disimpan")
	http.Redirect(w, r, "/orders/"+order.ID, http.StatusSeeOther)
}

// app/controllers/order_controller.go

func (server *Server) ShowOrder(w http.ResponseWriter, r *http.Request) {
	ren := userRender()
	vars := mux.Vars(r)
	id := vars["id"]

	if !IsLoggedIn(r) {
		SetFlash(w, r, "error", "Silakan login untuk melihat detail pesanan.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user := server.CurrentUser(w, r)

	var order models.Order
	err := server.DB.
		Preload("OrderCustomer").
		Preload("OrderItems").
		Preload("OrderItems.Product").
		Preload("OrderItems.Product.ProductImages").
		Where("id = ? AND user_id = ?", id, user.ID).
		First(&order).Error
	if err != nil {
		SetFlash(w, r, "error", "Pesanan tidak ditemukan.")
		http.Redirect(w, r, "/orders", http.StatusSeeOther)
		return
	}

	// cukup panggil sekali, tidak perlu loop manual
	server.hydrateOrderDetail(&order)

	data := map[string]interface{}{
		"user":      user,
		"isAdmin":   IsAdminUser(user),
		"order":     order,
		"cartCount": server.GetCartCount(w, r),
		"success":   GetFlash(w, r, "success"),
		"error":     GetFlash(w, r, "error"),
	}
	server.InjectNavbarBadges(data, user)
	_ = ren.HTML(w, http.StatusOK, "order_detail", data)
}

// mengisi field bantu untuk tampilan template (user & admin)
func (server *Server) hydrateOrderDetail(order *models.Order) {
	if order == nil {
		return
	}

	for i := range order.OrderItems {
		item := &order.OrderItems[i]

		// Nama produk: kalau kosong, ambil dari relasi Product
		if item.Name == "" && item.Product.ID != "" {
			item.Name = item.Product.Name
		}

		// Gambar produk:
		// 1) kalau kamu pakai tabel product_images → pakai itu
		if len(item.Product.ProductImages) > 0 {
			first := item.Product.ProductImages[0]
			item.ProductImageURL = "/uploads/products/" + first.Path

			// 2) kalau tidak ada, pakai kolom tunggal Product.Image (hasil upload ke public/uploads)
		} else if item.Product.Image != "" {
			item.ProductImageURL = "/public/uploads/" + item.Product.Image

			// 3) fallback terakhir → no-image
		} else {
			item.ProductImageURL = "/assets/img/no-image.png"
		}
	}
}

func (server *Server) getSelectedShippingCost(w http.ResponseWriter, r *http.Request) (float64, error) {
	_ = r.ParseForm()
	feeStr := r.FormValue("shipping_fee") // harus numeric (contoh: 14000)
	if feeStr == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(feeStr, 64)
	if err != nil {
		return 0, nil
	}
	if f < 0 {
		f = 0
	}
	return f, nil
}

func (server *Server) SaveOrder(user *models.User, r *CheckoutRequest) (*models.Order, error) {
	var orderItems []models.OrderItem
	orderID := uuid.New().String()

	// Pembayaran manual → tidak pakai payment URL
	paymentURL := ""

	// isi orderItems dari isi cart
	if len(r.Cart.CartItems) > 0 {
		for _, cartItem := range r.Cart.CartItems {
			orderItems = append(orderItems, models.OrderItem{
				ProductID:       cartItem.ProductID,
				Qty:             cartItem.Qty,
				BasePrice:       cartItem.BasePrice,
				BaseTotal:       cartItem.BaseTotal,
				TaxAmount:       cartItem.TaxAmount,
				TaxPercent:      cartItem.TaxPercent,
				DiscountAmount:  cartItem.DiscountAmount,
				DiscountPercent: cartItem.DiscountPercent,
				SubTotal:        cartItem.SubTotal,
				Sku:             cartItem.Product.Sku,
				Name:            cartItem.Product.Name,
				Weight:          cartItem.Product.Weight,
				Size:            cartItem.Size,
				OrderID:         orderID,
			})
		}
	}

	// data penerima
	orderCustomer := &models.OrderCustomer{
		UserID:     user.ID,
		OrderID:    orderID,
		FirstName:  r.ShippingAddress.FirstName,
		LastName:   r.ShippingAddress.LastName,
		CityID:     r.ShippingAddress.CityID,
		ProvinceID: r.ShippingAddress.ProvinceID,
		Address1:   r.ShippingAddress.Address1,
		Address2:   r.ShippingAddress.Address2,
		Phone:      r.ShippingAddress.Phone,
		Email:      r.ShippingAddress.Email,
		PostCode:   r.ShippingAddress.PostCode,
	}

	// hitung ongkir & grand total
	shipDec := decimal.NewFromFloat(r.ShippingFee.Fee)
	grandTotal := r.Cart.GrandTotal.Add(shipDec)

	// generate kode unik 3 digit
	rand.Seed(time.Now().UnixNano())
	uniqueCode := rand.Intn(900) + 100 // 100–999

	// siapkan data order
	orderData := &models.Order{
		ID:                  orderID,
		UserID:              user.ID,
		OrderItems:          orderItems,
		OrderCustomer:       orderCustomer,
		Status:              0,
		OrderDate:           time.Now(),
		PaymentDue:          time.Now().AddDate(0, 0, 7),
		PaymentStatus:       consts.OrderPaymentStatusUnpaid,
		PaymentMethod:       "Transfer Bank",
		BaseTotalPrice:      r.Cart.BaseTotalPrice,
		TaxAmount:           r.Cart.TaxAmount,
		TaxPercent:          r.Cart.TaxPercent,
		DiscountAmount:      r.Cart.DiscountAmount,
		DiscountPercent:     r.Cart.DiscountPercent,
		ShippingCost:        shipDec,
		GrandTotal:          grandTotal, // subtotal + tax + ongkir
		ShippingCourier:     r.ShippingFee.Courier,
		ShippingServiceName: r.ShippingFee.PackageName,
		PaymentToken:        sql.NullString{String: paymentURL, Valid: paymentURL != ""},
		// fitur 1: kode unik & total transfer
		PaymentUniqueCode: uniqueCode,
		PaymentTotal:      grandTotal.Add(decimal.NewFromInt(int64(uniqueCode))),
	}

	orderModel := models.Order{}
	order, err := orderModel.CreateOrder(server.DB, orderData)
	if err != nil {
		return nil, err
	}

	return order, nil
}

// helper: isi ProductImageURL utk setiap item
func attachProductImagesToOrder(db *gorm.DB, order *models.Order) {
	if order == nil {
		return
	}

	for i := range order.OrderItems {
		item := &order.OrderItems[i]

		// preload ProductImages untuk product ini
		var images []models.ProductImage
		if err := db.
			Where("product_id = ?", item.ProductID).
			Order("created_at ASC").
			Find(&images).Error; err != nil {
			continue
		}

		if len(images) > 0 {
			item.ProductImageURL = "/uploads/products/" + images[0].Path
		}
	}
}

// PayManual: simulasi pembayaran manual (tanpa Midtrans)
// Hanya boleh dilakukan oleh user pemilik order (atau kamu bisa perluas nanti untuk admin).
func (server *Server) PayManual(w http.ResponseWriter, r *http.Request) {
	ren := userRender()
	vars := mux.Vars(r)
	id := vars["id"]

	if !IsLoggedIn(r) {
		SetFlash(w, r, "error", "Silakan login terlebih dahulu.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user := server.CurrentUser(w, r)

	var order models.Order
	if err := server.DB.Where("id = ? AND user_id = ?", id, user.ID).
		First(&order).Error; err != nil {
		SetFlash(w, r, "error", "Pesanan tidak ditemukan.")
		http.Redirect(w, r, "/orders", http.StatusSeeOther)
		return
	}

	// handle upload
	err := r.ParseMultipartForm(10 << 20) // 10MB
	if err != nil {
		SetFlash(w, r, "error", "Gagal membaca form upload.")
		http.Redirect(w, r, "/orders/"+id+"/pay-manual", http.StatusSeeOther)
		return
	}

	file, handler, err := r.FormFile("payment_proof")
	if err != nil {
		SetFlash(w, r, "error", "Silakan pilih file bukti transfer.")
		http.Redirect(w, r, "/orders/"+id+"/pay-manual", http.StatusSeeOther)
		return
	}
	defer file.Close()

	// buat folder kalau belum ada
	uploadDir := "./uploads/payments"
	_ = os.MkdirAll(uploadDir, os.ModePerm)

	ext := filepath.Ext(handler.Filename)
	filename := fmt.Sprintf("order-%s-%d%s", order.ID, time.Now().Unix(), ext)
	filepathFull := filepath.Join(uploadDir, filename)

	dst, err := os.Create(filepathFull)
	if err != nil {
		SetFlash(w, r, "error", "Gagal menyimpan file di server.")
		http.Redirect(w, r, "/orders/"+id+"/pay-manual", http.StatusSeeOther)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		SetFlash(w, r, "error", "Gagal menyalin file.")
		http.Redirect(w, r, "/orders/"+id+"/pay-manual", http.StatusSeeOther)
		return
	}

	// update order
	order.PaymentProof = filename
	order.PaymentStatus = "waiting_review"
	if err := server.DB.Save(&order).Error; err != nil {
		SetFlash(w, r, "error", "Gagal menyimpan data pembayaran.")
		http.Redirect(w, r, "/orders/"+id+"/pay-manual", http.StatusSeeOther)
		return
	}

	SetFlash(w, r, "success", "Bukti transfer berhasil diupload. Menunggu konfirmasi admin.")
	http.Redirect(w, r, "/orders/"+id, http.StatusSeeOther)
	_ = ren
}

func savePaymentProofFile(r *http.Request, fieldName string) (string, error) {
	file, header, err := r.FormFile(fieldName)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// buat folder kalau belum ada
	uploadDir := "public/payment_proofs"
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return "", err
	}

	// nama file simpel: timestamp + original name
	filename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), header.Filename)
	filepath := filepath.Join(uploadDir, filename)

	out, err := os.Create(filepath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		return "", err
	}

	return filename, nil
}

func (server *Server) UploadPaymentProof(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		SetFlash(w, r, "error", "Silakan login terlebih dahulu.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user := server.CurrentUser(w, r)
	vars := mux.Vars(r)
	id := vars["id"]

	// ambil order milik user ini
	var order models.Order
	if err := server.DB.
		Where("id = ? AND user_id = ?", id, user.ID).
		First(&order).Error; err != nil {

		SetFlash(w, r, "error", "Pesanan tidak ditemukan.")
		http.Redirect(w, r, "/orders", http.StatusSeeOther)
		return
	}

	// ambil file dari form dengan name="payment_proof"
	filename, err := savePaymentProofFile(r, "payment_proof")
	if err != nil {
		SetFlash(w, r, "error", "Gagal mengunggah bukti pembayaran.")
		http.Redirect(w, r, "/orders/"+id, http.StatusSeeOther)
		return
	}

	// update order
	order.PaymentProof = filename
	order.PaymentStatus = "WAITING_REVIEW" // sesuaikan enum kamu
	if err := server.DB.Save(&order).Error; err != nil {
		SetFlash(w, r, "error", "Gagal menyimpan bukti pembayaran.")
		http.Redirect(w, r, "/orders/"+id, http.StatusSeeOther)
		return
	}

	SetFlash(w, r, "success", "Bukti pembayaran berhasil diunggah. Admin akan memeriksa pembayaran Anda.")
	http.Redirect(w, r, "/orders/"+id, http.StatusSeeOther)
}

// =========================
// Handler: daftar pesanan user (GET /orders)
// =========================

func (server *Server) OrdersIndex(w http.ResponseWriter, r *http.Request) {
	ren := userRender()

	if !IsLoggedIn(r) {
		SetFlash(w, r, "error", "Anda perlu login dulu untuk melihat pesanan.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user := server.CurrentUser(w, r)

	// --- filter ---
	q := server.DB.Model(&models.Order{}).
		Preload("OrderCustomer").
		Preload("OrderItems").
		Preload("OrderItems.Product").
		Where("user_id = ?", user.ID)

	statusFilter := r.URL.Query().Get("status")
	if statusFilter != "" && statusFilter != "all" {
		switch strings.ToLower(statusFilter) {
		case "pending":
			q = q.Where("status = ?", 0)
		case "processing":
			q = q.Where("status = ?", 1)
		case "shipped":
			q = q.Where("status = ?", 2)
		case "completed":
			q = q.Where("status = ?", 3)
		}
	}

	paymentFilter := r.URL.Query().Get("payment")
	if paymentFilter != "" && paymentFilter != "all" {
		switch paymentFilter {
		case "paid":
			q = q.Where("LOWER(payment_status) = 'paid'")
		case "unpaid":
			q = q.Where("LOWER(payment_status) = 'unpaid'")
		case "waiting_review":
			q = q.Where("LOWER(payment_status) = 'waiting_review'")
		}
	}

	dateFrom := r.URL.Query().Get("date_from")
	dateTo := r.URL.Query().Get("date_to")
	if dateFrom != "" {
		if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
			q = q.Where("created_at >= ?", t)
		}
	}
	if dateTo != "" {
		if t, err := time.Parse("2006-01-02", dateTo); err == nil {
			// tambah 1 hari biar inclusive
			q = q.Where("created_at < ?", t.Add(24*time.Hour))
		}
	}

	// --- pagination ---
	const perPage = 10
	pageParam := r.URL.Query().Get("page")
	page := 1
	if pageParam != "" {
		if p, err := strconv.Atoi(pageParam); err == nil && p > 0 {
			page = p
		}
	}

	var total int64
	_ = q.Count(&total).Error

	totalPages := int(math.Ceil(float64(total) / float64(perPage)))
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	offset := (page - 1) * perPage

	var orders []models.Order
	err := q.
		Order("created_at DESC").
		Limit(perPage).
		Offset(offset).
		Find(&orders).Error
	if err != nil {
		SetFlash(w, r, "error", "Gagal mengambil data pesanan.")
	}

	data := map[string]interface{}{
		"user":          user,
		"isAdmin":       IsAdminUser(user),
		"orders":        orders,
		"cartCount":     server.GetCartCount(w, r),
		"success":       GetFlash(w, r, "success"),
		"error":         GetFlash(w, r, "error"),
		"currentPage":   page,
		"totalPages":    totalPages,
		"statusFilter":  statusFilter,
		"paymentFilter": paymentFilter,
		"dateFrom":      dateFrom,
		"dateTo":        dateTo,
		"query":         r.URL.Query(),
	}

	_ = ren.HTML(w, http.StatusOK, "orders", data)
}

func (server *Server) PayManualForm(w http.ResponseWriter, r *http.Request) {
	ren := userRender()
	vars := mux.Vars(r)
	id := vars["id"]

	if !IsLoggedIn(r) {
		SetFlash(w, r, "error", "Silakan login terlebih dahulu.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user := server.CurrentUser(w, r)

	var order models.Order
	if err := server.DB.Where("id = ? AND user_id = ?", id, user.ID).
		First(&order).Error; err != nil {
		SetFlash(w, r, "error", "Pesanan tidak ditemukan.")
		http.Redirect(w, r, "/orders", http.StatusSeeOther)
		return
	}

	data := map[string]interface{}{
		"user":      user,
		"isAdmin":   IsAdminUser(user),
		"order":     order,
		"cartCount": server.GetCartCount(w, r),
		"success":   GetFlash(w, r, "success"),
		"error":     GetFlash(w, r, "error"),
	}

	_ = ren.HTML(w, http.StatusOK, "order_pay_manual", data)
}
