package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// MachGenProvider implements image generation via the MachGen engine with dual-reference image conditioning
type MachGenProvider struct {
	mu              sync.RWMutex
	baseURL         string
	apiKey          string
	model           string
	httpClient      *http.Client
	lastWasFallback bool
	fallbackReason  string
}

func NewMachGenProvider() *MachGenProvider {
	apiURL := os.Getenv("MACHGEN_API_URL")
	if apiURL == "" {
		apiURL = os.Getenv("MACHGEN_APT_URL") // Fallback for common typo
	}
	if apiURL == "" {
		apiURL = os.Getenv("UPSTREAM_BASE_URL")
	}
	if apiURL == "" {
		// Default to Pollinations MachGen engine
		apiURL = "https://image.pollinations.ai"
	}

	apiKey := os.Getenv("MACHGEN_API_KEY")
	modelName := os.Getenv("MACHGEN_MODEL")
	if modelName == "" {
		modelName = "gpt-image-2"
	}

	return &MachGenProvider{
		baseURL: strings.TrimRight(apiURL, "/"),
		apiKey:  apiKey,
		model:   modelName,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

func (m *MachGenProvider) Name() string {
	return "MachGen Dual-Reference Engine"
}

// Configure updates MachGen connection credentials dynamically at runtime
func (m *MachGenProvider) Configure(baseURL, apiKey, model string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if baseURL != "" {
		m.baseURL = strings.TrimRight(baseURL, "/")
	}
	if apiKey != "" {
		m.apiKey = apiKey
	}
	if model != "" {
		m.model = model
	}
}

func (m *MachGenProvider) IsFallbackEngaged() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastWasFallback
}

func (m *MachGenProvider) GetFallbackReason() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.fallbackReason
}

func (m *MachGenProvider) GetAPIKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.apiKey
}

func (m *MachGenProvider) GetBaseURL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.baseURL
}

// Generate implements the standard ArtQRProvider interface
func (m *MachGenProvider) Generate(ctx context.Context, req *GenerationRequest) ([]GeneratedImage, error) {
	w := req.Width
	h := req.Height
	if w <= 0 {
		w = 1024
	}
	if h <= 0 {
		h = 1024
	}

	rawImg, err := m.GenerateWithTwoReferences(ctx, req.ReferenceImageBytes, req.QRControlImagePNG, req.Prompt, m.model, w, h)
	if err != nil {
		return nil, err
	}

	return []GeneratedImage{
		{
			URL:      "",
			PNGBytes: rawImg,
			Seed:     req.Seed,
		},
	}, nil
}

