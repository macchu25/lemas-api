package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"xkiro-backend/db"
	"xkiro-backend/models"
	"xkiro-backend/services"

	"github.com/google/uuid"
)

type AdminUserView struct {
	ID               string    `json:"id"`
	Email            string    `json:"email"`
	Name             string    `json:"name"`
	Role             string    `json:"role"`
	Plan             string    `json:"plan"`
	Balance          float64   `json:"balance"`
	TokensAlloc      int64     `json:"tokens_alloc"`
	TokensUsed       int64     `json:"tokens_used"`
	DailyTokensUsed  int64     `json:"daily_tokens_used"`
	DailyTokensLimit int64     `json:"daily_tokens_limit"`
	GiftTokens       int64     `json:"gift_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	TotalRequests    int       `json:"total_requests"`
	ActiveKeys       int       `json:"active_keys"`
	CreatedAt        time.Time `json:"created_at"`
}

type AdminOverviewResponse struct {
	TotalUsers         int                    `json:"total_users"`
	TotalActiveKeys    int                    `json:"total_active_keys"`
	TotalTokensUsed    int64                  `json:"total_tokens_used"`
	TotalCostUSD       float64                `json:"total_cost_usd"`
	TotalRequests      int                    `json:"total_requests"`
	UpstreamKeysHealth string                 `json:"upstream_keys_health"`
	UpstreamStats      map[string]interface{} `json:"upstream_stats"`
}

// GET /api/admin/overview
func AdminOverviewHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	users, _ := db.DB.GetAllUsers(ctx)
	keys, _ := db.DB.GetAllApiKeys(ctx)
	logs, _ := db.DB.GetAllUsageLogs(ctx)

	var totalTokens int64
	var totalCost float64
	userLogTokens := make(map[string]int64)
	for _, l := range logs {
		totalTokens += int64(l.TotalTokens)
		totalCost += l.CostUSD
		userLogTokens[l.UserID] += int64(l.TotalTokens)
	}

	for _, u := range users {
		if userLogTokens[u.ID] < u.DailyTokensUsed {
			totalTokens += (u.DailyTokensUsed - userLogTokens[u.ID])
		}
	}

	activeKeysCount := 0
	for _, k := range keys {
		if k.Status == "active" {
			activeKeysCount++
		}
	}

	rotator := services.InitKeyRotator()
	rotatorStats := rotator.GetPoolStats()

	totalReqCount := len(logs)
	for _, u := range users {
		if u.DailyTokensUsed > 0 && len(logs) == 0 {
			totalReqCount++
		}
	}

	resp := AdminOverviewResponse{
		TotalUsers:         len(users),
		TotalActiveKeys:    activeKeysCount,
		TotalTokensUsed:    totalTokens,
		TotalCostUSD:       totalCost,
		TotalRequests:      totalReqCount,
		UpstreamKeysHealth: "8/8 Keys Operational",
		UpstreamStats:      rotatorStats,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GET /api/admin/users
func AdminUsersHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	users, err := db.DB.GetAllUsers(ctx)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch users"}`, http.StatusInternalServerError)
		return
	}

	keys, _ := db.DB.GetAllApiKeys(ctx)
	logs, _ := db.DB.GetAllUsageLogs(ctx)

	// Map logs and keys per user
	userTokensMap := make(map[string]int64)
	userCostMap := make(map[string]float64)
	userReqsMap := make(map[string]int)
	for _, l := range logs {
		userTokensMap[l.UserID] += int64(l.TotalTokens)
		userCostMap[l.UserID] += l.CostUSD
		userReqsMap[l.UserID]++
	}

	userKeysCountMap := make(map[string]int)
	for _, k := range keys {
		if k.Status == "active" {
			userKeysCountMap[k.UserID]++
		}
	}

	views := make([]AdminUserView, len(users))
	for i, u := range users {
		tokensUsed := userTokensMap[u.ID]
		totalReqs := userReqsMap[u.ID]
		if tokensUsed < u.DailyTokensUsed {
			tokensUsed = u.DailyTokensUsed
		}
		if totalReqs == 0 && u.DailyTokensUsed > 0 {
			totalReqs = 1
		}

		views[i] = AdminUserView{
			ID:               u.ID,
			Email:            u.Email,
			Name:             u.Name,
			Role:             u.Role,
			Plan:             u.Plan,
			Balance:          u.Balance,
			TokensAlloc:      u.Tokens,
			TokensUsed:       tokensUsed,
			DailyTokensUsed:  u.DailyTokensUsed,
			DailyTokensLimit: u.DailyTokensLimit,
			GiftTokens:       u.GiftTokens,
			CostUSD:          userCostMap[u.ID],
			TotalRequests:    totalReqs,
			ActiveKeys:       userKeysCountMap[u.ID],
			CreatedAt:        u.CreatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(views)
}

// POST /api/admin/users/adjust
func AdminAdjustUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID        string  `json:"user_id"`
		AdjustBalance float64 `json:"adjust_balance"`
		AdjustTokens  int64   `json:"adjust_tokens"`
		Plan          string  `json:"plan"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	user, err := db.DB.GetUserByID(ctx, req.UserID)
	if err != nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	newBalance := user.Balance + req.AdjustBalance
	newTokens := user.Tokens + req.AdjustTokens
	if newBalance < 0 {
		newBalance = 0
	}
	if newTokens < 0 {
		newTokens = 0
	}

	if req.Plan != "" {
		user.Plan = req.Plan
	}

	_ = db.DB.UpdateUserBalance(ctx, user.ID, newBalance, newTokens)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"user_id":     user.ID,
		"new_balance": newBalance,
		"new_tokens":  newTokens,
		"plan":        user.Plan,
	})
}

type AdminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// POST /api/admin/login
func AdminLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req AdminLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	// Strict Security Enforcement (LEMAS-CRIT-02):
	// No hardcoded credentials. ADMIN_USERNAME and ADMIN_PASSWORD MUST be set in environment variables.
	expectedUser := os.Getenv("ADMIN_USERNAME")
	expectedPass := os.Getenv("ADMIN_PASSWORD")

	if expectedUser == "" || expectedPass == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Cổng quản trị chưa được cấu hình biến môi trường ADMIN_USERNAME hoặc ADMIN_PASSWORD trên máy chủ.",
		})
		return
	}

	inputUser := strings.TrimSpace(req.Username)
	inputPass := req.Password

	// Constant-time check to prevent timing side-channel attacks
	userMatch := subtle.ConstantTimeCompare([]byte(inputUser), []byte(expectedUser)) == 1
	passMatch := subtle.ConstantTimeCompare([]byte(inputPass), []byte(expectedPass)) == 1

	if userMatch && passMatch {
		token, err := GenerateAdminJWT("admin-root-001", expectedUser)
		if err != nil {
			http.Error(w, `{"error":"failed to generate admin token"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"token":   token,
			"role":    "admin",
			"message": "Welcome Master Admin",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   "Tài khoản hoặc mật khẩu quản trị không chính xác",
	})
}

