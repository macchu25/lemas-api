package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"xkiro-backend/db"
	"xkiro-backend/models"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type RegisterRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string       `json:"token"`
	User  *models.User `json:"user"`
}

func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Password == "" {
		http.Error(w, `{"error":"email and password are required"}`, http.StatusBadRequest)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, `{"error":"failed to hash password"}`, http.StatusInternalServerError)
		return
	}

	userID := "user-" + uuid.New().String()
	user := &models.User{
		ID:        userID,
		Email:     req.Email,
		Password:  string(hashedPassword),
		Name:      req.Name,
		Role:      "user",
		Balance:   10.00, // Welcome $10 free credits
		Tokens:    1000000,
		Plan:      "free",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if user.Name == "" {
		user.Name = req.Email
	}

	if err := db.DB.CreateUser(r.Context(), user); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Create initial API Key for user
	apiKey := &models.ApiKey{
		ID:          "key-" + uuid.New().String(),
		UserID:      userID,
		Key:         "xk-live-" + uuid.New().String(),
		Name:        "Default Key",
		SpendLimit:  50.0,
		SpendUsed:   0.0,
		Status:      "active",
		Permissions: []string{"chat:completions", "messages", "embeddings"},
		CreatedAt:   time.Now(),
	}
	_ = db.DB.CreateApiKey(r.Context(), apiKey)

	// Create initial Welcome Credit transaction
	initTx := &models.TopupTransaction{
		ID:        "tx-welcome-" + uuid.New().String()[:6],
		UserID:    userID,
		AmountUSD: 10.00,
		AmountVND: 254000,
		Method:    "Welcome Grant",
		BankCode:  "Lemas Bonus",
		Memo:      "LEMAS WELCOME",
		Status:    "completed",
		CreatedAt: time.Now(),
	}
	_ = db.DB.CreateTopupTransaction(r.Context(), initTx)

	token, err := GenerateJWT(user.ID, user.Email)
	if err != nil {
		http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AuthResponse{
		Token: token,
		User:  user,
	})
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	user, err := db.DB.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid email or password"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid email or password"})
		return
	}

	token, err := GenerateJWT(user.ID, user.Email)
	if err != nil {
		http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AuthResponse{
		Token: token,
		User:  user,
	})
}

func GetMeHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(UserContextKey).(string)
	if !ok || userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	user, err := db.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(user)
}

type TopupRequest struct {
	Amount float64 `json:"amount"`
}

func TopupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(UserContextKey).(string)
	if !ok || userID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req TopupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount <= 0 {
		http.Error(w, `{"error":"invalid topup amount"}`, http.StatusBadRequest)
		return
	}

	user, err := db.DB.GetUserByID(r.Context(), userID)
	if err != nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	user.Balance += req.Amount
	user.UpdatedAt = time.Now()
	if err := db.DB.UpdateUser(r.Context(), user); err != nil {
		http.Error(w, `{"error":"failed to update balance"}`, http.StatusInternalServerError)
		return
	}

	userCode := "TOPUP88"
	if len(user.ID) >= 6 {
		userCode = strings.ToUpper(user.ID[len(user.ID)-6:])
	}

	tx := &models.TopupTransaction{
		ID:        "tx-" + uuid.New().String()[:8],
		UserID:    user.ID,
		AmountUSD: req.Amount,
		AmountVND: int64(req.Amount * 25400),
		Method:    "SePay VietQR",
		BankCode:  "MBBank",
		Memo:      "LEMAS " + userCode,
		Status:    "completed",
		CreatedAt: time.Now(),
	}
	_ = db.DB.CreateTopupTransaction(r.Context(), tx)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"balance":     user.Balance,
		"transaction": tx,
		"message":     "Nạp tiền thành công",
	})
}