// GenerateWithTwoReferences sends Reference Image 1 (Base Scene) and Reference Image 2 (Cleaned QR) to MachGen
func (m *MachGenProvider) GenerateWithTwoReferences(
	ctx context.Context,
	baseSceneBytes []byte,
	cleanedQRBytes []byte,
	promptText string,
	modelName string,
	width, height int,
) ([]byte, error) {
	if modelName == "" {
		modelName = m.model
	}
	if width <= 0 {
		width = 1024
	}
	if height <= 0 {
		height = 1024
	}

	// Dynamic fallback to environment variables if not set
	if m.apiKey == "" {
		if envKey := os.Getenv("MACHGEN_API_KEY"); envKey != "" {
			m.apiKey = envKey
		}
	}
	if envURL := os.Getenv("MACHGEN_API_URL"); envURL != "" {
		m.baseURL = strings.TrimRight(envURL, "/")
	} else if envAptURL := os.Getenv("MACHGEN_APT_URL"); envAptURL != "" {
		m.baseURL = strings.TrimRight(envAptURL, "/")
	}
	if modelName == "" || modelName == "flux" {
		if envModel := os.Getenv("MACHGEN_MODEL"); envModel != "" {
			modelName = envModel
		}
	}

	maskedKey := "none"
	if m.apiKey != "" {
		if len(m.apiKey) > 6 {
			maskedKey = m.apiKey[:3] + "..." + m.apiKey[len(m.apiKey)-3:]
		} else {
			maskedKey = "***"
		}
	}

	log.Printf("[MachGen] Dispatching generation request (model=%s, target=%dx%d, key=%s, base_size=%d, qr_size=%d)",
		modelName, width, height, maskedKey, len(baseSceneBytes), len(cleanedQRBytes))

	start := time.Now()

	// Reset fallback status for this generation request
	m.mu.Lock()
	m.lastWasFallback = false
	m.fallbackReason = ""
	m.mu.Unlock()

	// Approach A: If API key is configured OR endpoint is an API endpoint (not default pollinations)
	if m.apiKey != "" || strings.Contains(m.baseURL, "/v1") || strings.Contains(m.baseURL, "replicate") || strings.Contains(m.baseURL, "xkiro") || !strings.Contains(m.baseURL, "pollinations") {
		result, err := m.callAPIEndpoint(ctx, baseSceneBytes, cleanedQRBytes, promptText, modelName, width, height)
		if err == nil && len(result) > 0 {
			log.Printf("[MachGen] Dual-reference synthesis completed in %v", time.Since(start))
			return result, nil
		}

		log.Printf("[MachGen] ⚠️ Primary API endpoint (%s) call failed: %v", m.baseURL, err)

		// Record fallback state so callers (and UI) know auto-swap occurred
		m.mu.Lock()
		m.lastWasFallback = true
		m.fallbackReason = fmt.Sprintf("API chính (%s) gặp sự cố: %v -> Đã tự động swap sang MachGen FLUX Engine", m.baseURL, err)
		m.mu.Unlock()

		log.Printf("[MachGen] 🔄 Auto-swapping from primary API to MachGen Free FLUX Engine to prevent generation failure...")
	}

	// Approach B: Pollinations / MachGen Free Direct Image Stream Engine
	result, err := m.callPollinationsStream(ctx, promptText, modelName, width, height)
	if err == nil && len(result) > 100 {
		log.Printf("[MachGen] Generation completed in %v (%d bytes) [fallback=%v]", time.Since(start), len(result), m.IsFallbackEngaged())
		return result, nil
	}

	log.Printf("[MachGen] ⚠️ MachGen free engine stream failed (%v)", err)

	// Safe graceful degradation: If both upstream API and free AI stream fail,
	// use baseSceneBytes as living canvas so Art QR generation NEVER crashes
	if len(baseSceneBytes) > 0 {
		m.mu.Lock()
		m.lastWasFallback = true
		m.fallbackReason = "API chính và MachGen Free Engine bận -> Đã dùng ảnh mẫu tham chiếu làm nền"
		m.mu.Unlock()
		log.Printf("[MachGen] 🛡️ Using reference base scene (%d bytes) to guarantee 100%% generation uptime", len(baseSceneBytes))
		return baseSceneBytes, nil
	}

	return nil, fmt.Errorf("cả API chính và MachGen Engine dự phòng đều thất bại: %w", err)
}

func (m *MachGenProvider) callAPIEndpoint(
	ctx context.Context,
	baseSceneBytes []byte,
	cleanedQRBytes []byte,
	promptText string,
	modelName string,
	width, height int,
) ([]byte, error) {
	if strings.Contains(strings.ToLower(m.baseURL), "machgen.ai") {
		return m.callMachGenImageEdit(ctx, baseSceneBytes, cleanedQRBytes, promptText, modelName)
	}
	var req *http.Request
	var err error

	if strings.Contains(m.baseURL, "replicate") {
		// Replicate Predictions API
		payload := map[string]interface{}{
			"version": "black-forest-labs/flux-schnell",
			"input": map[string]interface{}{
				"prompt": promptText,
				"width":  width,
				"height": height,
			},
		}
		jsonBytes, _ := json.Marshal(payload)
		req, err = http.NewRequestWithContext(ctx, "POST", m.baseURL+"/predictions", bytes.NewReader(jsonBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if m.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+m.apiKey)
		}
	} else {
		// Multi-modal JSON payload with base64 reference images
		ref1Base64 := base64.StdEncoding.EncodeToString(baseSceneBytes)
		ref2Base64 := base64.StdEncoding.EncodeToString(cleanedQRBytes)

		payload := map[string]interface{}{
			"model":           modelName,
			"prompt":          promptText,
			"size":            fmt.Sprintf("%dx%d", width, height),
			"width":           width,
			"height":          height,
			"n":               1,
			"response_format": "b64_json",
			"reference_images": []string{
				"data:image/jpeg;base64," + ref1Base64,
				"data:image/png;base64," + ref2Base64,
			},
		}

		jsonBytes, _ := json.Marshal(payload)
		endpointURL := m.baseURL
		if !strings.HasSuffix(endpointURL, "/images/generations") {
			endpointURL = strings.TrimRight(endpointURL, "/") + "/images/generations"
		}
		req, err = http.NewRequestWithContext(ctx, "POST", endpointURL, bytes.NewReader(jsonBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if m.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+m.apiKey)
		}
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errMsg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(errMsg))
	}

	// If direct image stream was returned
	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "image/") {
		return io.ReadAll(resp.Body)
	}

	// If JSON response with image URL or base64
	var jsonResp struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
		Output []string `json:"output"`
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(respBytes, &jsonResp); err == nil {
		if len(jsonResp.Data) > 0 {
			if jsonResp.Data[0].B64JSON != "" {
				return base64.StdEncoding.DecodeString(jsonResp.Data[0].B64JSON)
			}
			if jsonResp.Data[0].URL != "" {
				return m.fetchImageFromURL(ctx, jsonResp.Data[0].URL)
			}
		}
		if len(jsonResp.Output) > 0 && jsonResp.Output[0] != "" {
			return m.fetchImageFromURL(ctx, jsonResp.Output[0])
		}
	}

	return nil, fmt.Errorf("could not parse image from API response")
}

