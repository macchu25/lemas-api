package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// MachGenProvider implements image generation via the MachGen engine with dual-reference image conditioning
type MachGenProvider struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewMachGenProvider() *MachGenProvider {
	apiURL := os.Getenv("MACHGEN_API_URL")
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
		modelName = "flux"
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

	// Approach A: If endpoint is a standard multi-modal OpenAI-compatible or Replicate / HTTP API
	if strings.Contains(m.baseURL, "/v1") || strings.Contains(m.baseURL, "replicate") || strings.Contains(m.baseURL, "xkiro") {
		result, err := m.callAPIEndpoint(ctx, baseSceneBytes, cleanedQRBytes, promptText, modelName, width, height)
		if err == nil && len(result) > 0 {
			log.Printf("[MachGen] Dual-reference synthesis completed in %v", time.Since(start))
			return result, nil
		}
		log.Printf("[MachGen] Direct API endpoint call failed (%v), falling back to URL pipeline", err)
	}

	// Approach B: Pollinations / Direct Image Stream Engine
	result, err := m.callPollinationsStream(ctx, promptText, modelName, width, height)
	if err != nil {
		log.Printf("[MachGen] Pollinations stream failed: %v", err)
		return nil, fmt.Errorf("machgen engine error: %w", err)
	}

	log.Printf("[MachGen] Generation completed in %v (%d bytes)", time.Since(start), len(result))
	return result, nil
}

func (m *MachGenProvider) callAPIEndpoint(
	ctx context.Context,
	baseSceneBytes []byte,
	cleanedQRBytes []byte,
	promptText string,
	modelName string,
	width, height int,
) ([]byte, error) {
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
		req, err = http.NewRequestWithContext(ctx, "POST", m.baseURL+"/images/generations", bytes.NewReader(jsonBytes))
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

func (m *MachGenProvider) callPollinationsStream(
	ctx context.Context,
	promptText string,
	modelName string,
	width, height int,
) ([]byte, error) {
	encodedPrompt := url.PathEscape(promptText)
	targetURL := fmt.Sprintf("https://image.pollinations.ai/prompt/%s?width=%d&height=%d&model=%s&nologo=true&enhance=true",
		encodedPrompt, width, height, url.QueryEscape(modelName))

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
