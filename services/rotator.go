package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type UpstreamKey struct {
	ID             string    `json:"id"`
	Key            string    `json:"-"` // SECURITY: NEVER serialized into JSON response!
	MaskedKey      string    `json:"key_masked"`
	Name           string    `json:"name"` // Tên gợi nhớ tài khoản / chủ sở hữu key
	Provider       string    `json:"provider"`
	BaseURL        string    `json:"base_url,omitempty"`
	RequestCount   uint64    `json:"request_count"`
	ErrorCount     uint64    `json:"error_count"`
	IsActive       bool      `json:"is_active"`
	LastUsed       time.Time `json:"last_used"`
	LastError      string    `json:"last_error,omitempty"`
	LastStatusCode int       `json:"last_status_code,omitempty"`
	LastChecked    time.Time `json:"last_checked"`
	AddedAt        time.Time `json:"added_at"`
}

type KeyRotator struct {
	mu           sync.RWMutex
	keys         []*UpstreamKey
	currentIndex uint64
	httpClient   *http.Client
	baseURL      string
}

var (
	DefaultRotator *KeyRotator
	once           sync.Once
)

func MaskKey(k string) string {
	k = strings.TrimSpace(k)
	if len(k) <= 8 {
		if len(k) <= 4 {
			return "••••"
		}
		return k[:2] + "••••" + k[len(k)-2:]
	}
	return k[:6] + "••••••••" + k[len(k)-4:]
}

func randomID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func InitKeyRotator() *KeyRotator {
	once.Do(func() {
		rawKeys := []string{}

		// Load keys exclusively from environment variables (.env / production env)
		if envKeys := os.Getenv("UPSTREAM_API_KEYS"); envKeys != "" {
			parts := strings.Split(envKeys, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					rawKeys = append(rawKeys, p)
				}
			}
		}

		if len(rawKeys) == 0 {
			log.Println("[Rotator] ⚠️ WARNING: No UPSTREAM_API_KEYS configured in .env!")
		}

		baseURL := os.Getenv("UPSTREAM_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.xkiro.com/v1"
		}

		now := time.Now()
		keyObjects := make([]*UpstreamKey, len(rawKeys))
		for i, k := range rawKeys {
			keyObjects[i] = &UpstreamKey{
				ID:          fmt.Sprintf("key-%d", i+1),
				Key:         k,
				MaskedKey:   MaskKey(k),
				Name:        fmt.Sprintf("ENV Account #%d", i+1),
				Provider:    "xKiro Upstream",
				BaseURL:     baseURL,
				IsActive:    true,
				LastUsed:    now,
				LastChecked: now,
				AddedAt:     now,
			}
		}

		DefaultRotator = &KeyRotator{
			keys:         keyObjects,
			currentIndex: 0,
			httpClient: &http.Client{
				Timeout: 60 * time.Second,
			},
			baseURL: baseURL,
		}

		log.Printf("[Rotator] 🚀 Initialized Brain Engine with %d upstream rotating API keys (Base: %s)", len(rawKeys), baseURL)
		log.Println("[Rotator] 🛡️ Stealth Mode active: Passive on-demand health tracking enabled (Startup bulk ping disabled)")
	})

	return DefaultRotator
}

func (r *KeyRotator) GetNextKey() (*UpstreamKey, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := len(r.keys)
	if total == 0 {
		return nil, -1
	}

	idx := int(atomic.AddUint64(&r.currentIndex, 1) % uint64(total))
	return r.keys[idx], idx
}

func (r *KeyRotator) GetPoolStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.getPoolStatsLocked()
}

func (r *KeyRotator) GetAllKeys() []*UpstreamKey {
	r.mu.RLock()
	defer r.mu.RUnlock()

	res := make([]*UpstreamKey, len(r.keys))
	for i, k := range r.keys {
		copyKey := *k
		copyKey.Key = "" // Security: Ensure raw key is wiped
		res[i] = &copyKey
	}
	return res
}