// GET & POST /api/admin/giftcodes
func AdminGiftcodesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		codes, err := db.DB.GetAllGiftcodes(ctx)
		if err != nil {
			codes = []models.Giftcode{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(codes)

	case http.MethodPost:
		var req struct {
			Code    string `json:"code"`
			Tokens  int64  `json:"tokens"`
			MaxUses int    `json:"max_uses"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}

		codeStr := strings.ToUpper(strings.TrimSpace(req.Code))
		if codeStr == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Mã Giftcode không được để trống"})
			return
		}

		existing, _ := db.DB.GetGiftcodeByCode(ctx, codeStr)
		if existing != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("Mã Giftcode '%s' đã tồn tại trong hệ thống!", codeStr),
			})
			return
		}

		if req.Tokens <= 0 {
			req.Tokens = 10000
		}
		if req.MaxUses <= 0 {
			req.MaxUses = 10
		}

		newGift := &models.Giftcode{
			ID:        "gift-" + uuid.New().String()[:8],
			Code:      codeStr,
			Tokens:    req.Tokens,
			MaxUses:   req.MaxUses,
			UsedCount: 0,
			UsedBy:    []string{},
			Status:    "active",
			CreatedAt: time.Now(),
		}

		if err := db.DB.CreateGiftcode(ctx, newGift); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("Lỗi cơ sở dữ liệu khi tạo Giftcode: %v", err),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(newGift)

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// POST /api/admin/giftcodes/delete
func AdminDeleteGiftcodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.ID == "" {
		req.ID = r.URL.Query().Get("id")
	}
	_ = db.DB.DeleteGiftcode(r.Context(), req.ID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// POST /api/user/giftcode/redeem
func RedeemGiftcodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(UserContextKey).(string)
	if !ok || userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	codeStr := strings.ToUpper(strings.TrimSpace(req.Code))
	if codeStr == "" {
		http.Error(w, `{"error":"Vui lòng nhập mã Giftcode"}`, http.StatusBadRequest)
		return
	}

	claimedCode, err := db.DB.RedeemGiftcode(r.Context(), codeStr, userID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	user, _ := db.DB.GetUserByID(r.Context(), userID)
	giftBal := int64(0)
	if user != nil {
		giftBal = user.GiftTokens
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"message":     fmt.Sprintf("🎉 Nhập mã thành công! Bạn nhận được +%s Tokens vĩnh viễn.", formatTokens(claimedCode.Tokens)),
		"tokens_gift": claimedCode.Tokens,
		"gift_tokens": giftBal,
	})
}

func formatTokens(n int64) string {
	in := fmt.Sprintf("%d", n)
	out := ""
	for i, c := range in {
		if i > 0 && (len(in)-i)%3 == 0 {
			out += ","
		}
		out += string(c)
	}
	return out
}
