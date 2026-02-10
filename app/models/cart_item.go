package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/shopspring/decimal"
)

type CartItem struct {
	ID              string `gorm:"size:36;not null;uniqueIndex;primary_key"`
	Cart            Cart
	CartID          string `gorm:"size:36;index"`
	Product         Product
	ProductID       string `gorm:"size:36;index"`
	Size            string `gorm:"size:20"`
	Qty             int
	BasePrice       decimal.Decimal `gorm:"type:decimal(16,2)"`
	BaseTotal       decimal.Decimal `gorm:"type:decimal(16,2)"`
	TaxAmount       decimal.Decimal `gorm:"type:decimal(16,2)"`
	TaxPercent      decimal.Decimal `gorm:"type:decimal(10,2)"`
	DiscountAmount  decimal.Decimal `gorm:"type:decimal(16,2)"`
	DiscountPercent decimal.Decimal `gorm:"type:decimal(10,2)"`
	SubTotal        decimal.Decimal `gorm:"type:decimal(16,2)"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (c *CartItem) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}

	return nil
}

func (c *CartItem) GetByID(db *gorm.DB, id string) (*CartItem, error) {
	var item CartItem

	err := db.Debug().
		Preload("Product").
		Model(&CartItem{}).
		Where("id = ?", id).
		First(&item).Error
	if err != nil {
		return nil, err
	}

	return &item, nil
}

func (c *CartItem) UpdateQty(db *gorm.DB, itemID string, qty int) error {
	var cart Cart

	_, err := cart.UpdateItemQty(db, itemID, qty)
	if err != nil {
		return err
	}

	return nil
}

func (c *CartItem) RemoveByID(db *gorm.DB, id string) error {
	return db.Debug().
		Where("id = ?", id).
		Delete(&CartItem{}).Error
}
