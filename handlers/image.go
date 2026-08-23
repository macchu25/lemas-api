package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"xkiro-backend/db"
	"xkiro-backend/models"
)

type ImageQuotaResponse struct {
	Plan         string `json:"plan"`
	DailyUsed    int64  `json:"daily_used"`
	DailyLimit   int64  `json:"daily_limit"`
	Remaining    int64  `json:"remaining"`
	IsUnlimited  bool   `json:"is_unlimited"`
	Allowed      bool   `json:"allowed"`
	Message      string `json:"message,omitempty"`
}

func isPaidPlan(plan string) bool {
	p := strings.ToLower(strings.TrimSpace(plan))
	return p == "pro" || p == "pro-plus" || p == "max" || p == "ultra" || p == "power" || p == "enterprise"
}

func checkAndRefreshImageQuota(user *models.User) {
	today := time.Now().Format("2006-01-02")
	if user.LastImageResetDate != today {
		user.DailyImagesUsed = 0
		user.LastImageResetDate = today
	}

	if isPaidPlan(user.Plan) {
		user.DailyImagesLimit = 500 // Generous/unlimited for paid tiers
	} else {
		user.DailyImagesLimit = 5 // Strict 5 images/day for Free plan users
	}
}

// GET /api/user/image/quota
func ImageQuotaHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(UserContextKey).(string)
	if !ok || userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	user, err := db.DB.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	checkAndRefreshImageQuota(user)
	_ = db.DB.UpdateUser(ctx, user)

	unlimited := isPaidPlan(user.Plan)
	limit := user.DailyImagesLimit
	if limit <= 0 {
		limit = 5
	}
	used := user.DailyImagesUsed
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ImageQuotaResponse{
		Plan:        user.Plan,
		DailyUsed:   used,
		DailyLimit:  limit,
		Remaining:   remaining,
		IsUnlimited: unlimited,
		Allowed:     unlimited || remaining > 0,
	})
}

// POST /api/user/image/consume
func ImageConsumeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(UserContextKey).(string)
	if !ok || userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	user, err := db.DB.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	checkAndRefreshImageQuota(user)

	unlimited := isPaidPlan(user.Plan)
	limit := user.DailyImagesLimit
	if limit <= 0 {
		limit = 5
	}

	if !unlimited && user.DailyImagesUsed >= limit {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"allowed": false,
			"error":   fmt.Sprintf("Bạn đã dùng hết hạn mức %d lượt tạo ảnh miễn phí trong ngày hôm nay. Hãy nâng cấp lên gói Lemas Pro để tạo ảnh không giới hạn!", limit),
			"daily_used": user.DailyImagesUsed,
			"daily_limit": limit,
			"remaining": 0,
		})
		return
	}

	// Increment image count
	user.DailyImagesUsed++
	_ = db.DB.UpdateUser(ctx, user)

	remaining := limit - user.DailyImagesUsed
	if remaining < 0 {
		remaining = 0
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ImageQuotaResponse{
		Plan:        user.Plan,
		DailyUsed:   user.DailyImagesUsed,
		DailyLimit:  limit,
		Remaining:   remaining,
		IsUnlimited: unlimited,
		Allowed:     true,
		Message:     "Lượt tạo ảnh đã được ghi nhận thành công.",
	})
}
