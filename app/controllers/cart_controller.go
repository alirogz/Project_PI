package controllers

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alirogz/goshop/app/models"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// =========================
// Helper: ambil / buat cart
// =========================

// ambil ID cart dari cookie, kalau belum ada bikin baru dan simpan ke cookie
func GetShoppingCartID(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie("cart_id")
	if err != nil || cookie.Value == "" {
		newID := uuid.New().String()
		http.SetCookie(w, &http.Cookie{
			Name:     "cart_id",
			Value:    newID,
			Path:     "/",
			HttpOnly: true,
			Expires:  time.Now().Add(7 * 24 * time.Hour),
		})
		return newID
	}
	return cookie.Value
}

// ambil cart dari DB berdasarkan cartID, kalau belum ada -> buat baru
func GetShoppingCart(db *gorm.DB, cartID string) (*models.Cart, error) {
	if cartID == "" {
		cartID = uuid.New().String()
	}

	cart := &models.Cart{}
	existingCart, err := cart.GetCart(db, cartID)
	if err != nil {
		// kalau cart belum ada → buat baru
		if err == gorm.ErrRecordNotFound {
			createdCart, errCreate := cart.CreateCart(db, cartID)
			if errCreate != nil {
				return nil, errCreate
			}
			return createdCart, nil
		}
		return nil, err
	}

	// pastikan totalWeight tidak nil
	// hitung total berat (gram) dari item di cart
	totalWeight := 0
	for _, item := range existingCart.CartItems {
		// asumsi Weight disimpan per 1 gram (atau nilai numerik yang kamu pakai)
		w, _ := item.Product.Weight.Float64()
		totalWeight += int(w) * item.Qty
	}
	existingCart.TotalWeight = totalWeight

	// hitung ulang total cart
	_, err = existingCart.CalculateCart(db, cartID)
	if err != nil {
		log.Println("CalculateCart error:", err)
	}

	return existingCart, nil
}

// =========================
// Handler: halaman keranjang
// =========================

func (server *Server) GetCart(w http.ResponseWriter, r *http.Request) {
	ren := userRender()
	user := server.CurrentUser(w, r)

	cartID := GetShoppingCartID(w, r)

	// ambil semua alamat user (kalau sudah login)
	var addresses []models.Address
	if user != nil {
		server.DB.
			Where("user_id = ?", user.ID).
			Order("is_primary DESC, created_at DESC").
			Find(&addresses)
	}

	// ambil cart (atau buat baru kalau belum ada)
	cart, err := GetShoppingCart(server.DB, cartID)
	if err != nil {
		log.Println("GetCart error:", err)
		// kalau error, tetap render cart kosong
		_ = ren.HTML(w, http.StatusOK, "cart", map[string]interface{}{
			"user":           user,
			"isAdmin":        IsAdminUser(user),
			"cart":           nil,
			"cartItems":      []models.CartItem{},
			"items":          []models.CartItem{},
			"cartCount":      0,
			"totalPrice":     decimal.Zero,
			"addresses":      addresses,
			"defaultAddress": nil,
		})
		return
	}

	// ambil item-item di cart
	items, err := cart.GetItems(server.DB, cartID)
	if err != nil {
		log.Println("GetItems error:", err)
	}

	// hitung ulang cart supaya grand_total, total_weight, dll ter-update
	cart, err = cart.CalculateCart(server.DB, cartID)
	if err != nil {
		log.Println("CalculateCart error:", err)
	}

	totalPrice := cart.GrandTotal

	// defaultAddress = alamat pertama (biasanya yg primary)
	var defaultAddress *models.Address
	if len(addresses) > 0 {
		defaultAddress = &addresses[0]
	}

	_ = ren.HTML(w, http.StatusOK, "cart", map[string]interface{}{
		"user":           user,
		"isAdmin":        IsAdminUser(user),
		"cart":           cart,
		"cartItems":      items,
		"items":          items,
		"cartCount":      len(items),
		"totalPrice":     totalPrice,
		"addresses":      addresses,
		"defaultAddress": defaultAddress,
	})
}

// =========================
// Handler: tambah item ke cart
// =========================

// =========================
// Handler: tambah item ke cart
// =========================

