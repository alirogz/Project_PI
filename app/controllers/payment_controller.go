package controllers

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alirogz/goshop/app/consts"
	"github.com/alirogz/goshop/app/models"
	"github.com/shopspring/decimal"
)

/*
   ==========================
   Shipping (local rules)
   ==========================
   Kita buat opsi ongkir lokal tanpa API:
   - REG: 10.000 + 2.000 per 1kg (dibulatkan ke atas)
   - YES: 20.000 + 3.000 per 1kg (dibulatkan ke atas)
   - FREE: 0 (untuk promo / debugging)
*/

// GET /shipping/options?weight=1000
// weight = gram (default 1000)
func (server *Server) ShippingOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	weightGram := 1000
	if s := r.URL.Query().Get("weight"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			weightGram = v
		}
	}
	kg := (weightGram + 999) / 1000 // ceil(gram/1000)

	type opt struct {
		Service string `json:"service"`
		Fee     int    `json:"fee"`
	}
	options := []opt{
		{Service: "REG", Fee: 10000 + 2000*kg},
		{Service: "YES", Fee: 20000 + 3000*kg},
		{Service: "FREE", Fee: 0},
	}

	_ = json.NewEncoder(w).Encode(Result{Code: 200, Data: options, Message: "ok"})
}

/*
   ==========================
   Mock Payment (tanpa Midtrans)
   ==========================
*/

// POST /payments/mock
// Body JSON: { "order_id": "xxxx", "amount": 125000, "provider": "manual" }
type mockPayReq struct {
	OrderID  string `json:"order_id"`
	Amount   int64  `json:"amount"`
	Provider string `json:"provider"` // "manual", "transfer", "cod", dll
}

func (server *Server) MockPay(w http.ResponseWriter, r *http.Request) {
	user := server.CurrentUser(w, r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req mockPayReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" || req.Amount <= 0 {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Pastikan order ada dan milik user (opsional: validasi owner)
	orderModel := models.Order{}
	order, err := orderModel.FindByID(server.DB, req.OrderID)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}
	if order.IsPaid() {
		_ = json.NewEncoder(w).Encode(Result{Code: 200, Data: nil, Message: "Already paid"})
		return
	}

	// Simpan payment record
	paymentModel := models.Payment{}
	amt := decimal.NewFromInt(req.Amount)
	payload := json.RawMessage([]byte(`{"provider":"` + req.Provider + `","at":"` + time.Now().Format(time.RFC3339) + `"}`))

	if _, err := paymentModel.CreatePayment(server.DB, &models.Payment{
		OrderID:           order.ID,
		Amount:            amt,
		TransactionID:     "MOCK-" + time.Now().Format("20060102150405"),
		TransactionStatus: consts.PaymentStatusSettlement,
		Payload:           &payload,
		PaymentType:       "manual",
	}); err != nil {
		http.Error(w, "failed to save payment", http.StatusBadRequest)
		return
	}

	// Tandai order sebagai paid
	if err := order.MarkAsPaid(server.DB); err != nil {
		http.Error(w, "failed to mark order paid", http.StatusBadRequest)
		return
	}

	_ = json.NewEncoder(w).Encode(Result{
		Code: 200,
		Data: map[string]string{
			"status":   "paid",
			"order_id": order.ID,
			"message":  "Payment saved (mock).",
		},
		Message: "ok",
	})
}

/*
   ==========================
   Auto-match pembayaran
   ==========================
*/

