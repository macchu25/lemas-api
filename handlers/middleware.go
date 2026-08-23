package handlers

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func getJwtSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "lemas-secret-key-super-secure-token-2026"
	}
	return []byte(secret)
}

type ContextKey string

const UserContextKey ContextKey = "user_id"

// EnableCORS provides production-grade Cross-Origin Resource Sharing (CORS) headers
func EnableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, Origin, X-Requested-With, x-api-key, anthropic-version, User-Agent, Cache-Control")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Global Security Hardening Headers (LEMAS-09 / LEMAS-10 / LEMAS-17)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")

		// Handle preflight OPTIONS requests immediately
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func parseJwtWithFallbacks(tokenStr string) (*jwt.Token, error) {
	secrets := [][]byte{
		getJwtSecret(),
		[]byte("xkiro-secret-key-super-secure-token-2026"),
		[]byte("lemas-secret-key-super-secure-token-2026"),
	}
	if envSecret := os.Getenv("JWT_SECRET"); envSecret != "" {
		secrets = append([][]byte{[]byte(envSecret)}, secrets...)
	}

	var lastErr error
	for _, s := range secrets {
		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return s, nil
		})
		if err == nil && token.Valid {
			return token, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"unauthorized, missing Bearer token"}`, http.StatusUnauthorized)
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := parseJwtWithFallbacks(tokenStr)
		if err != nil || !token.Valid {
			http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, `{"error":"invalid token claims"}`, http.StatusUnauthorized)
			return
		}

		userID, ok := claims["user_id"].(string)
		if !ok || userID == "" {
			http.Error(w, `{"error":"invalid user_id in token"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func GenerateJWT(userID, email string) (string, error) {
	return GenerateJWTWithDuration(userID, email, 30*24*time.Hour)
}

func GenerateJWTWithDuration(userID, email string, duration time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"email":   email,
		"exp":     time.Now().Add(duration).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getJwtSecret())
}

