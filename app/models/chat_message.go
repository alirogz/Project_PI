package models

import (
	"time"

	"gorm.io/gorm"
)

type ChatMessage struct {
	ID         string `gorm:"size:36;not null;uniqueIndex;primary_key"`
	ChatID     string `gorm:"size:36;not null;index"`
	SenderID   string `gorm:"size:36;not null;index"`
	SenderRole string `gorm:"size:20;not null;index"` // "user" | "admin"
	Message    string `gorm:"type:text;not null"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  gorm.DeletedAt
}

// ListMessagesAfter: ambil pesan setelah waktu tertentu (unix milli), opsional.
func (m *ChatMessage) ListMessagesAfter(db *gorm.DB, chatID string, afterUnixMilli int64, limit int) ([]ChatMessage, error) {
	var msgs []ChatMessage
	q := db.Model(ChatMessage{}).Where("chat_id = ?", chatID)
	if afterUnixMilli > 0 {
		after := time.Unix(0, afterUnixMilli*int64(time.Millisecond))
		q = q.Where("created_at > ?", after)
	}
	if limit <= 0 {
		limit = 50
	}
	err := q.Order("created_at asc").Limit(limit).Find(&msgs).Error
	return msgs, err
}
