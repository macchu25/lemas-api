package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"xkiro-backend/db"
	"xkiro-backend/handlers"
)

func main() {
	// Startup Security Configuration Checks
	if os.Getenv("JWT_SECRET") == "" {
		log.Println("[Security Notice] ⚠️ JWT_SECRET environment variable is not configured. An ephemeral CSPRNG secret is active in RAM.")
	}
	if os.Getenv("ADMIN_USERNAME") == "" || os.Getenv("ADMIN_PASSWORD") == "" {
		log.Println("[Security Notice] ⚠️ ADMIN_USERNAME or ADMIN_PASSWORD environment variable is not configured. Admin portal login is locked until configured.")
	}

	// Initialize Database (MongoDB + auto-fallback)
	db.InitDB()

	mux := http.NewServeMux()

	// Public Routes
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"healthy","service":"xkiro-gateway","version":"1.0.0"}`)
	})

	// Auth & User
	mux.HandleFunc("/api/auth/register", handlers.RegisterHandler)
	mux.HandleFunc("/api/auth/login", handlers.LoginHandler)
	mux.HandleFunc("/api/auth/oauth", handlers.OAuthHandler)
	mux.HandleFunc("/api/auth/me", handlers.AuthMiddleware(handlers.GetMeHandler))
	mux.HandleFunc("/api/user/topup", handlers.AuthMiddleware(handlers.TopupHandler))

	// API Keys Management
	mux.HandleFunc("/api/keys", handlers.AuthMiddleware(handlers.ApiKeysHandler))
	mux.HandleFunc("/api/keys/revoke/", handlers.AuthMiddleware(handlers.RevokeApiKeyHandler))
	mux.HandleFunc("/api/usage", handlers.AuthMiddleware(handlers.UsageLogsHandler))

	// Public Catalog & Info
	mux.HandleFunc("/api/models", handlers.ModelsHandler)
	mux.HandleFunc("/api/pricing", handlers.PricingTiersHandler)
	mux.HandleFunc("/api/deals", handlers.DealsHandler)
	mux.HandleFunc("/api/status", handlers.StatusHandler)
	mux.HandleFunc("/api/contact", handlers.ContactHandler)
	mux.HandleFunc("/api/tts", handlers.TTSHandler)

	// AI Gateway Endpoints (OpenAI & Anthropic Compatible)
	mux.HandleFunc("/v1/chat/completions", handlers.ChatCompletionsHandler)
	mux.HandleFunc("/v1/messages", handlers.MessagesHandler)
	mux.HandleFunc("/api/rotator/stats", handlers.RotatorStatsHandler)

	// Admin Portal Endpoints (Protected with AdminAuthMiddleware)
	mux.HandleFunc("/api/admin/login", handlers.AdminLoginHandler)
	mux.HandleFunc("/api/admin/overview", handlers.AdminAuthMiddleware(handlers.AdminOverviewHandler))
	mux.HandleFunc("/api/admin/users", handlers.AdminAuthMiddleware(handlers.AdminUsersHandler))
	mux.HandleFunc("/api/admin/users/adjust", handlers.AdminAuthMiddleware(handlers.AdminAdjustUserHandler))
	mux.HandleFunc("/api/admin/giftcodes", handlers.AdminAuthMiddleware(handlers.AdminGiftcodesHandler))
	mux.HandleFunc("/api/admin/giftcodes/delete", handlers.AdminAuthMiddleware(handlers.AdminDeleteGiftcodeHandler))

	// User Giftcode Redemption & Daily Image Quota
	mux.HandleFunc("/api/user/giftcode/redeem", handlers.AuthMiddleware(handlers.RedeemGiftcodeHandler))
	mux.HandleFunc("/api/user/image/quota", handlers.AuthMiddleware(handlers.ImageQuotaHandler))
	mux.HandleFunc("/api/user/image/consume", handlers.AuthMiddleware(handlers.ImageConsumeHandler))
	mux.HandleFunc("/api/user/art-qr/jobs", handlers.AuthMiddleware(handlers.ArtQRJobsHandler))
	mux.HandleFunc("/api/user/art-qr/status", handlers.AuthMiddleware(handlers.ArtQRStatusHandler))

	// Legacy Art QR handler (Graceful JSON response for cached browser clients)
	legacyArtQRHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "client_generated",
			"message": "Art QR generation is processed via local Canvas & Puter/FLUX client",
		})
	}
	mux.HandleFunc("/user/art-qr/jobs", legacyArtQRHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	handlerWithCORS := handlers.EnableCORS(mux)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handlerWithCORS,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB header limit
	}

	log.Printf("=====================================================")
	log.Printf("🚀 Lemas.AI Backend & AI Gateway running on port %s", port)
	log.Printf("⚡ Gateway Endpoints:")
	log.Printf("   - OpenAI SDK:    http://localhost:%s/v1/chat/completions", port)
	log.Printf("🔑 Key Auth:        Header: Authorization: Bearer lemas_sk_live_... (or x-api-key)")
	log.Printf("=====================================================")

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
}