func (r *KeyRotator) getPoolStatsLocked() map[string]interface{} {
	statsList := make([]map[string]interface{}, len(r.keys))
	activeCount := 0
	deadCount := 0

	for i, k := range r.keys {
		if k.IsActive {
			activeCount++
		} else {
			deadCount++
		}

		var lastUsedStr string
		if !k.LastUsed.IsZero() {
			lastUsedStr = k.LastUsed.Format(time.RFC3339)
		}

		var lastCheckedStr string
		if !k.LastChecked.IsZero() {
			lastCheckedStr = k.LastChecked.Format(time.RFC3339)
		}

		var addedAtStr string
		if !k.AddedAt.IsZero() {
			addedAtStr = k.AddedAt.Format(time.RFC3339)
		}

		statsList[i] = map[string]interface{}{
			"id":               k.ID,
			"name":             k.Name,
			"index":            i + 1,
			"key_masked":       k.MaskedKey,
			"provider":         k.Provider,
			"base_url":         k.BaseURL,
			"request_count":    atomic.LoadUint64(&k.RequestCount),
			"error_count":      atomic.LoadUint64(&k.ErrorCount),
			"is_active":        k.IsActive,
			"last_used":        lastUsedStr,
			"last_checked":     lastCheckedStr,
			"last_error":       k.LastError,
			"last_status_code": k.LastStatusCode,
			"added_at":         addedAtStr,
		}
	}

	return map[string]interface{}{
		"total_keys":    len(r.keys),
		"active_keys":   activeCount,
		"dead_keys":     deadCount,
		"default_model": "Lemas 1.0 (Flagship)",
		"keys":          statsList,
		"rotation_mode": "Smart Round-Robin with Live Health Guard",
	}
}

// AddKey registers a new API key to the active rotation pool with an optional account name/alias
func (r *KeyRotator) AddKey(ctx context.Context, rawKey string, name string, provider string, customBaseURL string, testFirst bool) (*UpstreamKey, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return nil, fmt.Errorf("API Key không được để trống")
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("Account Key #%d", len(r.keys)+1)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.keys {
		if existing.Key == rawKey {
			return nil, fmt.Errorf("API Key này đã tồn tại trong hệ thống (ID: %s)", existing.ID)
		}
	}

	targetBaseURL := strings.TrimRight(strings.TrimSpace(customBaseURL), "/")
	if targetBaseURL == "" {
		targetBaseURL = r.baseURL
	}

	if provider == "" {
		if strings.Contains(targetBaseURL, "deepseek.com") {
			provider = "DeepSeek Official"
		} else if strings.Contains(targetBaseURL, "openrouter.ai") {
			provider = "OpenRouter"
		} else if strings.Contains(targetBaseURL, "openai.com") {
			provider = "OpenAI"
		} else if strings.Contains(targetBaseURL, "groq.com") {
			provider = "Groq"
		} else if strings.Contains(targetBaseURL, "xkiro.com") {
			provider = "xKiro"
		} else {
			provider = "Custom Provider"
		}
	}

	now := time.Now()
	newKey := &UpstreamKey{
		ID:          "key-" + randomID(),
		Key:         rawKey,
		MaskedKey:   MaskKey(rawKey),
		Name:        name,
		Provider:    provider,
		BaseURL:     targetBaseURL,
		IsActive:    true,
		LastUsed:    now,
		LastChecked: now,
		AddedAt:     now,
	}

	if testFirst {
		// Test the key against upstream
		success, status, msg, _ := r.testSingleKeyInternal(ctx, rawKey, targetBaseURL, "")
		newKey.LastStatusCode = status
		newKey.LastError = msg
		newKey.IsActive = success
		if !success {
			return nil, fmt.Errorf("kiểm tra API key thất bại (HTTP %d): %s", status, msg)
		}
	}

	r.keys = append(r.keys, newKey)
	log.Printf("[Rotator] 🔑 Added new Upstream Key %s (%s) to pool. Total keys: %d", newKey.MaskedKey, newKey.Provider, len(r.keys))

	copyKey := *newKey
	copyKey.Key = "" // Wipe raw key in return
	return &copyKey, nil
}

// RemoveKey deletes a key by its unique ID
func (r *KeyRotator) RemoveKey(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx := -1
	for i, k := range r.keys {
		if k.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("không tìm thấy key với ID: %s", id)
	}

	r.keys = append(r.keys[:idx], r.keys[idx+1:]...)
	log.Printf("[Rotator] 🗑️ Removed Upstream Key ID %s from pool. Remaining: %d", id, len(r.keys))
	return nil
}

