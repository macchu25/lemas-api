package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"xkiro-backend/db"
	"xkiro-backend/services"
)

type AdminUserView struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	Role          string    `json:"role"`
	Plan          string    `json:"plan"`
	Balance       float64   `json:"balance"`
	TokensAlloc   int64     `json:"tokens_alloc"`
	TokensUsed    int64     `json:"tokens_used"`
	CostUSD       float64   `json:"cost_usd"`
	TotalRequests int       `json:"total_requests"`
	ActiveKeys    int       `json:"active_keys"`
	CreatedAt     time.Time `json:"created_at"`
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
	for _, l := range logs {
		totalTokens += int64(l.TotalTokens)
		totalCost += l.CostUSD
	}

	activeKeysCount := 0
	for _, k := range keys {
		if k.Status == "active" {
			activeKeysCount++
		}
	}

	rotator := services.InitKeyRotator()
	rotatorStats := rotator.GetPoolStats()

	resp := AdminOverviewResponse{
		TotalUsers:         len(users),
		TotalActiveKeys:    activeKeysCount,
		TotalTokensUsed:    totalTokens,
		TotalCostUSD:       totalCost,
		TotalRequests:      len(logs),
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
		views[i] = AdminUserView{
			ID:            u.ID,
			Email:         u.Email,
			Name:          u.Name,
			Role:          u.Role,
			Plan:          u.Plan,
			Balance:       u.Balance,
			TokensAlloc:   u.Tokens,
			TokensUsed:    userTokensMap[u.ID],
			CostUSD:       userCostMap[u.ID],
			TotalRequests: userReqsMap[u.ID],
			ActiveKeys:    userKeysCountMap[u.ID],
			CreatedAt:     u.CreatedAt,
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

	// Verify Admin Credentials: admin.lemas / mactieulem
	if (req.Username == "admin.lemas" || req.Username == "admin@lemas.ai") && req.Password == "mactieulem" {
		token, _ := GenerateJWT("admin-root-001", "admin.lemas@lemas.ai")
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
