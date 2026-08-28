package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type ContextKey string

const UserContextKey ContextKey = "user_id"

var allowedOrigins = map[string]bool{
	"https://lemas.io.vn":          true,
	"https://www.lemas.io.vn":      true,
	"https://api.lemas.io.vn":      true,
	"https://lemas-two.vercel.app": true,
	"http://localhost:3000":        true,
	"http://localhost:8080":        true,
	"http://127.0.0.1:3000":        true,
	"http://127.0.0.1:8080":        true,
}

func isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	if allowedOrigins[origin] {
		return true
	}
	if strings.HasSuffix(origin, ".lemas.io.vn") || strings.HasSuffix(origin, "-macchu25s-projects.vercel.app") {
		return true
	}
	return false
}

// EnableCORS provides production-grade Cross-Origin Resource Sharing (CORS) headers
func EnableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, Origin, X-Requested-With, x-api-key, anthropic-version, User-Agent, Cache-Control")
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")

		// Global Security Hardening Headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Handle preflight OPTIONS requests immediately with 200 OK
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

var (
	dynamicJwtSecretOnce sync.Once
	dynamicJwtSecret     []byte
)

func getJwtSecret() []byte {
	// Strict Security Enforcement (LEMAS-CRIT-03):
	// Read from environment variable JWT_SECRET.
	// If missing, generate an ephemeral 64-byte CSPRNG random secret in RAM.
	// NEVER hardcode fallback secrets in repository.
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		return []byte(secret)
	}

	dynamicJwtSecretOnce.Do(func() {
		buf := make([]byte, 64)
		if _, err := rand.Read(buf); err != nil {
			log.Fatalf("[Security Fatal] Failed to generate secure CSPRNG JWT secret: %v", err)
		}
		dynamicJwtSecret = []byte(hex.EncodeToString(buf))
		log.Println("[Security Warning] ⚠️ JWT_SECRET environment variable is not set. Generated ephemeral CSPRNG secret in memory.")
	})
	return dynamicJwtSecret
}

func parseJwtToken(tokenStr string) (*jwt.Token, error) {
	return jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		// Strict algorithm check to prevent algorithm confusion attacks (e.g. none attack)
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return getJwtSecret(), nil
	})
}

func parseJwtWithFallbacks(tokenStr string) (*jwt.Token, error) {
	return parseJwtToken(tokenStr)
}

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized, missing Bearer token"})
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := parseJwtToken(tokenStr)
		if err != nil || !token.Valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid or expired token"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid token claims"})
			return
		}

		userID, ok := claims["user_id"].(string)
		if !ok || userID == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid user_id in token"})
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// AdminAuthMiddleware strictly enforces that the caller possesses a valid admin-role JWT
func AdminAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized, admin authentication required"})
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := parseJwtToken(tokenStr)
		if err != nil || !token.Valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid or expired admin token"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid token claims"})
			return
		}

		role, _ := claims["role"].(string)
		if role != "admin" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden: requires administrator privileges"})
			return
		}

		userID, _ := claims["user_id"].(string)
		ctx := context.WithValue(r.Context(), UserContextKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func GenerateJWT(userID, email string) (string, error) {
	return GenerateJWTWithDuration(userID, email, "user", 30*24*time.Hour)
}

func GenerateAdminJWT(adminID, email string) (string, error) {
	return GenerateJWTWithDuration(adminID, email, "admin", 24*time.Hour)
}

func GenerateJWTWithDuration(userID, email, role string, duration time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"email":   email,
		"role":    role,
		"exp":     time.Now().Add(duration).Unix(),
		"iat":     time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getJwtSecret())
}