// ToggleKey activates or deactivates a key without deleting it
func (r *KeyRotator) ToggleKey(id string, active bool) (*UpstreamKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, k := range r.keys {
		if k.ID == id {
			k.IsActive = active
			copyKey := *k
			copyKey.Key = ""
			return &copyKey, nil
		}
	}
	return nil, fmt.Errorf("không tìm thấy key với ID: %s", id)
}

// Realistic client User-Agents to prevent fingerprinting
var stealthUserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:130.0) Gecko/20100101 Firefox/130.0",
	"OpenAI/NodeJS/4.52.0",
	"OpenAI/Python/1.42.0",
}

func getRandomUserAgent() string {
	b := make([]byte, 1)
	_, _ = rand.Read(b)
	idx := int(b[0]) % len(stealthUserAgents)
	return stealthUserAgents[idx]
}

// TestSingleKey tests an arbitrary key directly without necessarily adding it to pool
func (r *KeyRotator) TestSingleKey(ctx context.Context, rawKey string, baseURL string, model string) (bool, int, string, int64) {
	return r.testSingleKeyInternal(ctx, rawKey, baseURL, model)
}

func (r *KeyRotator) testSingleKeyInternal(ctx context.Context, rawKey string, baseURL string, model string) (bool, int, string, int64) {
	rawKey = strings.TrimSpace(rawKey)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = r.baseURL
	}
	if model == "" {
		if strings.Contains(baseURL, "deepseek.com") {
			model = "deepseek-chat"
		} else if strings.Contains(baseURL, "openai.com") {
			model = "gpt-4o-mini"
		} else if strings.Contains(baseURL, "openrouter.ai") {
			model = "deepseek/deepseek-chat"
		} else if strings.Contains(baseURL, "groq.com") {
			model = "llama-3.3-70b-versatile"
		} else {
			model = "deepseek/deepseek-v4-flash"
		}
	}

	testPayload := map[string]interface{}{
		"model":      model,
		"max_tokens": 5,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello"},
		},
	}
	jsonData, _ := json.Marshal(testPayload)

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return false, 0, err.Error(), 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("User-Agent", getRandomUserAgent())
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := r.httpClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return false, 0, fmt.Sprintf("Lỗi kết nối mạng: %v", err), latency
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, resp.StatusCode, "Kết nối thành công! API Key hoạt động bình thường.", latency
	}

	return false, resp.StatusCode, string(body), latency
}

// CheckAllKeys pings upstream using each key to verify live status with randomized jitter and neutral prompts
func (r *KeyRotator) CheckAllKeys(ctx context.Context) map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()

	benignGreetings := []string{"Hello", "Hi there", "Xin chào", "Greetings", "Good day"}

	for i, k := range r.keys {
		// Introduce small random jitter between key checks (150ms - 400ms) to avoid simultaneous burst fingerprinting
		if i > 0 {
			b := make([]byte, 1)
			_, _ = rand.Read(b)
			jitterMs := 150 + int(b[0])%250
			time.Sleep(time.Duration(jitterMs) * time.Millisecond)
		}

		greeting := benignGreetings[i%len(benignGreetings)]
		testPayload := map[string]interface{}{
			"model":      "deepseek/deepseek-v4-flash",
			"max_tokens": 1,
			"messages": []map[string]string{
				{"role": "user", "content": greeting},
			},
		}
		jsonData, _ := json.Marshal(testPayload)

		k.LastChecked = time.Now()
		targetBase := k.BaseURL
		if targetBase == "" {
			targetBase = r.baseURL
		}

		req, err := http.NewRequestWithContext(ctx, "POST", targetBase+"/chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			k.LastError = err.Error()
			k.IsActive = false
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+k.Key)
		req.Header.Set("User-Agent", getRandomUserAgent())
		req.Header.Set("Accept", "application/json")

		resp, err := r.httpClient.Do(req)
		if err != nil {
			k.LastError = err.Error()
			k.IsActive = false
			atomic.AddUint64(&k.ErrorCount, 1)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		k.LastStatusCode = resp.StatusCode
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			k.IsActive = true
			k.LastError = ""
		} else {
			k.IsActive = false
			k.LastError = string(body)
			atomic.AddUint64(&k.ErrorCount, 1)
		}
	}

	return r.getPoolStatsLocked()
}

