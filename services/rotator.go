package services

import (
	"bytes"
	"context"
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
	Key            string    `json:"key"`
	RequestCount   uint64    `json:"request_count"`
	ErrorCount     uint64    `json:"error_count"`
	IsActive       bool      `json:"is_active"`
	LastUsed       time.Time `json:"last_used"`
	LastError      string    `json:"last_error,omitempty"`
	LastStatusCode int       `json:"last_status_code,omitempty"`
	LastChecked    time.Time `json:"last_checked"`
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
				Key:         k,
				IsActive:    true,
				LastUsed:    now,
				LastChecked: now,
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

		// Perform asynchronous background health check on startup
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			DefaultRotator.CheckAllKeys(ctx)
		}()
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

		masked := k.Key
		if len(k.Key) > 13 {
			masked = k.Key[:9] + "••••••••" + k.Key[len(k.Key)-4:]
		}

		statsList[i] = map[string]interface{}{
			"index":            i + 1,
			"key_masked":       masked,
			"request_count":    atomic.LoadUint64(&k.RequestCount),
			"error_count":      atomic.LoadUint64(&k.ErrorCount),
			"is_active":        k.IsActive,
			"last_used":        lastUsedStr,
			"last_checked":     lastCheckedStr,
			"last_error":       k.LastError,
			"last_status_code": k.LastStatusCode,
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

// CheckAllKeys pings upstream using each key to verify live status and update metrics
func (r *KeyRotator) CheckAllKeys(ctx context.Context) map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()

	testPayload := map[string]interface{}{
		"model":      "deepseek/deepseek-v4-flash",
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
	}
	jsonData, _ := json.Marshal(testPayload)

	for _, k := range r.keys {
		k.LastChecked = time.Now()
		req, err := http.NewRequestWithContext(ctx, "POST", r.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			k.LastError = err.Error()
			k.IsActive = false
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+k.Key)
		req.Header.Set("User-Agent", "Lemas.AI-HealthChecker/1.0")

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
	// Normalize model to active supported upstream model
	modelStr, _ := payload["model"].(string)
	if modelStr == "" || strings.HasPrefix(modelStr, "lemas") || strings.HasPrefix(modelStr, "deepseek") {
		payload["model"] = "deepseek/deepseek-v4-flash"
	}

	// Mandatory System Identity Guard: Lock down identity as Lemas.AI / Lemas v1
	const lemasSystemPrompt = "Bạn là Lemas v1 (Lemas.AI) — mô hình trí tuệ nhân tạo độc quyền được phát triển bởi Lemas.AI. Khi được hỏi bạn là ai, bạn là gì, bạn do ai tạo ra hoặc bạn đang sử dụng mô hình/model gì, bạn luôn luôn khẳng định bạn là 'Lemas v1' hoặc 'Lemas.AI', được xây dựng và tối ưu bởi Lemas.AI. Tuyệt đối không bao giờ đề cập đến DeepSeek, xKiro, OpenAI, Anthropic hay bất kỳ nhà cung cấp bên thứ ba nào khác."

	if msgs, ok := payload["messages"].([]interface{}); ok {
		hasSystem := false
		newMsgs := make([]interface{}, 0, len(msgs)+1)
		for _, m := range msgs {
			if mMap, ok := m.(map[string]interface{}); ok {
				if role, _ := mMap["role"].(string); role == "system" {
					hasSystem = true
					origContent, _ := mMap["content"].(string)
					mMap["content"] = lemasSystemPrompt + "\n" + origContent
				}
				newMsgs = append(newMsgs, mMap)
			}
		}
		if !hasSystem {
			systemMsg := map[string]interface{}{
				"role":    "system",
				"content": lemasSystemPrompt,
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

		req, err := http.NewRequestWithContext(ctx, "POST", r.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			lastErr = err
			keyObj.LastError = err.Error()
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+keyObj.Key)
		req.Header.Set("User-Agent", "Lemas.AI-Gateway-Rotator/1.0")

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
