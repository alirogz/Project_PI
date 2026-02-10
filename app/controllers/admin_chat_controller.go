package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/alirogz/goshop/app/models"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func (server *Server) AdminChatsIndex(w http.ResponseWriter, r *http.Request) {
	ren := userRender()
	user := server.CurrentUser(w, r)
	if user == nil || !IsAdminUser(user) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	var chats []models.Chat
	// preload user; sort by updated_at desc
	if err := server.DB.Model(models.Chat{}).Preload("User").Order("updated_at desc").Find(&chats).Error; err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	unread := map[string]int64{}

	for _, c := range chats {
		var cnt int64

		q := server.DB.Model(models.ChatMessage{}).
			Where("chat_id = ? AND sender_role = ?", c.ID, "user")

		if c.AdminLastReadAt != nil {
			q = q.Where("created_at > ?", *c.AdminLastReadAt)
		}

		q.Count(&cnt)
		unread[c.ID] = cnt
	}

	var totalUnread int64
	for _, v := range unread {
		totalUnread += v
	}

	_ = ren.HTML(w, http.StatusOK, "admin_chats", map[string]interface{}{
		"user":        user,
		"isAdmin":     true,
		"cartCount":   server.GetCartCount(w, r),
		"chats":       chats,
		"unread":      unread,
		"totalUnread": totalUnread,
	})
}

func (server *Server) AdminChatsShow(w http.ResponseWriter, r *http.Request) {
	ren := userRender()
	admin := server.CurrentUser(w, r)
	if admin == nil || !IsAdminUser(admin) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	vars := mux.Vars(r)
	chatID := vars["id"]

	chatModel := models.Chat{}
	chat, err := chatModel.FindByID(server.DB, chatID)
	if err != nil {
		http.Error(w, "Chat not found", http.StatusNotFound)
		return
	}

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
		Update("admin_last_read_at", &now)

	_ = ren.HTML(w, http.StatusOK, "admin_chat_show", map[string]interface{}{
		"user":      admin,
		"isAdmin":   true,
		"cartCount": server.GetCartCount(w, r),
		"chat":      chat,
		"messages":  msgs,
		"lastTs":    lastTs,
	})
}

// ADMIN: fetch messages (polling)
func (server *Server) AdminChatMessages(w http.ResponseWriter, r *http.Request) {
	admin := server.CurrentUser(w, r)
	if admin == nil || !IsAdminUser(admin) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	vars := mux.Vars(r)
	chatID := vars["id"]

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
	msgs, err := msgModel.ListMessagesAfter(server.DB, chatID, after, limit)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"chat_id":  chatID,
		"messages": msgs,
	})
}

// ADMIN: send message
func (server *Server) AdminChatSend(w http.ResponseWriter, r *http.Request) {
	admin := server.CurrentUser(w, r)
	if admin == nil || !IsAdminUser(admin) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	vars := mux.Vars(r)
	chatID := vars["id"]

	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	text := r.FormValue("message")
	if len(text) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	msg := models.ChatMessage{
		ID:         uuid.NewString(),
		ChatID:     chatID,
		SenderID:   admin.ID,
		SenderRole: "admin",
		Message:    text,
	}
	if err := server.DB.Create(&msg).Error; err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "message": msg})
}