func (r *KeyRotator) ForwardChat(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	// Respect the model requested by the user. Only apply a default if empty or 'default'
	modelStr, _ := payload["model"].(string)
	modelStr = strings.TrimSpace(modelStr)
	if modelStr == "" || modelStr == "default" || modelStr == "lemas-1.0" {
		payload["model"] = "deepseek/deepseek-v4-flash"
	} else {
		// Use exact model requested by user (e.g. deepseek/deepseek-r1, openai/gpt-4o, claude-3-7-sonnet, etc.)
		payload["model"] = modelStr
	}

	// Neutral, professional AI system prompt without mentioning upstream provider names
	const neutralSystemPrompt = "Bạn là một trợ lý trí tuệ nhân tạo thông minh, hữu ích và lịch sự. Hãy trả lời câu hỏi của người dùng một cách chính xác, tự nhiên và chuyên nghiệp nhất."

	if msgs, ok := payload["messages"].([]interface{}); ok {
		hasSystem := false
		newMsgs := make([]interface{}, 0, len(msgs)+1)
		for _, m := range msgs {
			if mMap, ok := m.(map[string]interface{}); ok {
				if role, _ := mMap["role"].(string); role == "system" {
					hasSystem = true
					origContent, _ := mMap["content"].(string)
					mMap["content"] = neutralSystemPrompt + "\n" + origContent
				}
				newMsgs = append(newMsgs, mMap)
			}
		}
		if !hasSystem {
			systemMsg := map[string]interface{}{
				"role":    "system",
				"content": neutralSystemPrompt,
			}
			newMsgs = append([]interface{}{systemMsg}, newMsgs...)
		}
		payload["messages"] = newMsgs
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	maxAttempts := len(r.keys)
	if maxAttempts == 0 {
		return nil, fmt.Errorf("no upstream keys configured in pool")
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		keyObj, keyIdx := r.GetNextKey()
		if keyObj == nil {
			continue
		}

		atomic.AddUint64(&keyObj.RequestCount, 1)
		keyObj.LastUsed = time.Now()
		keyObj.LastChecked = time.Now()

		targetBase := keyObj.BaseURL
		if targetBase == "" {
			targetBase = r.baseURL
		}

		req, err := http.NewRequestWithContext(ctx, "POST", targetBase+"/chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			lastErr = err
			keyObj.LastError = err.Error()
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+keyObj.Key)
		req.Header.Set("User-Agent", getRandomUserAgent())
		req.Header.Set("Accept", "application/json")

		resp, err := r.httpClient.Do(req)
		if err != nil {
			log.Printf("[Rotator] ⚠️ Key #%d request failed: %v. Rotating to next key...", keyIdx+1, err)
			atomic.AddUint64(&keyObj.ErrorCount, 1)
			keyObj.LastError = err.Error()
			keyObj.IsActive = false
			lastErr = err
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		keyObj.LastStatusCode = resp.StatusCode

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			keyObj.IsActive = true
			keyObj.LastError = ""
			var result map[string]interface{}
			if err := json.Unmarshal(body, &result); err != nil {
				lastErr = fmt.Errorf("failed to parse upstream response: %w", err)
				continue
			}
			return result, nil
		}

		// If error (e.g. 401 invalid key, 403 quota, 429 rate limit or 5xx), update status and rotate
		errMsg := string(body)
		keyObj.LastError = errMsg
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			keyObj.IsActive = false
		}
		atomic.AddUint64(&keyObj.ErrorCount, 1)
		log.Printf("[Rotator] ⚠️ Upstream Key #%d returned HTTP %d (%s). Rotating to next key in pool...",
			keyIdx+1, resp.StatusCode, errMsg)
		lastErr = fmt.Errorf("upstream HTTP %d: %s", resp.StatusCode, errMsg)
	}

	return nil, fmt.Errorf("all %d upstream keys in rotation pool failed: %v", maxAttempts, lastErr)
}
