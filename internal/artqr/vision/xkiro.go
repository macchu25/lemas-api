package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"xkiro-backend/internal/artqr/model"
)

type StyleAnalysisResult struct {
	Style               string         `json:"style"`
	Palette             []string       `json:"palette"`
	Composition         map[string]any `json:"composition"`
	Geometry            []string       `json:"geometry"`
	Lighting            string         `json:"lighting"`
	Texture             string         `json:"texture"`
	BrushDirection      string         `json:"brush_direction"`
	Contrast            string         `json:"contrast"`
	QRRegionDescription string         `json:"qr_region_description"`
	IntegrationStrategy []string       `json:"integration_strategy"`
	GeneratedPrompt     string         `json:"generated_prompt"`
}

type StyleAnalyzer interface {
	AnalyzeStyle(ctx context.Context, refImgBytes []byte, placement model.Placement) (*StyleAnalysisResult, error)
}

type XKiroVisionAnalyzer struct {
	BaseURL string
	APIKey  string
	Model   string
}

func NewXKiroVisionAnalyzer() *XKiroVisionAnalyzer {
	baseURL := strings.TrimRight(os.Getenv("XKIRO_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(os.Getenv("UPSTREAM_BASE_URL"), "/")
	}
	if baseURL == "" {
		baseURL = "https://api.xkiro.com/v1"
	}
	modelName := os.Getenv("XKIRO_VISION_MODEL")
	if modelName == "" {
		modelName = "deepseek/deepseek-v4-flash-vision-exp"
	}
	apiKey := os.Getenv("XKIRO_API_KEY")
	if apiKey == "" {
		keys := strings.Split(os.Getenv("UPSTREAM_API_KEYS"), ",")
		if len(keys) > 0 && strings.TrimSpace(keys[0]) != "" {
			apiKey = strings.TrimSpace(keys[0])
		}
	}

	return &XKiroVisionAnalyzer{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   modelName,
	}
}

func describeRegion(p model.Placement) string {
	vertical := "center"
	if p.Y < 0.33 {
		vertical = "top"
	} else if p.Y > 0.60 {
		vertical = "bottom"
	}

	horizontal := "center"
	if p.X < 0.33 {
		horizontal = "left"
	} else if p.X > 0.60 {
		horizontal = "right"
	}

	if vertical == "center" && horizontal == "center" {
		return "center"
	}
	if vertical == "center" {
		return horizontal
	}
	if horizontal == "center" {
		return vertical
	}
	return vertical + "-" + horizontal
}

func (a *XKiroVisionAnalyzer) AnalyzeStyle(ctx context.Context, refImgBytes []byte, placement model.Placement) (*StyleAnalysisResult, error) {
	if len(refImgBytes) == 0 {
		return nil, errors.New("empty reference image")
	}

	regionName := describeRegion(placement)
	base64Img := base64.StdEncoding.EncodeToString(refImgBytes)
	mimeType := http.DetectContentType(refImgBytes)
	if !strings.HasPrefix(mimeType, "image/") {
		mimeType = "image/jpeg"
	}

	systemInstruction := `You are an expert art director and ControlNet prompt engineer.
Analyze the provided reference artwork to extract its visual DNA and design a seamless Art QR integration.
The QR code is placed in the ` + regionName + ` area (normalized X: ` + fmt.Sprintf("%.2f", placement.X) + `, Y: ` + fmt.Sprintf("%.2f", placement.Y) + `, Size: ` + fmt.Sprintf("%.2f", placement.Size) + `).

You must respond with ONLY a valid JSON object adhering strictly to this schema:
{
  "style": "string (e.g. post-impressionist oil painting, cyberpunk digital art, vintage botanical watercolor)",
  "palette": ["color 1", "color 2", "color 3", "color 4"],
  "composition": {
    "main_subject": "string",
    "background": "string",
    "foreground": "string"
  },
  "geometry": ["feature 1", "feature 2"],
  "lighting": "string (e.g. dramatic rim lighting, soft ambient glow)",
  "texture": "string (e.g. thick impasto brush strokes, smooth matte paper)",
  "brush_direction": "string",
  "contrast": "string",
  "qr_region_description": "string (description of the scene around the ` + regionName + ` region)",
  "integration_strategy": ["actionable rule 1 for disguising QR modules with artwork textures", "actionable rule 2"],
  "generated_prompt": "string (a rich, evocative 50-80 word prompt describing the scene and integrating QR modules naturally into elements of the art, high aesthetic, no watermarks, no separate QR card)"
}`

	userPrompt := "Analyze this reference image and provide the JSON style extraction for the " + regionName + " QR placement."

	requestBody := map[string]any{
		"model": a.Model,
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": systemInstruction,
			},
			{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": userPrompt},
					map[string]any{
						"type": "image_url",
						"image_url": map[string]string{
							"url": fmt.Sprintf("data:%s;base64,%s", mimeType, base64Img),
						},
					},
				},
			},
		},
		"temperature":     0.2,
		"response_format": map[string]string{"type": "json_object"},
	}

	payloadBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	reqURL := a.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if a.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.APIKey)
	}

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xKiro vision request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xKiro vision returned status %d", resp.StatusCode)
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil || len(chatResp.Choices) == 0 {
		return nil, errors.New("invalid response from vision model")
	}

	rawContent := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	rawContent = strings.TrimPrefix(rawContent, "```json")
	rawContent = strings.TrimPrefix(rawContent, "```")
	rawContent = strings.TrimSuffix(rawContent, "```")
	rawContent = strings.TrimSpace(rawContent)

	var analysis StyleAnalysisResult
	if err := json.Unmarshal([]byte(rawContent), &analysis); err != nil {
		return nil, fmt.Errorf("failed to parse vision analysis JSON: %w (content: %s)", err, rawContent)
	}

	return &analysis, nil
}
