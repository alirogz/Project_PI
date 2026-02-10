package models

import (
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/alirogz/goshop/app/consts"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type Order struct {
	ID            string `gorm:"size:36;not null;uniqueIndex;primary_key"`
	UserID        string `gorm:"size:36;index"`
	User          User
	OrderItems    []OrderItem
	OrderCustomer *OrderCustomer
	Code          string `gorm:"size:50;index"`

	// STATUS & PAYMENT (pakai struktur asli)
	Status        int
	OrderDate     time.Time
	PaymentDue    time.Time
	PaymentStatus string `gorm:"size:50;index"`

	PaidAt       sql.NullTime   `gorm:"type:timestamp"`
	PaymentToken sql.NullString `gorm:"size:100;index"`

	// TOTAL & BIAYA
	BaseTotalPrice    decimal.Decimal `gorm:"type:decimal(16,2)"`
	TaxAmount         decimal.Decimal `gorm:"type:decimal(16,2)"`
	TaxPercent        decimal.Decimal `gorm:"type:decimal(10,2)"`
	DiscountAmount    decimal.Decimal `gorm:"type:decimal(16,2)"`
	DiscountPercent   decimal.Decimal `gorm:"type:decimal(10,2)"`
	ShippingCost      decimal.Decimal `gorm:"type:decimal(16,2)"`
	GrandTotal        decimal.Decimal `gorm:"type:decimal(16,2)"`
	PaymentUniqueCode int             `gorm:"column:payment_unique_code"`
	PaymentTotal      decimal.Decimal `gorm:"column:payment_total"`

	// INFO PENGIRIMAN & APPROVAL
	Note                string         `gorm:"type:text"`
	ShippingCourier     string         `gorm:"size:100"`
	ShippingServiceName string         `gorm:"size:100"`
	ApprovedBy          sql.NullString `gorm:"size:36"`
	ApprovedAt          sql.NullTime
	CancelledBy         sql.NullString `gorm:"size:36"`
	CancelledAt         sql.NullTime
	CancellationNote    sql.NullString `gorm:"size:255"`

	// FIELD TAMBAHAN UNTUK PEMBAYARAN MANUAL
	PaymentMethod string `gorm:"size:50"`  // contoh: "Transfer Bank"
	PaymentProof  string `gorm:"size:255"` // nama file bukti transfer

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt
}

// Step: untuk kebutuhan tampilan tracking status (1–3)
func (o Order) Step() int {
	switch o.Status {
	case consts.OrderStatusPending:
		return 1
	case consts.OrderStatusReceived:
		return 2
	case consts.OrderStatusDelivered:
		return 3
	case consts.OrderStatusCancelled:
		// Cancelled kita kasih 0 (nanti bisa dikasih style khusus)
		return 0
	default:
		return 1
	}
}

func (o *Order) BeforeCreate(db *gorm.DB) error {
	if o.ID == "" {
		o.ID = uuid.New().String()
	}

	o.Code = generateOrderNumber(db)

	return nil
}

func (o *Order) CreateOrder(db *gorm.DB, order *Order) (*Order, error) {
	result := db.Debug().Create(order)
	if result.Error != nil {
		return nil, result.Error
	}

	return order, nil
}

func (o *Order) FindByID(db *gorm.DB, id string) (*Order, error) {
	var order Order

	err := db.Debug().
		Preload("OrderCustomer").
		Preload("OrderItems").
		Preload("OrderItems.Product").
		Preload("User").
		Model(&Order{}).Where("id = ?", id).
		First(&order).Error
	if err != nil {
		return nil, err
	}

	return &order, nil
}

func (o *Order) GetStatusLabel() string {
	var statusLabel string

	switch o.Status {
	case consts.OrderStatusPending:
		statusLabel = "PENDING"
	case consts.OrderStatusDelivered:
		statusLabel = "DELIVERED"
	case consts.OrderStatusReceived:
		statusLabel = "RECEIVED"
	case consts.OrderStatusCancelled:
		statusLabel = "CANCELLED"
	default:
		statusLabel = "UNKNOWN"
	}

	return statusLabel
}

