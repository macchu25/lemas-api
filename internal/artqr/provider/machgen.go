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
		apiURL = "https://apigiare.vn/v1"
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

func (m *MachGenProvider) GetModel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.model
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

// EndpointConfig specifies upstream endpoint parameters for image synthesis
type EndpointConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	Name    string
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
	if modelName == "" {
		modelName = "gpt-image-2"
	}
	candidates := []EndpointConfig{
		{
			BaseURL: m.baseURL,
			APIKey:  m.apiKey,
			Model:   modelName,
			Name:    "Primary MachGen/API",
		},
	}
	return m.GenerateWithCandidates(ctx, baseSceneBytes, cleanedQRBytes, promptText, width, height, candidates)
}

// GenerateWithCandidates tries each candidate API endpoint with gpt-image-2 sequentially.
// If apigiare runs out of quota, it auto-swaps to the next candidate (MachGen) with gpt-image-2.
// Only if all configured endpoints fail does it fallback to the free engine.
func (m *MachGenProvider) GenerateWithCandidates(
	ctx context.Context,
	baseSceneBytes []byte,
	cleanedQRBytes []byte,
	promptText string,
	width, height int,
	candidates []EndpointConfig,
) ([]byte, error) {
	if width <= 0 {
		width = 1024
	}
	if height <= 0 {
		height = 1024
	}

	start := time.Now()

	// Reset fallback status for this generation request
	m.mu.Lock()
	m.lastWasFallback = false
	m.fallbackReason = ""
	m.mu.Unlock()

	// Approach A: Try all candidate endpoints with gpt-image-2
	for i, cand := range candidates {
		candBase := strings.TrimRight(strings.TrimSpace(cand.BaseURL), "/")
		candKey := strings.TrimSpace(cand.APIKey)
		candModel := strings.TrimSpace(cand.Model)
		if candModel == "" {
			candModel = "gpt-image-2"
		}

		if candKey == "" && !strings.Contains(candBase, "pollinations") && candBase == "" {
			continue
		}

		log.Printf("[MachGen] Attempting candidate #%d: [%s] (model=%s, url=%s)", i+1, cand.Name, candModel, candBase)

		// Configure provider with this candidate
		m.Configure(candBase, candKey, candModel)

		result, err := m.callAPIEndpoint(ctx, baseSceneBytes, cleanedQRBytes, promptText, candModel, width, height)
		if err == nil && len(result) > 0 {
			if i > 0 {
				m.mu.Lock()
				m.lastWasFallback = true
				m.fallbackReason = fmt.Sprintf("Đã tự động chuyển sang %s (model: %s)", cand.Name, candModel)
				m.mu.Unlock()
				log.Printf("[MachGen] 🔄 Successfully swapped to candidate #%d [%s] (model: %s) in %v",
					i+1, cand.Name, candModel, time.Since(start))
			} else {
				log.Printf("[MachGen] Dual-reference synthesis completed with primary endpoint [%s] (model: %s) in %v",
					cand.Name, candModel, time.Since(start))
			}
			return result, nil
		}

		log.Printf("[MachGen] ⚠️ Candidate #%d [%s] failed: %v", i+1, cand.Name, err)
	}

	// All candidate gpt-image-2 endpoints failed or ran out of quota.
	// As requested: Do NOT use ANY free fallback or mock canvas. Stop immediately and notify user of system interruption.
	log.Printf("[MachGen] ❌ All gpt-image-2 candidate endpoints failed or exhausted quota. Returning system interruption error.")
	return nil, fmt.Errorf("Hệ thống tạo ảnh AI (gpt-image-2) tạm thời gián đoạn do hạn ngạch dịch vụ đã hết. Vui lòng liên hệ Quản trị viên để kiểm tra và nạp thêm số dư API.")
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