func (server *Server) AddItemToCart(w http.ResponseWriter, r *http.Request) {
	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		log.Println("ParseForm error:", err)
		http.Redirect(w, r, "/carts", http.StatusSeeOther)
		return
	}

	// ambil product_id & qty dari form
	productID := r.FormValue("product_id")
	if productID == "" {
		// kalau product_id kosong, balik ke halaman produk
		http.Redirect(w, r, "/products", http.StatusSeeOther)
		return
	}

	qtyStr := r.FormValue("qty")
	qty := 1
	if q, err := strconv.Atoi(qtyStr); err == nil && q > 0 {
		qty = q
	}

	// ambil ukuran dari form (boleh kosong)
	size := r.FormValue("size")

	// ambil / buat cart berdasarkan cookie cart_id
	cartID := GetShoppingCartID(w, r)
	cart, err := GetShoppingCart(server.DB, cartID)
	if err != nil {
		log.Println("GetShoppingCart error:", err)
		http.Redirect(w, r, "/carts", http.StatusSeeOther)
		return
	}

	// buat item cart
	item := models.CartItem{
		ProductID: productID,
		Qty:       qty,
		Size:      size,
	}

	// simpan ke database
	if _, err := cart.AddItem(server.DB, item); err != nil {
		log.Println("AddItem error:", err)
	}

	http.Redirect(w, r, "/carts", http.StatusSeeOther)
}

// =========================
// Handler: update qty item
// =========================

func (server *Server) UpdateCartItemQty(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		log.Println("ParseForm error:", err)
		http.Redirect(w, r, "/carts", http.StatusSeeOther)
		return
	}

	// form mengirimkan "update" dengan nilai "plus-<id>" atau "minus-<id>"
	update := r.FormValue("update")
	if update == "" {
		http.Redirect(w, r, "/carts", http.StatusSeeOther)
		return
	}

	parts := strings.SplitN(update, "-", 2)
	if len(parts) != 2 {
		http.Redirect(w, r, "/carts", http.StatusSeeOther)
		return
	}

	action := parts[0] // "plus" atau "minus"
	itemID := parts[1] // id cart_items
	if action != "plus" && action != "minus" {
		http.Redirect(w, r, "/carts", http.StatusSeeOther)
		return
	}

	cartItemModel := models.CartItem{}
	item, err := cartItemModel.GetByID(server.DB, itemID)
	if err != nil {
		log.Println("Get CartItem error:", err)
		http.Redirect(w, r, "/carts", http.StatusSeeOther)
		return
	}

	qty := item.Qty
	if action == "plus" {
		qty++
	} else if action == "minus" {
		qty--
		if qty < 1 {
			qty = 1
		}
	}

	if err := item.UpdateQty(server.DB, itemID, qty); err != nil {
		log.Println("UpdateQty error:", err)
	}

	// setelah update qty, hitung ulang cart
	if item.CartID != "" {
		cart := &models.Cart{}
		if _, err := cart.CalculateCart(server.DB, item.CartID); err != nil {
			log.Println("CalculateCart error:", err)
		}
	}

	http.Redirect(w, r, "/carts", http.StatusSeeOther)
}

// =========================
// Handler: hapus item dari cart
// =========================

// RemoveCartItem menghapus 1 item dari cart berdasarkan ID cart_items
func (server *Server) RemoveCartItem(w http.ResponseWriter, r *http.Request) {
	// Bisa dipanggil via GET /carts/remove/{id} atau POST /carts/remove dengan form item_id

	// 1. Coba ambil dari URL param dulu
	vars := mux.Vars(r)
	itemID := vars["id"]

	// 2. Kalau kosong, coba ambil dari form (POST)
	if itemID == "" {
		if err := r.ParseForm(); err == nil {
			itemID = r.FormValue("item_id")
		}
	}

	// 3. Kalau tetap kosong, langsung balik ke /carts
	if itemID == "" {
		http.Redirect(w, r, "/carts", http.StatusSeeOther)
		return
	}

	// 4. Hapus cart item berdasarkan ID
	cartItemModel := models.CartItem{}
	if err := cartItemModel.RemoveByID(server.DB, itemID); err != nil {
		log.Println("RemoveCartItem error:", err)
	}

	// 5. Redirect ke halaman cart (GetCartDetail nanti yang menghitung ulang total)
	http.Redirect(w, r, "/carts", http.StatusSeeOther)
}