// POST /admin/payments/auto-match
// POST /admin/payments/auto-match
func (s *Server) AutoMatchPayments(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	db := s.DB

	// 1. Ambil semua order UNPAID dengan payment_total > 0 dan metode Transfer Bank
	var orders []models.Order
	if err := db.
		Where("payment_status = ? AND payment_method = ? AND payment_total > 0",
			consts.OrderPaymentStatusUnpaid, "Transfer Bank").
		Find(&orders).Error; err != nil {

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "error",
			"message": "Gagal mengambil data order",
		})
		return
	}

	if len(orders) == 0 {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"message": "Tidak ada order UNPAID dengan payment_total > 0",
			"matched": 0,
		})
		return
	}

	// 2. Ambil semua mutasi bank yang belum dipasangkan
	var bankTxs []models.BankTransaction
	if err := db.
		Where("matched = ?", false).
		Find(&bankTxs).Error; err != nil {

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "error",
			"message": "Gagal mengambil data mutasi bank",
		})
		return
	}

	if len(bankTxs) == 0 {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"message": "Tidak ada mutasi bank yang belum dipasangkan",
			"matched": 0,
		})
		return
	}

	// 3. Buat index: amount (dibulatkan ke rupiah) -> list index transaksi
	amountToIdx := make(map[int][]int)
	for i, tx := range bankTxs {
		amtFloat := tx.Amount.InexactFloat64() // contoh 344389.00
		key := int(math.Round(amtFloat))       // jadi 344389
		amountToIdx[key] = append(amountToIdx[key], i)
	}

	var matchedCount int

	// 4. Untuk setiap order, cari mutasi dengan nominal yang sama
	//    lalu pilih kandidat terbaik berdasarkan:
	//    - tanggal transaksi paling dekat dengan tanggal order
	//    - note/berita mengandung hint ID order (jika ada)
	for i := range orders {
		order := &orders[i]

		// konversi PaymentTotal (decimal) ke rupiah int
		orderAmt := int(math.Round(order.PaymentTotalFloat())) // 344389.00 -> 344389

		idxList, ok := amountToIdx[orderAmt]
		if !ok || len(idxList) == 0 {
			continue // tidak ada mutasi dengan nominal ini
		}

		// pilih kandidat terbaik di antara transaksi dengan nominal sama
		bestIdx := -1
		bestScore := -1
		var bestDiff time.Duration

		for _, idx := range idxList {
			tx := &bankTxs[idx]

			score, diff := calculateMatchScore(order, tx)

			if bestIdx == -1 || score > bestScore || (score == bestScore && diff < bestDiff) {
				bestIdx = idx
				bestScore = score
				bestDiff = diff
			}
		}

		if bestIdx == -1 {
			continue
		}

		// Jika tidak ada petunjuk kuat dan selisih waktu terlalu jauh (> 24 jam),
		// kita skip supaya tidak salah pasang.
		if bestDiff > 24*time.Hour && bestScore < 2 {
			continue
		}

		// keluarkan bestIdx dari antrian amountToIdx
		txIdx := bestIdx
		newList := make([]int, 0, len(idxList)-1)
		for _, idx := range idxList {
			if idx != txIdx {
				newList = append(newList, idx)
			}
		}
		if len(newList) == 0 {
			delete(amountToIdx, orderAmt)
		} else {
			amountToIdx[orderAmt] = newList
		}

		bankTx := bankTxs[txIdx]

		// 5. Tandai order sebagai PAID (pakai helper yang sudah ada)
		if err := order.MarkAsPaid(db); err != nil {
			continue
		}

		// 6. Tandai mutasi sebagai sudah dipasangkan
		now := time.Now()
		bankTx.Matched = true
		bankTx.MatchedOrder = order.ID
		bankTx.MatchedAt = now
		if err := db.Save(&bankTx).Error; err != nil {
			continue
		}

		matchedCount++
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "Proses auto-match selesai",
		"matched": matchedCount,
	})
}

