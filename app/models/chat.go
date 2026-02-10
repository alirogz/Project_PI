package models

import (
	"time"

	"gorm.io/gorm"
)

// Chat merepresentasikan 1 ruang percakapan antara 1 user dan admin.
// Versi sederhana: 1 user = 1 chat (1 thread).
type Chat struct {
	ID              string        `gorm:"size:36;not null;uniqueIndex;primary_key"`
	UserID          string        `gorm:"size:36;not null;index"`
	User            User          `gorm:"foreignKey:UserID"`
	Messages        []ChatMessage `gorm:"foreignKey:ChatID"`
	AdminLastReadAt *time.Time
	UserLastReadAt  *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       gorm.DeletedAt
}

func (c *Chat) FindByID(db *gorm.DB, chatID string) (*Chat, error) {
	var chat Chat
	err := db.Model(Chat{}).
		Preload("User").
		Where("id = ?", chatID).
		First(&chat).Error
	if err != nil {
		return nil, err
	}
	return &chat, nil
}

// FindOrCreateByUserID: 1 user => 1 chat thread.
func (c *Chat) FindOrCreateByUserID(db *gorm.DB, chatID, userID string) (*Chat, error) {
	var chat Chat
	err := db.Model(Chat{}).Where("user_id = ?", userID).First(&chat).Error
	if err == nil {
		return &chat, nil
	}

	chat = Chat{
		ID:     chatID,
		UserID: userID,
	}
	if err := db.Create(&chat).Error; err != nil {
		return nil, err
	}
	return &chat, nil
}
