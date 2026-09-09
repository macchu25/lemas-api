package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"xkiro-backend/db"
	"xkiro-backend/models"

	"github.com/google/uuid"
)

// SePayWebhookPayload defines the JSON body sent by SePay when a bank transaction occurs
type SePayWebhookPayload struct {
	ID              int64       `json:"id"`
	Gateway         string      `json:"gateway"`
	TransactionDate string      `json:"transactionDate"`
	AccountNumber   string      `json:"accountNumber"`
	Code            interface{} `json:"code"`
	Content         string      `json:"content"`
	TransferType    string      `json:"transferType"` // "in" or "out"
	TransferAmount  float64     `json:"transferAmount"`
	Accumulated     float64     `json:"accumulated"`
	SubAccount      interface{} `json:"subAccount"`
	ReferenceCode   string      `json:"referenceCode"`
	Description     string      `json:"description"`
}

// SePayWebhookHandler handles incoming automated payment webhooks from SePay.vn
// Endpoint: POST /api/payment/sepay/webhook
func SePayWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// 1. Security Check: Validate SePay API Token / Secret
	expectedApiKey := strings.TrimSpace(os.Getenv("SEPAY_API_KEY"))
	if expectedApiKey != "" {
		authHeader := r.Header.Get("Authorization")
		apiKeyHeader := r.Header.Get("X-Api-Key")
		tokenParam := r.URL.Query().Get("token")

		isValid := false
		if strings.HasPrefix(authHeader, "Apikey ") && strings.TrimPrefix(authHeader, "Apikey ") == expectedApiKey {
			isValid = true
		} else if strings.HasPrefix(authHeader, "Bearer ") && strings.TrimPrefix(authHeader, "Bearer ") == expectedApiKey {
			isValid = true
		} else if apiKeyHeader == expectedApiKey || tokenParam == expectedApiKey {
			isValid = true
		}

		if !isValid {
			log.Printf("[SePay Webhook] ⚠️ Unauthorized webhook attempt from IP: %s", r.RemoteAddr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Unauthorized SePay Webhook signature",
			})
			return
		}
	}

	// 2. Decode incoming SePay transaction payload
	var payload SePayWebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("[SePay Webhook] ❌ Error parsing JSON payload: %v", err)
		http.Error(w, `{"error":"Invalid JSON payload"}`, http.StatusBadRequest)
		return
	}

	log.Printf("[SePay Webhook] 📥 Received: ID=%d, Amount=%.0f VND, Type=%s, Content='%s'",
		payload.ID, payload.TransferAmount, payload.TransferType, payload.Content)

	// Only process incoming money ("in") with positive amount
	if strings.ToLower(payload.TransferType) != "in" || payload.TransferAmount <= 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Ignored outgoing or zero-amount transaction",
		})
		return
	}

	// 3. Extract User Identification from Transfer Memo
	// Syntax examples: "LEMAS 99A1BC", "LEMAS USER123456", "LEMAS-99A1BC"
	memo := strings.ToUpper(payload.Content)
	var matchedUserID string
	var matchedUser *models.User

	// Regex to extract code after "LEMAS"
	re := regexp.MustCompile(`LEMAS[_\s\-]?([A-Z0-9]+)`)
	matches := re.FindStringSubmatch(memo)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if len(matches) > 1 {
		userCode := strings.TrimSpace(matches[1])

		// Search all users in DB to find matching ID or ID suffix
		allUsers, err := db.DB.GetAllUsers(ctx)
		if err == nil {
			for _, u := range allUsers {
				cleanID := strings.ToUpper(strings.ReplaceAll(u.ID, "user-", ""))
				if strings.HasSuffix(cleanID, userCode) || strings.EqualFold(u.ID, userCode) || strings.EqualFold(u.Email, userCode) {
					userCopy := u
					matchedUser = &userCopy
					matchedUserID = u.ID
					break
				}
			}
		}
	}

	// If no match found by regex, fallback to search by exact token in content
	if matchedUser == nil {
		allUsers, err := db.DB.GetAllUsers(ctx)
		if err == nil {
			for _, u := range allUsers {
				cleanSuffix := strings.ToUpper(strings.ReplaceAll(u.ID, "user-", ""))
				if len(cleanSuffix) >= 6 {
					cleanSuffix = cleanSuffix[len(cleanSuffix)-6:]
				}
				if strings.Contains(memo, cleanSuffix) {
					userCopy := u
					matchedUser = &userCopy
					matchedUserID = u.ID
					break
				}
			}
		}
	}

	if matchedUser == nil {
		log.Printf("[SePay Webhook] ⚠️ Could not match user for memo: '%s'", payload.Content)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("Transaction received but no matching user found for content '%s'", payload.Content),
		})
		return
	}

	// 4. Calculate USD conversion (Exchange rate: 25,400 VND / 1 USD)
	exchangeRate := 25400.0
	amountUSD := payload.TransferAmount / exchangeRate
	amountUSD = float64(int(amountUSD*100)) / 100.0 // Round to 2 decimals

	// Bonus tokens (e.g. 1000 tokens per $1 USD)
	tokensAwarded := int64(amountUSD * 1000)

	// 5. Update User Balance & Plan in Database
	matchedUser.Balance += amountUSD
	matchedUser.Tokens += tokensAwarded
	matchedUser.UpdatedAt = time.Now()

	// If topped up >= $10, automatically upgrade to Pro if currently free
	if matchedUser.Balance >= 10.0 && (matchedUser.Plan == "" || matchedUser.Plan == "free") {
		matchedUser.Plan = "pro"
	}

	if err := db.DB.UpdateUser(ctx, matchedUser); err != nil {
		log.Printf("[SePay Webhook] ❌ Error updating user balance in DB: %v", err)
		http.Error(w, `{"error":"Failed to credit user balance"}`, http.StatusInternalServerError)
		return
	}

	// 6. Record Topup Transaction in Database
	tx := &models.TopupTransaction{
		ID:        "topup-" + uuid.New().String()[:8],
		UserID:    matchedUserID,
		AmountUSD: amountUSD,
		AmountVND: int64(payload.TransferAmount),
		Method:    "SePay VietQR 24/7",
		BankCode:  payload.Gateway,
		Memo:      payload.Content,
		Status:    "completed",
		CreatedAt: time.Now(),
	}
	_ = db.DB.CreateTopupTransaction(ctx, tx)

	log.Printf("[SePay Webhook] ✅ CREDITED: +$%.2f USD (+%d tokens) to User '%s' (%s). New Balance: $%.2f",
		amountUSD, tokensAwarded, matchedUser.Name, matchedUser.Email, matchedUser.Balance)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"message":      "Payment verified and credited successfully",
		"user_id":      matchedUserID,
		"amount_usd":   amountUSD,
		"amount_vnd":   payload.TransferAmount,
		"new_balance":  matchedUser.Balance,
	})
}