// IsPaid: true kalau PaidAt valid atau status payment = paid
func (o *Order) IsPaid() bool {
	return (o.PaidAt.Valid) || (o.PaymentStatus == consts.OrderPaymentStatusPaid)
}

func generateOrderNumber(db *gorm.DB) string {
	now := time.Now()
	month := now.Month()
	year := strconv.Itoa(now.Year())

	dateCode := "/ORDER/" + intToRoman(int(month)) + "/" + year

	var latestOrder Order

	err := db.Debug().Order("created_at DESC").Find(&latestOrder).Error

	latestNumber, _ := strconv.Atoi(strings.Split(latestOrder.Code, "/")[0])
	if err != nil {
		latestNumber = 1
	}

	number := latestNumber + 1

	invoiceNumber := strconv.Itoa(number) + dateCode

	return invoiceNumber
}

func intToRoman(num int) string {
	values := []int{
		1000, 900, 500, 400,
		100, 90, 50, 40,
		10, 9, 5, 4, 1,
	}

	symbols := []string{
		"M", "CM", "D", "CD",
		"C", "XC", "L", "XL",
		"X", "IX", "V", "IV",
		"I"}
	roman := ""
	i := 0

	for num > 0 {
		// calculate the number of times this num is completly divisible by values[i]
		// times will only be > 0, when num >= values[i]
		k := num / values[i]
		for j := 0; j < k; j++ {
			// buildup roman numeral
			roman += symbols[i]

			// reduce the value of num.
			num -= values[i]
		}
		i++
	}
	return roman
}

// MarkAsPaid: menandai order sudah dibayar
func (o *Order) MarkAsPaid(db *gorm.DB) error {
	now := time.Now()
	o.PaidAt = sql.NullTime{Time: now, Valid: true}
	o.PaymentStatus = consts.OrderPaymentStatusPaid

	return db.Model(o).Updates(map[string]interface{}{
		"paid_at":        o.PaidAt,
		"payment_status": consts.OrderPaymentStatusPaid,
		"updated_at":     time.Now(),
	}).Error
}

func (o Order) GrandTotalFloat() float64 {
	return o.GrandTotal.InexactFloat64()
}

func (o Order) PaymentTotalFloat() float64 {
	f, _ := o.PaymentTotal.Float64()
	return f
}

// SubtotalFloat: konversi BaseTotalPrice (decimal) ke float64
func (o Order) SubtotalFloat() float64 {
	return o.BaseTotalPrice.InexactFloat64()
}

// ShippingCostFloat: konversi ShippingCost (decimal) ke float64
func (o Order) ShippingCostFloat() float64 {
	return o.ShippingCost.InexactFloat64()
}

// StatusText: teks status untuk ditampilkan ke user
// StatusText: ubah kode angka di DB jadi label yang enak dibaca
func (o Order) StatusText() string {
	switch o.Status {
	case 0:
		return "Pending"
	case 1:
		return "Diproses"
	case 2:
		return "Dikirim"
	case 3:
		return "Selesai"
	default:
		return "Unknown"
	}
}

// StatusStep: dipakai untuk stepper (1–4)
func (o Order) StatusStep() int {
	switch o.Status {
	case 0:
		return 1 // Pending
	case 1:
		return 2 // Diproses
	case 2:
		return 3 // Dikirim
	case 3:
		return 4 // Selesai
	default:
		return 1
	}
}

func (o Order) PaymentStatusText() string {
	switch o.PaymentStatus {
	case consts.OrderPaymentStatusUnpaid:
		return "Belum Dibayar"
	case consts.OrderPaymentStatusPaid:
		return "Lunas"
	case "waiting_review":
		return "Menunggu Konfirmasi"
	case "rejected":
		return "Ditolak"
	default:
		return "Unknown"
	}
}