func (m *MachGenProvider) machGenBaseURL() string {
	base := strings.TrimRight(m.baseURL, "/")
	for _, suffix := range []string{"/api/v0/generate", "/api/v0", "/v1"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return base
}

func (m *MachGenProvider) uploadMachGenImage(ctx context.Context, imageBytes []byte, filename string) (string, error) {
	if len(imageBytes) == 0 {
		return "", fmt.Errorf("MachGen source image %s is empty", filename)
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	file, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err = file.Write(imageBytes); err != nil {
		return "", err
	}
	if err = w.Close(); err != nil {
		return "", err
	}
	endpoint := m.machGenBaseURL() + "/api/v0/upload"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	responseBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("MachGen upload HTTP %d: %s", resp.StatusCode, string(responseBytes))
	}
	var uploaded struct {
		ArtifactPath string `json:"artifact_path"`
	}
	if err := json.Unmarshal(responseBytes, &uploaded); err != nil || uploaded.ArtifactPath == "" {
		return "", fmt.Errorf("invalid MachGen upload response: %s", string(responseBytes))
	}
	return "@input/" + strings.TrimPrefix(uploaded.ArtifactPath, "/"), nil
}

func (m *MachGenProvider) callMachGenImageEdit(ctx context.Context, guideImage, cleanedQR []byte, promptText, modelName string) ([]byte, error) {
	if m.apiKey == "" {
		return nil, fmt.Errorf("MACHGEN_API_KEY is required")
	}
	if modelName == "" || strings.EqualFold(modelName, "gpt-image-2") || modelName == "flux" {
		modelName = "GPT-Image-2"
	}
	guideRef, err := m.uploadMachGenImage(ctx, guideImage, "art-qr-guide.png")
	if err != nil {
		return nil, err
	}
	qrRef, err := m.uploadMachGenImage(ctx, cleanedQR, "art-qr-transparent.png")
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"model":          modelName,
		"task_type":      "I2I",
		"prompt":         promptText,
		"src_image_urls": []string{guideRef, qrRef},
		"image_config":   map[string]int{"width": 1280, "height": 1280},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint := m.machGenBaseURL() + "/api/v0/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	responseBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("MachGen generate HTTP %d: %s", resp.StatusCode, string(responseBytes))
	}
	var created struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(responseBytes, &created); err != nil || created.TaskID == "" {
		return nil, fmt.Errorf("invalid MachGen generate response: %s", string(responseBytes))
	}

	pollURL := m.machGenBaseURL() + "/api/v0/tasks/" + url.PathEscape(created.TaskID)
	delay := 2 * time.Second
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		pollReq, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return nil, err
		}
		pollReq.Header.Set("Authorization", "Bearer "+m.apiKey)
		pollResp, err := m.httpClient.Do(pollReq)
		if err != nil {
			return nil, err
		}
		pollBytes, readErr := io.ReadAll(io.LimitReader(pollResp.Body, 1<<20))
		pollResp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if pollResp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("MachGen task HTTP %d: %s", pollResp.StatusCode, string(pollBytes))
		}
		var job struct {
			Status     string            `json:"status"`
			TaskOutput map[string]string `json:"task_output"`
			Error      string            `json:"error_msg"`
		}
		if err := json.Unmarshal(pollBytes, &job); err != nil {
			return nil, err
		}
		if job.Status == "COMPLETED" {
			imageURL := job.TaskOutput["image"]
			if imageURL == "" {
				imageURL = m.machGenBaseURL() + "/api/v0/assets/" + url.PathEscape(created.TaskID)
			}
			return m.fetchMachGenAsset(ctx, imageURL)
		}
		if job.Status == "FAILED" {
			return nil, fmt.Errorf("MachGen image edit failed: %s", job.Error)
		}
		if delay < 8*time.Second {
			delay += time.Second
		}
	}
}

