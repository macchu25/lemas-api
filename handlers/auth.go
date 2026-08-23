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
	Provider   string `json:"provider"` // "google", "github"
	Credential string `json:"credential,omitempty"`
	Token      string `json:"token,omitempty"`
	Email      string `json:"email,omitempty"`
	Name       string `json:"name,omitempty"`
	Avatar     string `json:"avatar,omitempty"`
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if req.Provider == "" {
		req.Provider = "google"
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	name := strings.TrimSpace(req.Name)

	// If Google Credential / ID Token is provided, verify it directly with Google TokenInfo
	tokenToVerify := req.Credential
	if tokenToVerify == "" {
		tokenToVerify = req.Token
	}

	if req.Provider == "google" && tokenToVerify != "" {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("https://oauth2.googleapis.com/tokeninfo?id_token=" + tokenToVerify)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var googleClaims struct {
				Email         string `json:"email"`
				EmailVerified string `json:"email_verified"`
				Name          string `json:"name"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&googleClaims); err == nil && googleClaims.Email != "" {
				email = strings.ToLower(strings.TrimSpace(googleClaims.Email))
				if googleClaims.Name != "" {
					name = googleClaims.Name
				}
			}
		}
	}

	if email == "" {
		http.Error(w, `{"error":"Email xác thực OAuth không hợp lệ"}`, http.StatusBadRequest)
		return
	}

	if name == "" {
		name = "Lemas Developer"
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

	email := strings.ToLower(strings.TrimSpace(req.Email))
	password := req.Password

	if email == "" || password == "" {
		http.Error(w, `{"error":"email and password are required"}`, http.StatusBadRequest)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, `{"error":"failed to hash password"}`, http.StatusInternalServerError)
		return
	}

	userID := "user-" + uuid.New().String()
	user := &models.User{
		ID:                 userID,
		Email:              email,
		Password:           string(hashedPassword),
		Name:               strings.TrimSpace(req.Name),
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
		user.Name = email
	}

	if err := db.DB.CreateUser(r.Context(), user); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Email này đã được đăng ký trong hệ thống"})
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

	email := strings.ToLower(strings.TrimSpace(req.Email))
	user, err := db.DB.GetUserByEmail(r.Context(), email)
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

	role := user.Role
	if role == "" {
		role = "user"
	}
	token, err := GenerateJWTWithDuration(user.ID, user.Email, role, duration)
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

// TopupHandler - Direct client-side arbitrary balance addition is permanently disabled.
// Balances are credited exclusively via validated Webhooks, Giftcodes, or Admin adjustments.
func TopupHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   "Tự nạp số dư trực tiếp từ client đã bị vô hiệu hóa vì lý do an toàn tài chính. Vui lòng quét mã VietQR để thanh toán tự động qua cổng SePay hoặc nhập mã Giftcode.",
	})
}
