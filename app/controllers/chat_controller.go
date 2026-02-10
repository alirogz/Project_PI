package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/alirogz/goshop/app/models"
	"github.com/google/uuid"
)

// USER CHAT PAGE
func (server *Server) ChatPage(w http.ResponseWriter, r *http.Request) {
	ren := userRender()
	user := server.CurrentUser(w, r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// 1 user => 1 chat (create if not exists)
	chatModel := models.Chat{}
	chat, err := chatModel.FindOrCreateByUserID(server.DB, uuid.NewString(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// load last 50 messages
	msgModel := models.ChatMessage{}
	msgs, err := msgModel.ListMessagesAfter(server.DB, chat.ID, 0, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	lastTs := int64(0)
	if len(msgs) > 0 {
		lastTs = msgs[len(msgs)-1].CreatedAt.UnixMilli()
	}

	now := time.Now()
	server.DB.Model(&models.Chat{}).
		Where("id = ?", chat.ID).
		Update("user_last_read_at", &now)

	var userUnread int64

	q := server.DB.Model(models.ChatMessage{}).
		Where("chat_id = ? AND sender_role = ?", chat.ID, "admin")

	if chat.UserLastReadAt != nil {
		q = q.Where("created_at > ?", *chat.UserLastReadAt)
	}
	q.Count(&userUnread)

	_ = ren.HTML(w, http.StatusOK, "chat", map[string]interface{}{
		"user":       user,
		"isAdmin":    IsAdminUser(user),
		"cartCount":  server.GetCartCount(w, r),
		"chat":       chat,
		"messages":   msgs,
		"lastTs":     lastTs,
		"userUnread": userUnread,
	})
}

// USER: fetch messages (polling)
func (server *Server) ChatMessages(w http.ResponseWriter, r *http.Request) {
	user := server.CurrentUser(w, r)
	if user == nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	chatModel := models.Chat{}
	chat, err := chatModel.FindOrCreateByUserID(server.DB, uuid.NewString(), user.ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	afterStr := r.URL.Query().Get("after")
	after := int64(0)
	if afterStr != "" {
		if v, err := strconv.ParseInt(afterStr, 10, 64); err == nil {
			after = v
		}
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil {
			limit = v
		}
	}

	msgModel := models.ChatMessage{}
	msgs, err := msgModel.ListMessagesAfter(server.DB, chat.ID, after, limit)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"chat_id":  chat.ID,
		"messages": msgs,
	})
}

// USER: send message
func (server *Server) ChatSend(w http.ResponseWriter, r *http.Request) {
	user := server.CurrentUser(w, r)
	if user == nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	text := r.FormValue("message")
	if len(text) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	chatModel := models.Chat{}
	chat, err := chatModel.FindOrCreateByUserID(server.DB, uuid.NewString(), user.ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	msg := models.ChatMessage{
		ID:         uuid.NewString(),
		ChatID:     chat.ID,
		SenderID:   user.ID,
		SenderRole: "user",
		Message:    text,
	}
	if err := server.DB.Create(&msg).Error; err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "message": msg})
}
