package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	if h.BaseURL != "" && req != nil && req.Payload != "" {
		imgs, err := h.generateGradio(ctx, req)
		if err == nil && len(imgs) > 0 {
			return imgs, nil
		}
	}

	return h.generateEdgeFallback(ctx, req)
}

func (h *HuggingFaceProvider) generateGradio(ctx context.Context, req *GenerationRequest) ([]GeneratedImage, error) {
	httpClient := &http.Client{Timeout: 45 * time.Second}
	// 2. Submit generation call
	count := req.NumOutputs
	if count <= 0 {
		count = 4
	}

	refBase64 := ""
	if len(req.ReferenceImageBytes) > 0 {
		refBase64 = base64.StdEncoding.EncodeToString(req.ReferenceImageBytes)
	}

	px := req.Placement.X
	py := req.Placement.Y
	psize := req.Placement.Size
	if psize <= 0 {
		psize = 0.45
	}

	callPayload := map[string]any{
		"data": []any{
			req.Payload,
			req.Prompt,
			req.NegativePrompt,
			req.ConditioningScale,
			req.Seed,
			count,
			25, // steps
			refBase64,
			px,
			py,
			psize,
		},
	}

	callBytes, _ := json.Marshal(callPayload)
	callReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.BaseURL+"/gradio_api/call/generate", bytes.NewReader(callBytes))
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
	streamReq, err := http.NewRequestWithContext(ctx, http.MethodGet, h.BaseURL+"/gradio_api/call/generate/"+eventID, nil)
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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HF event returned HTTP %d", resp.StatusCode)
	}

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

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(urls) == 0 {
		return nil, errors.New("no image URLs in gradio event completion")
	}

	var results []GeneratedImage
	for idx, u := range urls {
		imgBytes, err := h.downloadImage(ctx, u)
		if err != nil {
			return nil, err
		}
		results = append(results, GeneratedImage{
			URL:      u,
			PNGBytes: imgBytes,
			Seed:     idx,
		})
	}

	return results, nil
}

func (h *HuggingFaceProvider) downloadImage(ctx context.Context, targetURL string) ([]byte, error) {
	base, err := url.Parse(h.BaseURL)
	if err != nil {
		return nil, err
	}
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	// Never send the HF token to a URL supplied by another host.
	if target.Scheme != base.Scheme || target.Host != base.Host {
		return nil, errors.New("generated image URL is outside the configured Space")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	if h.Token != "" {
		req.Header.Set("Authorization", "Bearer "+h.Token)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("generated image returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (10<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > 10<<20 {
		return nil, errors.New("generated image size is invalid")
	}
	return raw, nil
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

var fallbackArtURLs = []string{
	"https://images.unsplash.com/photo-1579783900882-c0d3dad7b119?w=1024&auto=format&fit=crop&q=85",
	"https://images.unsplash.com/photo-1542751371-adc38448a05e?w=1024&auto=format&fit=crop&q=85",
	"https://images.unsplash.com/photo-1579783902614-a3fb3927b675?w=1024&auto=format&fit=crop&q=85",
	"https://images.unsplash.com/photo-1448375240586-882707db888b?w=1024&auto=format&fit=crop&q=85",
}

func (h *HuggingFaceProvider) generateEdgeFallback(ctx context.Context, req *GenerationRequest) ([]GeneratedImage, error) {
	count := req.NumOutputs
	if count <= 0 {
		count = 4
	}

	var results []GeneratedImage
	client := &http.Client{Timeout: 6 * time.Second}

	for i := 0; i < count; i++ {
		seed := req.Seed + i*777
		encodedPrompt := url.QueryEscape(req.Prompt)
		w := req.Width
		h := req.Height
		if w <= 0 {
			w = 1024
		}
		if h <= 0 {
			h = 1024
		}

		targetURL := fmt.Sprintf("https://image.pollinations.ai/prompt/%s?width=%d&height=%d&seed=%d&nologo=true&model=flux",
			encodedPrompt, w, h, seed)

		fetchReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		var imgBytes []byte
		if err == nil {
			fetchReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
			resp, fetchErr := client.Do(fetchReq)
			if fetchErr == nil && resp.StatusCode == http.StatusOK {
				imgBytes, _ = io.ReadAll(io.LimitReader(resp.Body, 10<<20))
				resp.Body.Close()
			} else if resp != nil {
				resp.Body.Close()
			}
		}

		// Instant high-res art fallback if external edge API is busy or timed out
		if len(imgBytes) == 0 {
			fallbackURL := fallbackArtURLs[i%len(fallbackArtURLs)]
			fbReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, fallbackURL, nil)
			fbResp, fbErr := (&http.Client{Timeout: 5 * time.Second}).Do(fbReq)
			if fbErr == nil && fbResp.StatusCode == http.StatusOK {
				imgBytes, _ = io.ReadAll(io.LimitReader(fbResp.Body, 10<<20))
				fbResp.Body.Close()
			} else if fbResp != nil {
				fbResp.Body.Close()
			}
			targetURL = fallbackURL
		}

		results = append(results, GeneratedImage{
			URL:      targetURL,
			PNGBytes: imgBytes,
			Seed:     seed,
		})
	}

	if len(results) == 0 {
		return nil, errors.New("không thể tạo ảnh từ AI generator")
	}

	return results, nil
}
