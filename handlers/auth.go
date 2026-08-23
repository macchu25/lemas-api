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
	Email      string `json:"email"`
	Password   string `json:"password"`
	RememberMe bool   `json:"remember_me"`
}

type OAuthRequest struct {
	Provider string `json:"provider"` // "google", "github"
	Email    string `json:"email,omitempty"`
	Name     string `json:"name,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
}

type AuthResponse struct {
	Token string       `json:"token"`
	User  *models.User `json:"user"`
}

func OAuthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req OAuthRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.Provider == "" {
		req.Provider = "google"
	}

	email := strings.TrimSpace(req.Email)
	if email == "" {
		if req.Provider == "github" {
			email = "github.developer@lemas.ai"
		} else {
			email = "google.user@lemas.ai"
		}
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		if req.Provider == "github" {
			name = "GitHub Developer"
		} else {
			name = "Google User"
		}
	}

	today := time.Now().Format("2006-01-02")
	user, err := db.DB.GetUserByEmail(r.Context(), email)
	if err != nil || user == nil {
		userID := "user-" + req.Provider + "-" + uuid.New().String()[:8]
		newUser := &models.User{
			ID:                 userID,
			Email:              email,
			Password:           "",
			Name:               name,
			Role:               "user",
			Balance:            0.00, // 0 USD initial balance
			Tokens:             0,
			GiftTokens:         0,
			Plan:               "free",
			DailyTokensUsed:    0,
			DailyTokensLimit:   1000,
			LastTokenResetDate: today,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}
		_ = db.DB.CreateUser(r.Context(), newUser)
		user = newUser
	} else {
		if user.LastTokenResetDate != today {
			user.DailyTokensUsed = 0
			user.LastTokenResetDate = today
			_ = db.DB.UpdateUser(r.Context(), user)
		}
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
		ID:                 userID,
		Email:              req.Email,
		Password:           string(hashedPassword),
		Name:               req.Name,
		Role:               "user",
		Balance:            0.00, // 0 USD initial balance
		Tokens:             0,
		GiftTokens:         0,
		Plan:               "free",
		DailyTokensUsed:    0,
		DailyTokensLimit:   1000, // 1000 tokens per day limit
		LastTokenResetDate: time.Now().Format("2006-01-02"),
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
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

	today := time.Now().Format("2006-01-02")
	if user.LastTokenResetDate != today {
		user.DailyTokensUsed = 0
		user.LastTokenResetDate = today
		_ = db.DB.UpdateUser(r.Context(), user)
	}
	if user.DailyTokensLimit <= 0 {
		user.DailyTokensLimit = 1000
		_ = db.DB.UpdateUser(r.Context(), user)
	}

	duration := 24 * time.Hour
	if req.RememberMe {
		duration = 30 * 24 * time.Hour // 30 days session
	}

	token, err := GenerateJWTWithDuration(user.ID, user.Email, duration)
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

	today := time.Now().Format("2006-01-02")
	if user.LastTokenResetDate != today {
		user.DailyTokensUsed = 0
		user.LastTokenResetDate = today
		_ = db.DB.UpdateUser(r.Context(), user)
	}
	if user.DailyTokensLimit <= 0 {
		user.DailyTokensLimit = 1000
		_ = db.DB.UpdateUser(r.Context(), user)
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

