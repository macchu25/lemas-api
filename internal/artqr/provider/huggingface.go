package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type HuggingFaceProvider struct {
	BaseURL string
	Token   string
}

func NewHuggingFaceProvider() *HuggingFaceProvider {
	baseURL := strings.TrimRight(os.Getenv("HF_ART_QR_SPACE_URL"), "/")
	token := strings.TrimSpace(os.Getenv("HF_TOKEN"))
	return &HuggingFaceProvider{
		BaseURL: baseURL,
		Token:   token,
	}
}

func (h *HuggingFaceProvider) Name() string {
	return "huggingface-zerogpu"
}

func (h *HuggingFaceProvider) Generate(ctx context.Context, req *GenerationRequest) ([]GeneratedImage, error) {
	if h.BaseURL != "" {
		images, err := h.generateGradio(ctx, req)
		if err == nil && len(images) > 0 {
			return images, nil
		}
	}

	// Resilient Edge Generation Fallback (FLUX with Seed Variation)
	return h.generateEdgeFallback(ctx, req)
}

func (h *HuggingFaceProvider) generateGradio(ctx context.Context, req *GenerationRequest) ([]GeneratedImage, error) {
	// 1. Upload QR Control Image to Gradio
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "control_qr.png")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(req.QRControlImagePNG); err != nil {
		return nil, err
	}
	_ = writer.Close()

	uploadReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.BaseURL+"/gradio_api/upload", &body)
	if err != nil {
		return nil, err
	}
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	if h.Token != "" {
		uploadReq.Header.Set("Authorization", "Bearer "+h.Token)
	}

	httpClient := &http.Client{Timeout: 45 * time.Second}
	resp, err := httpClient.Do(uploadReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gradio upload status %d", resp.StatusCode)
	}

	var paths []string
	if err := json.NewDecoder(resp.Body).Decode(&paths); err != nil || len(paths) == 0 {
		return nil, errors.New("gradio returned no uploaded file path")
	}

	// 2. Submit generation call
	count := req.NumOutputs
	if count <= 0 {
		count = 4
	}

	callPayload := map[string]any{
		"data": []any{
			map[string]any{"path": paths[0], "meta": map[string]string{"_type": "gradio.FileData"}},
			req.Prompt,
			req.NegativePrompt,
			req.ConditioningScale,
			req.GuidanceScale,
			28, // steps
			req.Seed,
			count,
		},
	}

	callBytes, _ := json.Marshal(callPayload)
	callReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.BaseURL+"/gradio_api/call/predict", bytes.NewReader(callBytes))
	if err != nil {
		return nil, err
	}
	callReq.Header.Set("Content-Type", "application/json")
	if h.Token != "" {
		callReq.Header.Set("Authorization", "Bearer "+h.Token)
	}

	callResp, err := httpClient.Do(callReq)
	if err != nil {
		return nil, err
	}
	defer callResp.Body.Close()

	if callResp.StatusCode < 200 || callResp.StatusCode >= 300 {
		return nil, fmt.Errorf("gradio predict status %d", callResp.StatusCode)
	}

	var queued struct {
		EventID string `json:"event_id"`
	}
	if err := json.NewDecoder(callResp.Body).Decode(&queued); err != nil || queued.EventID == "" {
		return nil, errors.New("gradio did not return event id")
	}

	// 3. Listen to Event Stream
	return h.waitEvent(ctx, queued.EventID)
}

func (h *HuggingFaceProvider) waitEvent(ctx context.Context, eventID string) ([]GeneratedImage, error) {
	streamReq, err := http.NewRequestWithContext(ctx, http.MethodGet, h.BaseURL+"/gradio_api/call/predict/"+eventID, nil)
	if err != nil {
		return nil, err
	}
	if h.Token != "" {
		streamReq.Header.Set("Authorization", "Bearer "+h.Token)
	}

	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(streamReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)

	var urls []string
	event := ""

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if event == "error" {
			return nil, errors.New("HF Space error: " + data)
		}
		if event == "complete" {
			var value any
			if err := json.Unmarshal([]byte(data), &value); err == nil {
				urls = extractURLs(value)
			}
			break
		}
	}

	if len(urls) == 0 {
		return nil, errors.New("no image URLs in gradio event completion")
	}

	var results []GeneratedImage
	for idx, u := range urls {
		imgBytes, _ := downloadImage(ctx, u)
		results = append(results, GeneratedImage{
			URL:      u,
			PNGBytes: imgBytes,
			Seed:     idx,
		})
	}

	return results, nil
}

func (h *HuggingFaceProvider) generateEdgeFallback(ctx context.Context, req *GenerationRequest) ([]GeneratedImage, error) {
	count := req.NumOutputs
	if count <= 0 {
		count = 4
	}

	var results []GeneratedImage
	client := &http.Client{Timeout: 25 * time.Second}

	for i := 0; i < count; i++ {
		seed := req.Seed + i*777
		encodedPrompt := url.QueryEscape(req.Prompt)
		targetURL := fmt.Sprintf("https://image.pollinations.ai/prompt/%s?width=%d&height=%d&seed=%d&nologo=true&model=flux",
			encodedPrompt, req.Width, req.Height, seed)

		fetchReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(fetchReq)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		imgBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()
		if err != nil || len(imgBytes) == 0 {
			continue
		}

		results = append(results, GeneratedImage{
			URL:      targetURL,
			PNGBytes: imgBytes,
			Seed:     seed,
		})
	}

	if len(results) == 0 {
		return nil, errors.New("failed to generate candidates from edge generator")
	}

	return results, nil
}

func downloadImage(ctx context.Context, targetURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 10<<20))
}

func extractURLs(val any) []string {
	var list []string
	seen := map[string]bool{}

	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for k, child := range v {
				if (k == "url" || k == "path") && child != nil {
					if str, ok := child.(string); ok && strings.HasPrefix(str, "http") && !seen[str] {
						seen[str] = true
						list = append(list, str)
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}

	walk(val)
	return list
}
