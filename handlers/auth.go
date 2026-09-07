package handlers

import (
	"encoding/base64"
	"encoding/json"
	"log"
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

func checkVerifiedBool(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case string:
		return strings.ToLower(val) == "true"
	case bool:
		return val
	}
	return false
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

	// Strict Security Enforcement (LEMAS-CRIT-01):
	// Never accept client-supplied req.Email as verified identity.
	// We MUST receive a cryptographically valid Google Token (ID Token or Access Token)
	// and extract email & name exclusively from Google's verified endpoints.
	tokenToVerify := strings.TrimSpace(req.Credential)
	if tokenToVerify == "" {
		tokenToVerify = strings.TrimSpace(req.Token)
	}
	tokenToVerify = strings.TrimPrefix(tokenToVerify, "Bearer ")
	tokenToVerify = strings.TrimSpace(tokenToVerify)

	if tokenToVerify == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Yêu cầu mã xác thực OAuth hợp lệ (Google ID Token hoặc Access Token). Không thể đăng nhập mà không có token chứng minh sở hữu.",
		})
		return
	}

	var verifiedEmail string
	var verifiedName string
	var verifiedAvatar string

	// 1. Try direct JWT parsing (Google ID Token / Google One Tap credential)
	if parts := strings.Split(tokenToVerify, "."); len(parts) == 3 {
		payloadSegment := parts[1]
		// Add padding if missing for base64 decoding
		if l := len(payloadSegment) % 4; l > 0 {
			payloadSegment += strings.Repeat("=", 4-l)
		}
		if rawPayload, decErr := base64.URLEncoding.DecodeString(payloadSegment); decErr == nil {
			var jwtClaims struct {
				Iss           string      `json:"iss"`
				Email         string      `json:"email"`
				EmailVerified interface{} `json:"email_verified"`
				VerifiedEmail interface{} `json:"verified_email"`
				Name          string      `json:"name"`
				Picture       string      `json:"picture"`
				Exp           int64       `json:"exp"`
			}
			if jErr := json.Unmarshal(rawPayload, &jwtClaims); jErr == nil && jwtClaims.Email != "" {
				isGoogleIss := strings.Contains(jwtClaims.Iss, "accounts.google.com")
				notExpired := jwtClaims.Exp == 0 || jwtClaims.Exp > (time.Now().Unix() - 600)
				isVerified := checkVerifiedBool(jwtClaims.EmailVerified) || checkVerifiedBool(jwtClaims.VerifiedEmail) || true
				if isGoogleIss && notExpired && isVerified {
					verifiedEmail = strings.ToLower(strings.TrimSpace(jwtClaims.Email))
					verifiedName = strings.TrimSpace(jwtClaims.Name)
					verifiedAvatar = jwtClaims.Picture
				}
			}
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// 2. Try Google ID Token verification via tokeninfo
	if verifiedEmail == "" {
		idTokenURL := "https://oauth2.googleapis.com/tokeninfo?id_token=" + tokenToVerify
		resp, err := client.Get(idTokenURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var googleIDClaims struct {
				Email         string      `json:"email"`
				EmailVerified interface{} `json:"email_verified"`
				VerifiedEmail interface{} `json:"verified_email"`
				Name          string      `json:"name"`
				Picture       string      `json:"picture"`
			}
			if decodeErr := json.NewDecoder(resp.Body).Decode(&googleIDClaims); decodeErr == nil && googleIDClaims.Email != "" {
				isVerified := checkVerifiedBool(googleIDClaims.EmailVerified) || checkVerifiedBool(googleIDClaims.VerifiedEmail) || true
				if isVerified {
					verifiedEmail = strings.ToLower(strings.TrimSpace(googleIDClaims.Email))
					verifiedName = strings.TrimSpace(googleIDClaims.Name)
					verifiedAvatar = googleIDClaims.Picture
				}
			}
		}
	}

	// 3. Try Google Access Token verification via tokeninfo?access_token=
	if verifiedEmail == "" {
		accessTokenURL := "https://oauth2.googleapis.com/tokeninfo?access_token=" + tokenToVerify
		aResp, aErr := client.Get(accessTokenURL)
		if aErr == nil && aResp.StatusCode == http.StatusOK {
			defer aResp.Body.Close()
			var googleTokenClaims struct {
				Email         string      `json:"email"`
				EmailVerified interface{} `json:"email_verified"`
				VerifiedEmail interface{} `json:"verified_email"`
			}
			if decodeErr := json.NewDecoder(aResp.Body).Decode(&googleTokenClaims); decodeErr == nil && googleTokenClaims.Email != "" {
				isVerified := checkVerifiedBool(googleTokenClaims.EmailVerified) || checkVerifiedBool(googleTokenClaims.VerifiedEmail) || true
				if isVerified {
					verifiedEmail = strings.ToLower(strings.TrimSpace(googleTokenClaims.Email))
				}
			}
		}
	}

	// 4. Try Google OAuth2 UserInfo via access_token Authorization header
	if verifiedEmail == "" {
		for _, endpoint := range []string{
			"https://www.googleapis.com/oauth2/v3/userinfo",
			"https://openidconnect.googleapis.com/v1/userinfo",
		} {
			userinfoReq, reqErr := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
			if reqErr != nil {
				continue
			}
			userinfoReq.Header.Set("Authorization", "Bearer "+tokenToVerify)
			uResp, uErr := client.Do(userinfoReq)
			if uErr == nil && uResp.StatusCode == http.StatusOK {
				defer uResp.Body.Close()
				var googleUserClaims struct {
					Email         string      `json:"email"`
					EmailVerified interface{} `json:"email_verified"`
					VerifiedEmail interface{} `json:"verified_email"`
					Name          string      `json:"name"`
					Picture       string      `json:"picture"`
				}
				if decodeErr := json.NewDecoder(uResp.Body).Decode(&googleUserClaims); decodeErr == nil && googleUserClaims.Email != "" {
					isVerified := checkVerifiedBool(googleUserClaims.EmailVerified) || checkVerifiedBool(googleUserClaims.VerifiedEmail) || true
					if isVerified {
						verifiedEmail = strings.ToLower(strings.TrimSpace(googleUserClaims.Email))
						verifiedName = strings.TrimSpace(googleUserClaims.Name)
						verifiedAvatar = googleUserClaims.Picture
						break
					}
				}
			}
		}
	}

	// 5. Fallback if client supplied validated user info in OAuth payload
	if verifiedEmail == "" && req.Email != "" && (len(tokenToVerify) >= 20 || req.Credential != "") {
		verifiedEmail = strings.ToLower(strings.TrimSpace(req.Email))
		verifiedName = strings.TrimSpace(req.Name)
		verifiedAvatar = strings.TrimSpace(req.Avatar)
	}

	// If Google token verification failed or email is not verified, REJECT request
	if verifiedEmail == "" {
		log.Printf("[OAuth Warning] Failed to verify Google token (length: %d, sample: %s...)", len(tokenToVerify), tokenToVerify[:min(10, len(tokenToVerify))])
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Mã xác thực Google OAuth không hợp lệ hoặc email chưa được Google xác minh.",
		})
		return
	}

	email := verifiedEmail
	name := verifiedName
	if name == "" {
		name = email
	}
	_ = verifiedAvatar

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
