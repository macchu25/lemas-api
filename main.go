package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"xkiro-backend/db"
	"xkiro-backend/handlers"
)

func main() {
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

	// Admin Portal Endpoints
	mux.HandleFunc("/api/admin/overview", handlers.AdminOverviewHandler)
	mux.HandleFunc("/api/admin/users", handlers.AdminUsersHandler)
	mux.HandleFunc("/api/admin/users/adjust", handlers.AdminAdjustUserHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	handlerWithCORS := handlers.EnableCORS(mux)

	log.Printf("=====================================================")
	log.Printf("🚀 Lemas.AI Backend & AI Gateway running on port %s", port)
	log.Printf("⚡ Gateway Endpoints:")
	log.Printf("   - OpenAI SDK:    http://localhost:%s/v1/chat/completions", port)
	log.Printf("   - Anthropic SDK: http://localhost:%s/v1/messages", port)
	log.Printf("🔑 Demo API Key:    lemas-live-demo-key-88888888")
	log.Printf("👤 Demo Account:    demo@lemas.ai / password123")
	log.Printf("=====================================================")

	if err := http.ListenAndServe(":"+port, handlerWithCORS); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
