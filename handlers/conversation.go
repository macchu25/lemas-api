package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"xkiro-backend/db"
	"xkiro-backend/models"

	"github.com/google/uuid"
)

// UserConversationsHandler handles:
// GET /api/user/conversations -> returns list of conversations
// POST /api/user/conversations -> creates or updates a conversation
// DELETE /api/user/conversations/:id -> deletes a conversation
func UserConversationsHandler(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(UserContextKey).(string)
	if userID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	path := r.URL.Path
	convID := strings.TrimPrefix(path, "/api/user/conversations/")
	convID = strings.TrimPrefix(convID, "/api/user/conversations")
	convID = strings.TrimPrefix(convID, "/")

	switch r.Method {
	case http.MethodGet:
		if convID != "" {
			c, err := db.DB.GetChatConversationByID(r.Context(), convID, userID)
			if err != nil || c == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "conversation not found"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success":      true,
				"conversation": c,
			})
			return
		}

		list, err := db.DB.GetChatConversationsByUser(r.Context(), userID)
		if err != nil || list == nil {
			list = []models.ChatConversation{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":       true,
			"conversations": list,
		})

	case http.MethodPost:
		var req struct {
			ID       string               `json:"id"`
			Title    string               `json:"title"`
			Model    string               `json:"model"`
			Messages []models.ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}

		if req.ID == "" {
			req.ID = "conv-" + uuid.New().String()
		}
		if req.Title == "" {
			if len(req.Messages) > 0 {
				title := req.Messages[0].Content
				if len([]rune(title)) > 40 {
					title = string([]rune(title)[:40]) + "..."
				}
				req.Title = title
			} else {
				req.Title = "Cuộc trò chuyện mới"
			}
		}
		if req.Model == "" {
			req.Model = "lemas-1.0"
		}

		for i := range req.Messages {
			if req.Messages[i].ID == "" {
				req.Messages[i].ID = "msg-" + uuid.New().String()
			}
			if req.Messages[i].CreatedAt.IsZero() {
				req.Messages[i].CreatedAt = time.Now()
			}
		}

		conv := &models.ChatConversation{
			ID:        req.ID,
			UserID:    userID,
			Title:     req.Title,
			Model:     req.Model,
			Messages:  req.Messages,
			UpdatedAt: time.Now(),
		}

		err := db.DB.SaveChatConversation(r.Context(), conv)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":      true,
			"conversation": conv,
		})

	case http.MethodDelete:
		if convID == "" {
			http.Error(w, `{"error":"missing conversation id"}`, http.StatusBadRequest)
			return
		}
		err := db.DB.DeleteChatConversation(r.Context(), convID, userID)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}