// calculateMatchScore memberikan skor kecocokan antara satu order dan satu mutasi bank.
// Semakin tinggi skor, semakin yakin bahwa keduanya adalah pasangan yang benar.
// Selain skor, fungsi ini juga mengembalikan selisih waktu absolut antara
// waktu order dan waktu transaksi di bank.
func calculateMatchScore(order *models.Order, tx *models.BankTransaction) (int, time.Duration) {
	score := 0
	// default selisih waktu sangat besar
	diff := time.Duration(1<<63 - 1)

	// 1. Nilai tambah dari kedekatan tanggal/waktu
	//    Kalau struct Order kamu tidak punya CreatedAt, bisa diganti ke field lain.
	if !tx.TrxTime.IsZero() && !order.CreatedAt.IsZero() {
		d := tx.TrxTime.Sub(order.CreatedAt)
		if d < 0 {
			d = -d
		}
		diff = d

		// <= 24 jam dianggap sangat dekat
		if d <= 24*time.Hour {
			score += 1
		}
		// <= 6 jam boleh dapat bonus point lagi
		if d <= 6*time.Hour {
			score += 1
		}
	}

	// 2. Nilai tambah dari adanya hint order di Note
	//    Sekarang kita pakai ID order; kalau kamu punya kode lain (misalnya order.Code),
	//    bisa ditambahkan di sini.
	note := strings.ToLower(tx.Note)
	if note != "" {
		if strings.Contains(note, strings.ToLower(order.ID)) {
			score += 2
		}

		// CONTOH (opsional): kalau di Note ada "order #33" dan kamu punya field
		// order.Number (int) atau order.Code, kamu bisa tambah logika di sini.
		// Silakan sesuaikan dengan struktur modelmu sendiri.
	}

	return score, diff
}

/*
   ==========================
   Import mutasi bank via CSV
   ==========================
*/

// GET /admin/payments/import
// GET /admin/payments/import
// GET /admin/payments/import
func (s *Server) ShowImportBankPage(w http.ResponseWriter, r *http.Request) {
	if !IsLoggedIn(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	admin := s.CurrentUser(w, r)
	if !IsAdminUser(admin) {
		SetFlash(w, r, "error", "Unauthorized")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Ambil 20 mutasi bank terbaru
	var bankTxs []models.BankTransaction
	if err := s.DB.Order("trx_time DESC").Limit(20).Find(&bankTxs).Error; err != nil {
		// kalau error, kita cuma simpan flash, halaman tetap bisa dibuka
		SetFlash(w, r, "error", "Gagal mengambil data mutasi bank")
	}

	ren := adminRender()
	_ = ren.HTML(w, http.StatusOK, "admin_payments_import", map[string]interface{}{
		"user":      admin,
		"isAdmin":   IsAdminUser(admin),
		"cartCount": s.GetCartCount(w, r),
		"success":   r.URL.Query().Get("success"),
		"error":     GetFlash(w, r, "error"),
		"bankTxs":   bankTxs, // <— ini yang dipakai di tabel
	})

}

// POST /admin/payments/import
func (s *Server) HandleImportBankCSV(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	err := r.ParseMultipartForm(10 << 20) // max 10MB
	if err != nil {
		http.Error(w, "Gagal parsing form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File tidak ditemukan", http.StatusBadRequest)
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = ';'

	// Skip header: Tanggal;Deskripsi;Debit;Kredit;Saldo
	_, _ = reader.Read()

	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		if len(rec) < 4 {
			continue
		}

		tanggalStr := strings.TrimSpace(rec[0])
		desc := strings.TrimSpace(rec[1])
		amountStr := strings.TrimSpace(rec[3])

		// buang titik/koma
		amountStr = strings.ReplaceAll(amountStr, ".", "")
		amountStr = strings.ReplaceAll(amountStr, ",", "")

		amountDec, err := decimal.NewFromString(amountStr)
		if err != nil {
			continue
		}

		trxTime, err := time.Parse("02/01/2006", tanggalStr)
		if err != nil {
			trxTime = time.Now()
		}

		now := time.Now()

		tx := models.BankTransaction{
			Bank:         "BANK",
			Account:      "",
			Amount:       amountDec,
			Note:         desc,
			RefCode:      "",
			TrxTime:      trxTime,
			Matched:      false,
			MatchedOrder: "",
			MatchedAt:    now,
		}

		if err := s.DB.Create(&tx).Error; err != nil {
			log.Println("Insert error:", err)
			continue
		}
	}

	http.Redirect(w, r, "/admin/payments/import?success=Berhasil+import", http.StatusFound)
}

// DEBUG: cek isi tabel bank_transactions
// GET /admin/payments/bank-tx/debug
func (s *Server) DebugListBankTx(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var txs []models.BankTransaction
	if err := s.DB.Order("id desc").Limit(20).Find(&txs).Error; err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "error",
			"message": "Gagal mengambil data mutasi",
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"count":  len(txs),
		"data":   txs,
	})
}
