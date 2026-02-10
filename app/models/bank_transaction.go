package models

import (
	"time"

	"github.com/shopspring/decimal"
)

// Tabel untuk menyimpan mutasi rekening bank
type BankTransaction struct {
	ID      uint            `gorm:"primaryKey;autoIncrement"`
	Bank    string          `gorm:"size:50"`            // BCA / BRI / dll
	Account string          `gorm:"size:100"`           // No. rekening toko
	Amount  decimal.Decimal `gorm:"type:decimal(20,2)"` // Nominal transfer
	Note    string          `gorm:"size:255"`           // Berita / keterangan dari mutasi
	RefCode string          `gorm:"size:100"`           // Kode referensi bank (opsional)
	TrxTime time.Time       // Waktu transaksi di bank

	// Untuk penandaan sudah dipasangkan dengan order mana
	Matched      bool   `gorm:"default:false"`
	MatchedOrder string `gorm:"type:varchar(36);index"` // orders.id
	MatchedAt    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
