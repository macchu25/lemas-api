package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type UpstreamKey struct {
	Key          string
	RequestCount uint64
	ErrorCount   uint64
	IsActive     bool
	LastUsed     time.Time
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
		rawKeys := []string{
			"sk-xt-48f3942453cd56c311a41a705bd60472ba18d80763c58ab2",
			"sk-xt-65b72f4e618f50178d538a54e0d408b8d2004e4764fdb413",
			"sk-xt-9bd3036e461878903414cb00b769ba3bc3507897e537ba6e",
			"sk-xt-a34bd62e8cabc446cca3cf592f50be0e3b80fb4708926cd9",
			"sk-xt-5bf5d8cd17c63f92fd29174cec14992284901ced04d89818",
			"sk-xt-2babec9b4de550e3a6e4b01399fe5fcd008ad54eeb1b467a",
			"sk-xt-473cac3a9466a0e18777da1d4587f263b156e44161b7c8f7",
			"sk-xt-ddbb0cd051193aaee331487e806190b0b1526bb7432cea16",
		}

		keyObjects := make([]*UpstreamKey, len(rawKeys))
		for i, k := range rawKeys {
			keyObjects[i] = &UpstreamKey{
				Key:      k,
				IsActive: true,
				LastUsed: time.Now(),
			}
		}

		DefaultRotator = &KeyRotator{
			keys:         keyObjects,
			currentIndex: 0,
			httpClient: &http.Client{
				Timeout: 60 * time.Second,
			},
			baseURL: "https://api.xkiro.com/v1",
		}

		log.Printf("[Rotator] 🚀 Initialized Brain Engine with %d upstream rotating API keys", len(rawKeys))
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

	statsList := make([]map[string]interface{}, len(r.keys))
	for i, k := range r.keys {
		statsList[i] = map[string]interface{}{
			"index":         i + 1,
			"key_masked":    k.Key[:9] + "••••••••" + k.Key[len(k.Key)-4:],
			"request_count": k.RequestCount,
			"error_count":   k.ErrorCount,
			"is_active":     k.IsActive,
			"last_used":     k.LastUsed.Format(time.RFC3339),
		}
	}

	return map[string]interface{}{
		"total_keys":     len(r.keys),
		"active_keys":    len(r.keys),
		"default_model":  "Lemas 1.0 (Flagship)",
		"keys":           statsList,
		"rotation_mode":  "Smart Round-Robin with Auto-Failover",
	}
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

		req, err := http.NewRequestWithContext(ctx, "POST", r.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			lastErr = err
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+keyObj.Key)
		req.Header.Set("User-Agent", "Lemas.AI-Gateway-Rotator/1.0")

		log.Printf("[Rotator] ⚡ Routing request to `%s` via Key #%d (%s••••%s) [Attempt %d/%d]",
			payload["model"], keyIdx+1, keyObj.Key[:9], keyObj.Key[len(keyObj.Key)-4:], attempt+1, maxAttempts)

		resp, err := r.httpClient.Do(req)
		if err != nil {
			log.Printf("[Rotator] ⚠️ Key #%d request failed: %v. Rotating to next key...", keyIdx+1, err)
			atomic.AddUint64(&keyObj.ErrorCount, 1)
			lastErr = err
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var result map[string]interface{}
			if err := json.Unmarshal(body, &result); err != nil {
				lastErr = fmt.Errorf("failed to parse upstream response: %w", err)
				continue
			}
			return result, nil
		}

		// If error (e.g. 429 rate limit or 5xx), log and rotate to next key
		log.Printf("[Rotator] ⚠️ Upstream Key #%d returned HTTP %d (%s). Rotating to next key in pool...",
			keyIdx+1, resp.StatusCode, string(body))
		atomic.AddUint64(&keyObj.ErrorCount, 1)
		lastErr = fmt.Errorf("upstream HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil, fmt.Errorf("all %d upstream keys in rotation pool failed: %v", maxAttempts, lastErr)
}
