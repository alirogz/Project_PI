package models

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type Product struct {
	ID               string `gorm:"size:36;not null;uniqueIndex;primary_key"`
	ParentID         string `gorm:"size:36;index"`
	User             User
	UserID           string `gorm:"size:36;index"`
	ProductImages    []ProductImage
	Categories       []Category      `gorm:"many2many:product_categories;"`
	Sku              string          `gorm:"size:100;index"`
	Name             string          `gorm:"size:255"`
	Slug             string          `gorm:"size:255"`
	Price            decimal.Decimal `gorm:"type:decimal(16,2);"`
	Stock            int
	SizeOptions      string          `gorm:"column:size_options"`  // contoh: "S,M,L,XL"
	ColorOptions     string          `gorm:"column:color_options"` // contoh: "Hitam,Putih"
	Weight           decimal.Decimal `gorm:"type:decimal(10,2);"`
	ShortDescription string          `gorm:"type:text"`
	Description      string          `gorm:"type:text"`
	Status           int             `gorm:"default:0"`
	Image            string          `json:"image"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        gorm.DeletedAt
}

func (p *Product) GetProducts(db *gorm.DB, perPage int, page int) (*[]Product, int64, error) {
	var err error
	var products []Product
	var count int64

	err = db.Debug().Model(&Product{}).Count(&count).Error
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage

	err = db.Debug().Model(&Product{}).Order("created_at desc").Limit(perPage).Offset(offset).Find(&products).Error
	if err != nil {
		return nil, 0, err
	}

	return &products, count, nil
}

func (p *Product) FindBySlug(db *gorm.DB, slug string) (*Product, error) {
	var err error
	var product Product

	err = db.Debug().Preload("ProductImages").Model(&Product{}).Where("slug = ?", slug).First(&product).Error
	if err != nil {
		return nil, err
	}

	return &product, nil
}

func (p *Product) FindByID(db *gorm.DB, productID string) (*Product, error) {
	var err error
	var product Product

	err = db.Debug().Preload("ProductImages").Model(&Product{}).Where("id = ?", productID).First(&product).Error
	if err != nil {
		return nil, err
	}

	return &product, nil
}

func (p Product) CreatedAtFormatted() string {
	if p.CreatedAt.IsZero() {
		return "-" // kalau tanggalnya kosong / 0001-01-01
	}
	// format bebas, contoh: 2025-10-24 15:24
	return p.CreatedAt.Format("2006-01-02 15:04")
}

// Product adalah struct kamu yang sudah punya relasi ProductImages []ProductImage

func (p *Product) GetLatestProductsWithImages(db *gorm.DB, limit int) ([]Product, error) {
	var products []Product

	err := db.
		Preload("ProductImages"). // PENTING: preload relasi gambar
		Order("created_at desc").
		Limit(limit).
		Find(&products).Error

	return products, err
}

func (p *Product) SizeList() []string {
	if p.SizeOptions == "" {
		return []string{"S", "M", "L", "XL"} // fallback
	}
	parts := strings.Split(p.SizeOptions, ",")
	var out []string
	for _, s := range parts {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (p *Product) ColorList() []string {
	if p.ColorOptions == "" {
		return nil
	}
	parts := strings.Split(p.ColorOptions, ",")
	var out []string
	for _, s := range parts {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
