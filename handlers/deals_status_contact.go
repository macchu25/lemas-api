package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"xkiro-backend/db"
	"xkiro-backend/models"

	"github.com/google/uuid"
)

func DealsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	deals, err := db.DB.GetDeals(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to get deals"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(deals)
}

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	services, err := db.DB.GetStatusServices(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to get status"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"system_status": "All Systems Operational",
		"uptime_sla":    "99.99%",
		"average_ping":  "28ms",
		"regions":       services,
		"last_updated":  time.Now().Format(time.RFC3339),
	})
}

func ContactHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var msg models.ContactMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if msg.Email == "" || msg.Message == "" {
		http.Error(w, `{"error":"email and message are required"}`, http.StatusBadRequest)
		return
	}

	msg.ID = "msg-" + uuid.New().String()
	msg.CreatedAt = time.Now()

	if err := db.DB.CreateContactMessage(r.Context(), &msg); err != nil {
		http.Error(w, `{"error":"failed to save message"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Thank you! Our support team has received your message and will reply via email.",
	})
}
