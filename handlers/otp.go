package handlers

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"xkiro-backend/db"
	"xkiro-backend/models"
	"xkiro-backend/services"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type PendingRegistration struct {
	Name           string
	Email          string
	HashedPassword string
	OTPCode        string
	ExpiresAt      time.Time
	Attempts       int
}

var (
	otpStore = make(map[string]*PendingRegistration)
	otpMu    sync.RWMutex
)

type SendOTPRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type VerifyOTPRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

type ResendOTPRequest struct {
	Email string `json:"email"`
}

func generate6DigitOTP() string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	}
	return fmt.Sprintf("%06d", n.Int64()+100000)
}

// SendOTPHandler validates input, checks for existing user, stores pending registration, and sends OTP email
func SendOTPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req SendOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Dữ liệu yêu cầu không hợp lệ"})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	name := strings.TrimSpace(req.Name)
	password := req.Password

	if email == "" || !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Địa chỉ email không hợp lệ"})
		return
	}

	if len(password) < 6 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Mật khẩu phải có ít nhất 6 ký tự"})
		return
	}

	// Check if user already exists
	existingUser, err := db.DB.GetUserByEmail(r.Context(), email)
	if err == nil && existingUser != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Email này đã được đăng ký tài khoản trong hệ thống"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Không thể xử lý mật khẩu"})
		return
	}

	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	otpCode := generate6DigitOTP()

	otpMu.Lock()
	otpStore[email] = &PendingRegistration{
		Name:           name,
		Email:          email,
		HashedPassword: string(hashedPassword),
		OTPCode:        otpCode,
		ExpiresAt:      time.Now().Add(10 * time.Minute),
		Attempts:       0,
	}
	otpMu.Unlock()

	// Send OTP email
	go func() {
		if services.Email != nil {
			_ = services.Email.SendVerificationOTP(email, name, otpCode)
		} else {
			log.Printf("[OTP Fallback] Email: %s, OTP: %s", email, otpCode)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Mã xác thực đã được gửi tới email của bạn.",
		"email":   email,
	})
}

// VerifyOTPHandler checks the OTP code, creates the user account in DB, and issues JWT
func VerifyOTPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req VerifyOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Dữ liệu yêu cầu không hợp lệ"})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	otpInput := strings.TrimSpace(req.OTP)

	otpMu.Lock()
	pending, exists := otpStore[email]
	if !exists {
		otpMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Không tìm thấy yêu cầu đăng ký hoặc mã đã hết hạn. Vui lòng thử lại."})
		return
	}

	if time.Now().After(pending.ExpiresAt) {
		delete(otpStore, email)
		otpMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Mã xác thực đã hết hạn (quá 10 phút). Vui lòng gửi lại mã mới."})
		return
	}

	pending.Attempts++
	if pending.Attempts > 5 {
		delete(otpStore, email)
		otpMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Bạn đã nhập sai mã quá 5 lần. Vui lòng gửi lại mã mới."})
		return
	}

	if pending.OTPCode != otpInput {
		otpMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Mã xác thực không chính xác. Vui lòng kiểm tra lại email."})
		return
	}

	// OTP is valid! Remove from pending store and create user
	savedName := pending.Name
	savedHashedPassword := pending.HashedPassword
	delete(otpStore, email)
	otpMu.Unlock()

	today := time.Now().Format("2006-01-02")
	userID := "user-" + uuid.New().String()
	user := &models.User{
		ID:                 userID,
		Email:              email,
		Password:           savedHashedPassword,
		Name:               savedName,
		Role:               "user",
		Balance:            0.00,
		Tokens:             0,
		GiftTokens:         0,
		Plan:               "free",
		DailyTokensUsed:    0,
		DailyTokensLimit:   1000,
		LastTokenResetDate: today,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := db.DB.CreateUser(r.Context(), user); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Tài khoản với email này đã tồn tại"})
		return
	}

	token, err := GenerateJWT(user.ID, user.Email)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Không thể tạo phiên đăng nhập"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AuthResponse{
		Token: token,
		User:  user,
	})
}

// ResendOTPHandler generates a fresh OTP and resends email
func ResendOTPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req ResendOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Dữ liệu yêu cầu không hợp lệ"})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))

	otpMu.Lock()
	pending, exists := otpStore[email]
	if !exists {
		otpMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Không tìm thấy yêu cầu đăng ký. Vui lòng quay lại bước đăng ký."})
		return
	}

	newOTP := generate6DigitOTP()
	pending.OTPCode = newOTP
	pending.ExpiresAt = time.Now().Add(10 * time.Minute)
	pending.Attempts = 0
	savedName := pending.Name
	otpMu.Unlock()

	go func() {
		if services.Email != nil {
			_ = services.Email.SendVerificationOTP(email, savedName, newOTP)
		} else {
			log.Printf("[OTP Resend] Email: %s, OTP: %s", email, newOTP)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Mã xác thực mới đã được gửi tới email của bạn.",
	})
}
