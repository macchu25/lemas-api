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

type CreateKeyRequest struct {
	Name        string   `json:"name"`
	SpendLimit  float64  `json:"spend_limit"`
	Permissions []string `json:"permissions"`
}

func ApiKeysHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(UserContextKey).(string)
	if !ok || userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		keys, err := db.DB.GetApiKeysByUser(r.Context(), userID)
		if err != nil {
			http.Error(w, `{"error":"failed to get keys"}`, http.StatusInternalServerError)
			return
		}
		if keys == nil {
			keys = []models.ApiKey{}
		}

		// Security hardening: Mask key secrets in list view (LEMAS-06)
		maskedKeys := make([]models.ApiKey, len(keys))
		for i, k := range keys {
			masked := k
			if len(k.Key) > 12 {
				prefix := k.Key[:8]
				suffix := k.Key[len(k.Key)-4:]
				masked.Key = prefix + "••••••••" + suffix
			}
			maskedKeys[i] = masked
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(maskedKeys)

	case http.MethodPost:
		// API Key creation is locked by admin policy
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Chức năng tạo API Key mới hiện đang tạm khóa. Vui lòng sử dụng API Key mặc định được cấp sẵn trong tài khoản của bạn.",
		})
		return

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func RevokeApiKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(UserContextKey).(string)
	if !ok || userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	keyID := strings.TrimPrefix(r.URL.Path, "/api/keys/revoke/")
	if keyID == "" || keyID == r.URL.Path {
		keyID = r.URL.Query().Get("id")
	}

	if keyID == "" {
		http.Error(w, `{"error":"key id is required"}`, http.StatusBadRequest)
		return
	}

	if err := db.DB.RevokeApiKey(r.Context(), keyID, userID); err != nil {
		http.Error(w, `{"error":"failed to revoke key"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "API key revoked successfully",
	})
}

// User Usage & Real Analytics & Finance Ledger: GET /api/usage
func UsageLogsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(UserContextKey).(string)
	if !ok || userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	user, _ := db.DB.GetUserByID(ctx, userID)
	logs, err := db.DB.GetUsageLogsByUser(ctx, userID)
	if err != nil {
		logs = []models.UsageLog{}
	}

	topupTxs, err := db.DB.GetTopupTransactionsByUser(ctx, userID)
	if err != nil {
		topupTxs = []models.TopupTransaction{}
	}

	var totalDeposited float64
	for _, tx := range topupTxs {
		if tx.Status == "completed" {
			totalDeposited += tx.AmountUSD
		}
	}

	now := time.Now()
	thirtyDaysAgo := now.AddDate(0, 0, -30)
	sevenDaysAgo := now.AddDate(0, 0, -7)

	var totalSpend30d float64
	var totalTokens30d int64
	var totalRequests30d int
	var totalSpend7d float64

	dailyMap := make(map[string]float64)
	dailyTokenMap := make(map[string]int64)
	dailyReqMap := make(map[string]int)

	for _, l := range logs {
		if l.Timestamp.After(thirtyDaysAgo) {
			totalSpend30d += l.CostUSD
			totalTokens30d += int64(l.TotalTokens)
			totalRequests30d++

			dateKey := l.Timestamp.Format("01/02")
			dailyMap[dateKey] += l.CostUSD
			dailyTokenMap[dateKey] += int64(l.TotalTokens)
			dailyReqMap[dateKey]++
		}
		if l.Timestamp.After(sevenDaysAgo) {
			totalSpend7d += l.CostUSD
		}
	}

	avgDailySpend7d := totalSpend7d / 7.0
	daysUsed30d := len(dailyMap)

	var maxDailySpend30d float64
	for _, v := range dailyMap {
		if v > maxDailySpend30d {
			maxDailySpend30d = v
		}
	}

	// Generate 15 daily buckets for chart
	type DailyPoint struct {
		Date     string  `json:"date"`
		Cost     float64 `json:"cost"`
		Tokens   int64   `json:"tokens"`
		Requests int     `json:"requests"`
		Height   int     `json:"height"`
	}

	dailyChart := make([]DailyPoint, 15)
	for i := 14; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		dateKey := day.Format("01/02")
		c := dailyMap[dateKey]
		tok := dailyTokenMap[dateKey]
		reqs := dailyReqMap[dateKey]

		h := 10
		if maxDailySpend30d > 0 && c > 0 {
			h = int((c / maxDailySpend30d) * 85)
			if h < 15 {
				h = 15
			}
		} else if reqs > 0 {
			h = 35
		}

		dailyChart[14-i] = DailyPoint{
			Date:     dateKey,
			Cost:     c,
			Tokens:   tok,
			Requests: reqs,
			Height:   h,
		}
	}

	// Sort logs reverse for recent requests table
	recentReqs := make([]map[string]interface{}, 0, len(logs))
	for i := len(logs) - 1; i >= 0; i-- {
		l := logs[i]
		recentReqs = append(recentReqs, map[string]interface{}{
			"id":            l.ID,
			"model":         l.Model,
			"prompt_tokens": l.PromptTokens,
			"comp_tokens":   l.CompTokens,
			"total_tokens":  l.TotalTokens,
			"cost_usd":      l.CostUSD,
			"latency_ms":    l.LatencyMs,
			"status":        "200 OK",
			"time_ago":      timeAgo(l.Timestamp),
			"timestamp":     l.Timestamp.Format("2006-01-02 15:04:05"),
		})
		if len(recentReqs) >= 20 {
			break
		}
	}

	// Sort topups reverse for topup history table
	recentTopups := make([]map[string]interface{}, 0, len(topupTxs))
	for i := len(topupTxs) - 1; i >= 0; i-- {
		tx := topupTxs[i]
		recentTopups = append(recentTopups, map[string]interface{}{
			"id":         tx.ID,
			"amount_usd": tx.AmountUSD,
			"amount_vnd": tx.AmountVND,
			"method":     tx.Method,
			"bank_code":  tx.BankCode,
			"memo":       tx.Memo,
			"status":     tx.Status,
			"time_ago":   timeAgo(tx.CreatedAt),
			"created_at": tx.CreatedAt.Format("2006-01-02 15:04:05"),
		})
		if len(recentTopups) >= 20 {
			break
		}
	}

	currentBalance := 0.0
	if user != nil {
		currentBalance = user.Balance
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"balance":              currentBalance,
		"total_deposited":      totalDeposited,
		"total_spend_30d":      totalSpend30d,
		"total_tokens_30d":     totalTokens30d,
		"total_requests_30d":   totalRequests30d,
		"avg_daily_spend_7d":   avgDailySpend7d,
		"max_daily_spend_30d":  maxDailySpend30d,
		"days_used_30d":        daysUsed30d,
		"daily_chart":          dailyChart,
		"recent_requests":      recentReqs,
		"topup_history":        recentTopups,
	})
}

func timeAgo(t time.Time) string {
	diff := time.Since(t)
	if diff < time.Minute {
		return "Vừa xong"
	} else if diff < time.Hour {
		return fmt.Sprintf("%d phút trước", int(diff.Minutes()))
	} else if diff < 24*time.Hour {
		return fmt.Sprintf("%d giờ trước", int(diff.Hours()))
	}
	return fmt.Sprintf("%d ngày trước", int(diff.Hours()/24))
}
