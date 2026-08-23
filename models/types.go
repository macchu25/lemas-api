package models

import (
	"time"
)

type User struct {
	ID                 string    `json:"id" bson:"_id,omitempty"`
	Email              string    `json:"email" bson:"email"`
	Password           string    `json:"-" bson:"password"`
	Name               string    `json:"name" bson:"name"`
	Role               string    `json:"role" bson:"role"` // "user", "admin"
	Balance            float64   `json:"balance" bson:"balance"` // in USD
	Tokens             int64     `json:"tokens" bson:"tokens"` // token balance
	Plan               string    `json:"plan" bson:"plan"` // "free", "pro", "pro-plus", "max", "ultra", "power"
	DailyTokensUsed    int64     `json:"daily_tokens_used" bson:"daily_tokens_used"`
	DailyTokensLimit   int64     `json:"daily_tokens_limit" bson:"daily_tokens_limit"` // default 1000 tokens/day
	GiftTokens         int64     `json:"gift_tokens" bson:"gift_tokens"` // permanent gift tokens, never reset daily
	LastTokenResetDate string    `json:"last_token_reset_date" bson:"last_token_reset_date"` // YYYY-MM-DD
	DailyImagesUsed    int64     `json:"daily_images_used" bson:"daily_images_used"`
	DailyImagesLimit   int64     `json:"daily_images_limit" bson:"daily_images_limit"` // default 5 images/day for free plan
	LastImageResetDate string    `json:"last_image_reset_date" bson:"last_image_reset_date"` // YYYY-MM-DD
	CreatedAt          time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt          time.Time `json:"updated_at" bson:"updated_at"`
}

type Giftcode struct {
	ID        string    `json:"id" bson:"_id,omitempty"`
	Code      string    `json:"code" bson:"code"`
	Tokens    int64     `json:"tokens" bson:"tokens"` // Token reward amount (permanent)
	MaxUses   int       `json:"max_uses" bson:"max_uses"` // Maximum number of redemptions
	UsedCount int       `json:"used_count" bson:"used_count"` // Number of times redeemed
	UsedBy    []string  `json:"used_by" bson:"used_by"` // User IDs who claimed this code
	Status    string    `json:"status" bson:"status"` // "active", "exhausted"
	CreatedAt time.Time `json:"created_at" bson:"created_at"`
}

type ApiKey struct {
	ID          string    `json:"id" bson:"_id,omitempty"`
	UserID      string    `json:"user_id" bson:"user_id"`
	Key         string    `json:"key" bson:"key"` // format: "xk-live-..."
	Name        string    `json:"name" bson:"name"`
	SpendLimit  float64   `json:"spend_limit" bson:"spend_limit"`
	SpendUsed   float64   `json:"spend_used" bson:"spend_used"`
	Status      string    `json:"status" bson:"status"` // "active", "revoked"
	Permissions []string  `json:"permissions" bson:"permissions"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty" bson:"last_used_at,omitempty"`
	CreatedAt   time.Time `json:"created_at" bson:"created_at"`
}

type ModelItem struct {
	ID                 string   `json:"id" bson:"_id,omitempty"`
	Name               string   `json:"name" bson:"name"`
	Provider           string   `json:"provider" bson:"provider"`
	ProviderIcon       string   `json:"provider_icon" bson:"provider_icon"`
	Category           string   `json:"category" bson:"category"`
	ContextLength      string   `json:"context_length" bson:"context_length"`
	InputPrice         string   `json:"input_price" bson:"input_price"`
	OutputPrice        string   `json:"output_price" bson:"output_price"`
	OfficialInputPrice string   `json:"official_input_price" bson:"official_input_price"`
	OfficialOutputPrice string  `json:"official_output_price" bson:"official_output_price"`
	Discount           string   `json:"discount" bson:"discount"`
	IsFree             bool     `json:"is_free" bson:"is_free"`
	Description        string   `json:"description" bson:"description"`
	Tags               []string `json:"tags" bson:"tags"`
}

type PricingTier struct {
	ID       string   `json:"id" bson:"_id,omitempty"`
	Name     string   `json:"name" bson:"name"`
	Price    float64  `json:"price" bson:"price"`
	Period   string   `json:"period" bson:"period"`
	Tokens   string   `json:"tokens" bson:"tokens"`
	Badge    string   `json:"badge,omitempty" bson:"badge,omitempty"`
	Features []string `json:"features" bson:"features"`
}

type Deal struct {
	ID       string `json:"id" bson:"_id,omitempty"`
	Title    string `json:"title" bson:"title"`
	Tag      string `json:"tag" bson:"tag"`
	Desc     string `json:"desc" bson:"desc"`
	Code     string `json:"code" bson:"code"`
	Discount string `json:"discount" bson:"discount"`
	Status   string `json:"status" bson:"status"`
}

type StatusService struct {
	Name   string `json:"name" bson:"name"`
	Ping   string `json:"ping" bson:"ping"`
	Uptime string `json:"uptime" bson:"uptime"`
	Status string `json:"status" bson:"status"`
}

type ContactMessage struct {
	ID        string    `json:"id" bson:"_id,omitempty"`
	Name      string    `json:"name" bson:"name"`
	Email     string    `json:"email" bson:"email"`
	Subject   string    `json:"subject" bson:"subject"`
	Message   string    `json:"message" bson:"message"`
	CreatedAt time.Time `json:"created_at" bson:"created_at"`
}

type UsageLog struct {
	ID           string    `json:"id" bson:"_id,omitempty"`
	UserID       string    `json:"user_id" bson:"user_id"`
	ApiKeyID     string    `json:"api_key_id" bson:"api_key_id"`
	Model        string    `json:"model" bson:"model"`
	PromptTokens int       `json:"prompt_tokens" bson:"prompt_tokens"`
	CompTokens   int       `json:"completion_tokens" bson:"completion_tokens"`
	TotalTokens  int       `json:"total_tokens" bson:"total_tokens"`
	CostUSD      float64   `json:"cost_usd" bson:"cost_usd"`
	LatencyMs    int64     `json:"latency_ms" bson:"latency_ms"`
	Timestamp    time.Time `json:"timestamp" bson:"timestamp"`
}

type TopupTransaction struct {
	ID        string    `json:"id" bson:"_id,omitempty"`
	UserID    string    `json:"user_id" bson:"user_id"`
	AmountUSD float64   `json:"amount_usd" bson:"amount_usd"`
	AmountVND int64     `json:"amount_vnd" bson:"amount_vnd"`
	Method    string    `json:"method" bson:"method"` // "SePay VietQR"
	BankCode  string    `json:"bank_code" bson:"bank_code"` // "MBBank"
	Memo      string    `json:"memo" bson:"memo"`
	Status    string    `json:"status" bson:"status"` // "completed"
	CreatedAt time.Time `json:"created_at" bson:"created_at"`
}

// OpenAI format requests & responses
type ChatCompletionRequest struct {
	Model       string                   `json:"model"`
	Messages    []ChatCompletionMessage  `json:"messages"`
	Temperature float64                  `json:"temperature,omitempty"`
	MaxTokens   int                      `json:"max_tokens,omitempty"`
	Stream      bool                     `json:"stream,omitempty"`
}

type ChatCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionResponse struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []ChatCompletionChoice   `json:"choices"`
	Usage   ChatCompletionUsage      `json:"usage"`
}

type ChatCompletionChoice struct {
	Index        int                   `json:"index"`
	Message      ChatCompletionMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type ChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
