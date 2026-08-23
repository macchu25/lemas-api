package db

import (
	"context"
	"log"
	"time"

	"xkiro-backend/models"

	"golang.org/x/crypto/bcrypt"
)

func SeedData(store Store) {
	ctx := context.Background()

	// 1. Seed Models
	scrapedModels := []models.ModelItem{
		// Lemas Flagship Model
		{
			ID: "lemas-1.0", Name: "Lemas 1.0 (Flagship)", Provider: "Lemas.AI", ProviderIcon: "cpu",
			Category: "Next-Gen Brain", ContextLength: "128K", InputPrice: "$0.05 / 1M", OutputPrice: "$0.20 / 1M",
			OfficialInputPrice: "$0.25 / 1M", OfficialOutputPrice: "$1.00 / 1M", Discount: "-80%", IsFree: true,
			Description: "Ultra low-latency next-gen frontier model with reasoning tokens, live web ground, and high token efficiency.",
			Tags: []string{"Featured Brain", "Flagship", "Ultra Fast", "Reasoning"},
		},
		{
			ID: "deepseek/deepseek-r1", Name: "DeepSeek R1", Provider: "DeepSeek", ProviderIcon: "deepseek",
			Category: "Reasoning", ContextLength: "64K", InputPrice: "$0.14 / 1M", OutputPrice: "$0.55 / 1M",
			OfficialInputPrice: "$0.55 / 1M", OfficialOutputPrice: "$2.19 / 1M", Discount: "-74%", IsFree: true,
			Description: "Open-weights reasoning model matching o1 performance with chain-of-thought capabilities.",
			Tags: []string{"Free", "Reasoning", "Coding", "Math"},
		},
		{
			ID: "deepseek/deepseek-chat", Name: "DeepSeek V3", Provider: "DeepSeek", ProviderIcon: "deepseek",
			Category: "General", ContextLength: "64K", InputPrice: "$0.07 / 1M", OutputPrice: "$0.28 / 1M",
			OfficialInputPrice: "$0.27 / 1M", OfficialOutputPrice: "$1.10 / 1M", Discount: "-75%", IsFree: true,
			Description: "Fast multi-lingual general assistant with 671B parameters and state-of-the-art benchmark scores.",
			Tags: []string{"Free", "Fast", "General"},
		},
		{
			ID: "deepseek/deepseek-coder", Name: "DeepSeek Coder V2.5", Provider: "DeepSeek", ProviderIcon: "deepseek",
			Category: "Coding", ContextLength: "128K", InputPrice: "$0.08 / 1M", OutputPrice: "$0.32 / 1M",
			OfficialInputPrice: "$0.30 / 1M", OfficialOutputPrice: "$1.20 / 1M", Discount: "-73%", IsFree: true,
			Description: "Specialized code generation and refactoring model across 300+ programming languages.",
			Tags: []string{"Free", "Coding", "Long Context"},
		},

		// Anthropic
		{
			ID: "anthropic/claude-3-7-sonnet", Name: "Claude 3.7 Sonnet", Provider: "Anthropic", ProviderIcon: "anthropic",
			Category: "Flagship / Hybrid", ContextLength: "200K", InputPrice: "$1.80 / 1M", OutputPrice: "$9.00 / 1M",
			OfficialInputPrice: "$3.00 / 1M", OfficialOutputPrice: "$15.00 / 1M", Discount: "-40%", IsFree: false,
			Description: "Anthropic's most intelligent hybrid model with dynamic thinking budget & exceptional coding.",
			Tags: []string{"Flagship", "Coding", "Reasoning", "Thinking"},
		},
		{
			ID: "anthropic/claude-3-5-sonnet", Name: "Claude 3.5 Sonnet", Provider: "Anthropic", ProviderIcon: "anthropic",
			Category: "Coding & Vision", ContextLength: "200K", InputPrice: "$1.50 / 1M", OutputPrice: "$7.50 / 1M",
			OfficialInputPrice: "$3.00 / 1M", OfficialOutputPrice: "$15.00 / 1M", Discount: "-50%", IsFree: false,
			Description: "Benchmark leader in code, reasoning, multimodal analysis, and computer use workflows.",
			Tags: []string{"Popular", "Coding", "Vision"},
		},
		{
			ID: "anthropic/claude-3-5-haiku", Name: "Claude 3.5 Haiku", Provider: "Anthropic", ProviderIcon: "anthropic",
			Category: "Fast", ContextLength: "200K", InputPrice: "$0.45 / 1M", OutputPrice: "$2.25 / 1M",
			OfficialInputPrice: "$1.00 / 1M", OfficialOutputPrice: "$5.00 / 1M", Discount: "-55%", IsFree: false,
			Description: "Blazing fast inference with intelligence surpassing prior Claude 3 Opus generation.",
			Tags: []string{"Fast", "Cost-effective"},
		},
		{
			ID: "anthropic/claude-3-opus", Name: "Claude 3 Opus", Provider: "Anthropic", ProviderIcon: "anthropic",
			Category: "Complex Analysis", ContextLength: "200K", InputPrice: "$7.50 / 1M", OutputPrice: "$37.50 / 1M",
			OfficialInputPrice: "$15.00 / 1M", OfficialOutputPrice: "$75.00 / 1M", Discount: "-50%", IsFree: false,
			Description: "Deep creative analysis, high nuance understanding, and complex multi-step reasoning.",
			Tags: []string{"Deep Reasoning", "Enterprise"},
		},

		// OpenAI
		{
			ID: "openai/gpt-4.5", Name: "GPT-4.5 (Preview)", Provider: "OpenAI", ProviderIcon: "openai",
			Category: "Flagship", ContextLength: "128K", InputPrice: "$35.00 / 1M", OutputPrice: "$70.00 / 1M",
			OfficialInputPrice: "$75.00 / 1M", OfficialOutputPrice: "$150.00 / 1M", Discount: "-53%", IsFree: false,
			Description: "OpenAI's largest world-model with profound knowledge depth and natural conversational tone.",
			Tags: []string{"Flagship", "Knowledge"},
		},
		{
			ID: "openai/gpt-4o", Name: "GPT-4o", Provider: "OpenAI", ProviderIcon: "openai",
			Category: "Multimodal", ContextLength: "128K", InputPrice: "$1.25 / 1M", OutputPrice: "$5.00 / 1M",
			OfficialInputPrice: "$2.50 / 1M", OfficialOutputPrice: "$10.00 / 1M", Discount: "-50%", IsFree: false,
			Description: "Omni-model supporting high-speed text, audio, and vision processing with low latency.",
			Tags: []string{"Popular", "Multimodal", "Vision"},
		},
		{
			ID: "openai/gpt-4o-mini", Name: "GPT-4o mini", Provider: "OpenAI", ProviderIcon: "openai",
			Category: "Fast / Free Tier", ContextLength: "128K", InputPrice: "$0.075 / 1M", OutputPrice: "$0.30 / 1M",
			OfficialInputPrice: "$0.15 / 1M", OfficialOutputPrice: "$0.60 / 1M", Discount: "-50%", IsFree: true,
			Description: "Affordable lightweight model for everyday chat, summarization, and agent task execution.",
			Tags: []string{"Free", "Fast", "Lightweight"},
		},
		{
			ID: "openai/o1", Name: "OpenAI o1", Provider: "OpenAI", ProviderIcon: "openai",
			Category: "Reasoning", ContextLength: "200K", InputPrice: "$7.50 / 1M", OutputPrice: "$30.00 / 1M",
			OfficialInputPrice: "$15.00 / 1M", OfficialOutputPrice: "$60.00 / 1M", Discount: "-50%", IsFree: false,
			Description: "Advanced reasoning model using reinforcement learning for competitive coding and science.",
			Tags: []string{"Reasoning", "Math", "Science"},
		},
		{
			ID: "openai/o3-mini", Name: "OpenAI o3-mini", Provider: "OpenAI", ProviderIcon: "openai",
			Category: "Reasoning", ContextLength: "200K", InputPrice: "$0.55 / 1M", OutputPrice: "$2.20 / 1M",
			OfficialInputPrice: "$1.10 / 1M", OfficialOutputPrice: "$4.40 / 1M", Discount: "-50%", IsFree: false,
			Description: "High speed STEM and coding reasoning with selectable reasoning effort parameter.",
			Tags: []string{"Reasoning", "Coding", "Fast"},
		},

		// Google Gemini
		{
			ID: "google/gemini-2.0-flash", Name: "Gemini 2.0 Flash", Provider: "Google", ProviderIcon: "gemini",
			Category: "Multimodal", ContextLength: "1M", InputPrice: "$0.05 / 1M", OutputPrice: "$0.20 / 1M",
			OfficialInputPrice: "$0.10 / 1M", OfficialOutputPrice: "$0.40 / 1M", Discount: "-50%", IsFree: true,
			Description: "Ultra fast next-gen model with native tool use, real-time streaming, and 1M token context.",
			Tags: []string{"Free", "1M Context", "Fast", "Audio/Vision"},
		},
		{
			ID: "google/gemini-2.0-pro", Name: "Gemini 2.0 Pro Experimental", Provider: "Google", ProviderIcon: "gemini",
			Category: "Flagship", ContextLength: "2M", InputPrice: "$0.60 / 1M", OutputPrice: "$2.40 / 1M",
			OfficialInputPrice: "$1.25 / 1M", OfficialOutputPrice: "$5.00 / 1M", Discount: "-52%", IsFree: false,
			Description: "Google's strongest model for coding, complex agentic tasks, and massive 2M context.",
			Tags: []string{"2M Context", "Coding", "Flagship"},
		},
		{
			ID: "google/gemini-1.5-pro", Name: "Gemini 1.5 Pro", Provider: "Google", ProviderIcon: "gemini",
			Category: "Long Context", ContextLength: "2M", InputPrice: "$0.62 / 1M", OutputPrice: "$2.50 / 1M",
			OfficialInputPrice: "$1.25 / 1M", OfficialOutputPrice: "$5.00 / 1M", Discount: "-50%", IsFree: false,
			Description: "Industry-leading 2M token context window capable of ingesting entire codebases and video.",
			Tags: []string{"2M Context", "Multimodal"},
		},

		// xAI
		{
			ID: "xai/grok-2", Name: "Grok 2", Provider: "xAI", ProviderIcon: "grok",
			Category: "Frontier", ContextLength: "128K", InputPrice: "$1.00 / 1M", OutputPrice: "$5.00 / 1M",
			OfficialInputPrice: "$2.00 / 1M", OfficialOutputPrice: "$10.00 / 1M", Discount: "-50%", IsFree: false,
			Description: "xAI's flagship conversational and reasoning engine with real-time knowledge.",
			Tags: []string{"Frontier", "Vision", "Real-time"},
		},
		{
			ID: "xai/grok-2-mini", Name: "Grok 2 Mini", Provider: "xAI", ProviderIcon: "grok",
			Category: "Fast", ContextLength: "128K", InputPrice: "$0.10 / 1M", OutputPrice: "$0.50 / 1M",
			OfficialInputPrice: "$0.20 / 1M", OfficialOutputPrice: "$1.00 / 1M", Discount: "-50%", IsFree: true,
			Description: "Lightweight, highly efficient version of Grok 2 optimized for quick responses.",
			Tags: []string{"Free", "Fast"},
		},

		// Qwen
		{
			ID: "qwen/qwen-2.5-72b-instruct", Name: "Qwen 2.5 72B Instruct", Provider: "Qwen", ProviderIcon: "qwen",
			Category: "Open Weights", ContextLength: "128K", InputPrice: "$0.18 / 1M", OutputPrice: "$0.70 / 1M",
			OfficialInputPrice: "$0.40 / 1M", OfficialOutputPrice: "$1.60 / 1M", Discount: "-56%", IsFree: true,
			Description: "Top-ranked open-weights model beating Llama 3.3 70B in coding and math.",
			Tags: []string{"Free", "Open-Weights", "Multilingual"},
		},
		{
			ID: "qwen/qwen-2.5-coder-32b", Name: "Qwen 2.5 Coder 32B", Provider: "Qwen", ProviderIcon: "qwen",
			Category: "Coding", ContextLength: "128K", InputPrice: "$0.10 / 1M", OutputPrice: "$0.40 / 1M",
			OfficialInputPrice: "$0.25 / 1M", OfficialOutputPrice: "$1.00 / 1M", Discount: "-60%", IsFree: true,
			Description: "Coding powerhouse capable of matching GPT-4o level code generation.",
			Tags: []string{"Free", "Coding", "Leader"},
		},

		// Mistral
		{
			ID: "mistral/mistral-large-2411", Name: "Mistral Large 2", Provider: "Mistral AI", ProviderIcon: "mistral",
			Category: "Enterprise", ContextLength: "128K", InputPrice: "$1.00 / 1M", OutputPrice: "$3.00 / 1M",
			OfficialInputPrice: "$2.00 / 1M", OfficialOutputPrice: "$6.00 / 1M", Discount: "-50%", IsFree: false,
			Description: "Mistral's top-tier multilingual model designed for complex workflows and coding.",
			Tags: []string{"Enterprise", "Multilingual", "Coding"},
		},
		{
			ID: "mistral/codestral-2501", Name: "Codestral 2501", Provider: "Mistral AI", ProviderIcon: "mistral",
			Category: "Coding", ContextLength: "256K", InputPrice: "$0.15 / 1M", OutputPrice: "$0.45 / 1M",
			OfficialInputPrice: "$0.30 / 1M", OfficialOutputPrice: "$0.90 / 1M", Discount: "-50%", IsFree: true,
			Description: "Cutting edge code completion and FIM with 256K context.",
			Tags: []string{"Free", "Coding", "FIM"},
		},
	}
	_ = store.InsertModels(ctx, scrapedModels)

	// 2. Seed Pricing Tiers
	pricingTiers := []models.PricingTier{
		{
			ID: "free", Name: "Free", Price: 0, Period: "forever", Tokens: "500K",
			Features: []string{"20+ Free AI models", "DeepSeek R1 / V3 Free", "AI Image Generation", "Standard Speed (OpenAI & Anthropic SDKs)", "Community Support"},
		},
		{
			ID: "pro", Name: "Pro", Price: 20, Period: "month", Tokens: "15M", Badge: "Popular",
			Features: []string{"Access to all 200+ models", "Claude 3.7 Sonnet & Opus 4.8", "GPT-4.5 & GPT-4o", "DeepSeek V3 & R1 Fast Tier", "Unlimited API Keys", "Sub-60ms Routing", "Priority Support"},
		},
		{
			ID: "pro-plus", Name: "Pro+", Price: 50, Period: "month", Tokens: "45M",
			Features: []string{"Everything in Pro", "Higher Concurrency (100 req/s)", "Gemini 2.5 Flash / Pro", "Custom Spend Limits", "Webhook Notifications", "1-on-1 Support"},
		},
		{
			ID: "max", Name: "Max", Price: 100, Period: "month", Tokens: "100M",
			Features: []string{"Everything in Pro+", "High Volume Rate Discounts", "Concurrency 250 req/s", "Dedicated Endpoint Routing", "Team Access & Permissions", "SLA 99.9% Guarantee"},
		},
		{
			ID: "ultra", Name: "Ultra", Price: 250, Period: "month", Tokens: "300M", Badge: "Best Value",
			Features: []string{"Ultra-high throughput (500 req/s)", "Custom Model Routing & Fallbacks", "Custom Contract & Invoicing", "Dedicated Account Manager", "99.95% Uptime SLA"},
		},
		{
			ID: "power", Name: "Power", Price: 500, Period: "month", Tokens: "700M",
			Features: []string{"Enterprise Grade Infrastructure", "Custom Token Top-ups", "Custom Integrations & Proxy Rules", "24/7 Dedicated Phone & Telegram Support", "99.99% Uptime SLA"},
		},
	}
	_ = store.InsertPricingTiers(ctx, pricingTiers)

	// 3. Seed Deals
	deals := []models.Deal{
		{
			ID: "deal-1", Title: "New Agent Welcome Bonus", Tag: "FREE TOKENS",
			Desc: "Register today and get 500,000 free tokens immediately to test all free models with zero credit card required.",
			Code: "WELCOME500K", Discount: "100% Free", Status: "Active",
		},
		{
			ID: "deal-2", Title: "First Top-Up +30% Bonus", Tag: "TOP-UP BONUS",
			Desc: "Recharge $20 or more on your first deposit and receive an extra 30% bonus tokens added instantly.",
			Code: "BOOST30", Discount: "+30% Bonus", Status: "Active",
		},
		{
			ID: "deal-3", Title: "Developer Referral Program", Tag: "COMMISSION",
			Desc: "Invite fellow developers to xKiro and earn 15% lifetime revenue share on every token purchase they make.",
			Code: "REFERRAL15", Discount: "15% Lifetime", Status: "Active",
		},
		{
			ID: "deal-4", Title: "DeepSeek R1 High-Speed Pool", Tag: "SPECIAL TIER",
			Desc: "Enjoy dedicated DeepSeek R1 reasoning endpoints at 70% lower price than standard providers with instant response.",
			Code: "DEEPSEEK70", Discount: "70% OFF", Status: "Active",
		},
	}
	_ = store.InsertDeals(ctx, deals)

	// 4. Seed Status Services
	services := []models.StatusService{
		{Name: "US East (N. Virginia)", Ping: "18ms", Uptime: "99.99%", Status: "Operational"},
		{Name: "US West (Oregon)", Ping: "24ms", Uptime: "99.99%", Status: "Operational"},
		{Name: "Europe (Frankfurt)", Ping: "32ms", Uptime: "99.98%", Status: "Operational"},
		{Name: "Asia (Tokyo)", Ping: "45ms", Uptime: "100.0%", Status: "Operational"},
		{Name: "Asia (Singapore)", Ping: "38ms", Uptime: "99.99%", Status: "Operational"},
		{Name: "Vietnam (Hanoi Edge)", Ping: "12ms", Uptime: "100.0%", Status: "Operational"},
	}
	_ = store.InsertStatusServices(ctx, services)

	// 5. Seed Demo User & Key
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	demoUser := models.User{
		ID:                 "user-demo-001",
		Email:              "demo@lemas.ai",
		Password:           string(hashedPassword),
		Name:               "Lemas Developer",
		Role:               "user",
		Balance:            50.00,
		Tokens:             15000000,
		Plan:               "pro",
		DailyTokensUsed:    0,
		DailyTokensLimit:   1000,
		LastTokenResetDate: time.Now().Format("2006-01-02"),
		CreatedAt:          time.Now(),
	}
	_ = store.CreateUser(ctx, &demoUser)

	demoKey := models.ApiKey{
		ID:          "key-demo-001",
		UserID:      "user-demo-001",
		Key:         "lemas-live-demo-key-88888888",
		Name:        "Default Sandbox Key",
		SpendLimit:  100.0,
		SpendUsed:   0.0,
		Status:      "active",
		Permissions: []string{"chat:completions", "messages"},
		CreatedAt:   time.Now(),
	}
	_ = store.CreateApiKey(ctx, &demoKey)

	log.Printf("[DB Seed] Seeded %d models, %d pricing tiers, %d deals, and demo user.", len(scrapedModels), len(pricingTiers), len(deals))
}