func (m *MachGenProvider) fetchMachGenAsset(ctx context.Context, assetURL string) ([]byte, error) {
	if strings.HasPrefix(assetURL, "/") {
		assetURL = m.machGenBaseURL() + assetURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MachGen asset HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (m *MachGenProvider) imageData(ctx context.Context, imageURL, b64 string) ([]byte, error) {
	if b64 != "" {
		return base64.StdEncoding.DecodeString(b64)
	}
	if imageURL != "" {
		return m.fetchImageFromURL(ctx, imageURL)
	}
	return nil, fmt.Errorf("image result is empty")
}

func (m *MachGenProvider) callPollinationsStream(
	ctx context.Context,
	promptText string,
	modelName string,
	width, height int,
) ([]byte, error) {
	// Clean prompt: strip image edit instruction trailers so FLUX receives a pure visual description
	cleanPrompt := promptText
	if idx := strings.Index(cleanPrompt, "\n\nThe supplied edit image"); idx != -1 {
		cleanPrompt = strings.TrimSpace(cleanPrompt[:idx])
	}
	if idx := strings.Index(cleanPrompt, "\n\n"); idx != -1 && len(cleanPrompt) > 200 {
		cleanPrompt = strings.TrimSpace(cleanPrompt[:idx])
	}

	// Model mapping for Pollinations:
	// Pollinations supports "flux", "flux-realism", "turbo". It does not support "gpt-image-2".
	pollinationsModel := "flux"
	lowerModel := strings.ToLower(modelName)
	if strings.Contains(lowerModel, "turbo") {
		pollinationsModel = "turbo"
	} else if strings.Contains(lowerModel, "realism") {
		pollinationsModel = "flux-realism"
	}

	// Dynamic seed for distinct artistic synthesis
	seed := (time.Now().UnixNano() / 1000) % 100000000

	encodedPrompt := url.PathEscape(cleanPrompt)
	targetURL := fmt.Sprintf("https://image.pollinations.ai/prompt/%s?width=%d&height=%d&model=%s&seed=%d&nologo=true&enhance=true",
		encodedPrompt, width, height, url.QueryEscape(pollinationsModel), seed)

	log.Printf("[MachGen] Calling Pollinations FLUX Engine (model=%s, seed=%d, length=%d chars)",
		pollinationsModel, seed, len(cleanPrompt))

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		log.Printf("[MachGen] Pollinations FLUX rate-limited (429), retrying with turbo model...")
		time.Sleep(500 * time.Millisecond)
		turboURL := fmt.Sprintf("https://image.pollinations.ai/prompt/%s?width=%d&height=%d&model=turbo&seed=%d&nologo=true",
			encodedPrompt, width, height, seed)
		turboReq, tErr := http.NewRequestWithContext(ctx, "GET", turboURL, nil)
		if tErr == nil {
			turboReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
			if tResp, dErr := m.httpClient.Do(turboReq); dErr == nil {
				defer tResp.Body.Close()
				if tResp.StatusCode >= 200 && tResp.StatusCode < 300 {
					if tData, rErr := io.ReadAll(tResp.Body); rErr == nil && len(tData) > 100 {
						return tData, nil
					}
				}
			}
		}
		return nil, fmt.Errorf("HTTP 429 from image server")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d from image server", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(data) < 100 {
		return nil, fmt.Errorf("empty image received from provider")
	}

	return data, nil
}

func (m *MachGenProvider) fetchImageFromURL(ctx context.Context, imgURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", imgURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch image URL HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
