package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"xkiro-backend/internal/artqr/model"
)

type StyleAnalysisResult struct {
	Style               string         `json:"style"`
	SubjectDetails      map[string]any `json:"subject_details,omitempty"`
	Palette             []string       `json:"palette"`
	Composition         map[string]any `json:"composition,omitempty"`
	Lighting            string         `json:"lighting"`
	Texture             string         `json:"texture"`
	Contrast            string         `json:"contrast,omitempty"`
	QRRegionAnalysis    map[string]any `json:"qr_region_analysis,omitempty"`
	QRRegionDescription string         `json:"qr_region_description,omitempty"`
	IntegrationStrategy []string       `json:"integration_strategy,omitempty"`
	PatchPrompt         string         `json:"patch_prompt,omitempty"`
	GeneratedPrompt     string         `json:"generated_prompt"`
	RawJSON             string         `json:"raw_json,omitempty"`
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
	apiKey := strings.TrimSpace(os.Getenv("XKIRO_API_KEY"))
	modelName := strings.TrimSpace(os.Getenv("XKIRO_VISION_MODEL"))
	if modelName == "" {
		modelName = "gpt-4o"
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

func cropPatch(refImgBytes []byte, p model.Placement) []byte {
	img, _, err := image.Decode(bytes.NewReader(refImgBytes))
	if err != nil {
		return nil
	}
	bounds := img.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()
	if origW == 0 || origH == 0 {
		return nil
	}
	minDim := origW
	if origH < minDim {
		minDim = origH
	}
	pSize := int(p.Size * float64(minDim))
	if pSize < 64 {
		pSize = minDim / 2
	}
	px0 := int(p.X * float64(origW))
	py0 := int(p.Y * float64(origH))
	if px0 < 0 {
		px0 = 0
	}
	if py0 < 0 {
		py0 = 0
	}
	if px0+pSize > origW {
		pSize = origW - px0
	}
	if py0+pSize > origH {
		pSize = origH - py0
	}

	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := img.(subImager); ok {
		sub := si.SubImage(image.Rect(bounds.Min.X+px0, bounds.Min.Y+py0, bounds.Min.X+px0+pSize, bounds.Min.Y+py0+pSize))
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, sub, &jpeg.Options{Quality: 92}); err == nil {
			return buf.Bytes()
		}
	}
	return nil
}

func (a *XKiroVisionAnalyzer) getKeys() []string {
	var keys []string
	if a.APIKey != "" {
		keys = append(keys, a.APIKey)
	}
	if envKey := strings.TrimSpace(os.Getenv("XKIRO_API_KEY")); envKey != "" {
		keys = append(keys, envKey)
	}
	if raw := os.Getenv("UPSTREAM_API_KEYS"); raw != "" {
		for _, k := range strings.Split(raw, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

func (a *XKiroVisionAnalyzer) getBaseURL() string {
	if u := strings.TrimSpace(os.Getenv("XKIRO_BASE_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	if a.BaseURL != "" && a.BaseURL != "https://api.xkiro.com/v1" {
		return a.BaseURL
	}
	if u := strings.TrimSpace(os.Getenv("UPSTREAM_BASE_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://apigiare.vn/v1"
}

func (a *XKiroVisionAnalyzer) getModel() string {
	if m := strings.TrimSpace(os.Getenv("XKIRO_VISION_MODEL")); m != "" {
		return m
	}
	if a.Model != "" && a.Model != "deepseek/deepseek-v4-flash-vision-exp" {
		return a.Model
	}
	return "gpt-4o"
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

	patchBytes := cropPatch(refImgBytes, placement)
	patchBase64 := ""
	if len(patchBytes) > 0 {
		patchBase64 = base64.StdEncoding.EncodeToString(patchBytes)
	}

	systemInstruction := `You are an expert art director, forensic visual AI, and ControlNet QR prompt engineer.
Analyze the provided artwork in exhaustive forensic detail and construct a precision JSON breakdown for seamless ControlNet QR embedding.
The QR placement target is in the ` + regionName + ` region (normalized coordinates: X=` + fmt.Sprintf("%.2f", placement.X) + `, Y=` + fmt.Sprintf("%.2f", placement.Y) + `, Size=` + fmt.Sprintf("%.2f", placement.Size) + `).

Respond with a strictly formatted, rich JSON object with this exact schema:
{
  "style": "Exact artistic genre, historical medium (e.g. 19th Century Royal Military Oil Portrait, Impasto canvas, Baroque Chiaroscuro)",
  "subject_details": {
    "identity": "Exhaustive description of subject, facial structure, gaze, haircut, costume, uniform collars, shoulder epaulets, cords, medals, ribbons",
    "wardrobe_material": "Detailed fabric types (heavy velvet, gold bullion embroidery, braided aguillette, silk sash, metallic badges)",
    "color_scheme": "Color accents (navy blue, crimson scarlet, burnished gold, antique bronze)",
    "protected_regions": "Face, eyes, hair, posture, and facial expression must remain 100% untouched"
  },
  "palette": ["#hex1", "#hex2", "#hex3", "#hex4", "#hex5"],
  "composition": {
    "main_subject": "Position, framing, and focal points",
    "background": "Atmospheric background, vignette lighting, dark textured canvas",
    "foreground": "Details of foreground clothing and adornments"
  },
  "lighting": "Precise lighting direction, key light, warm ambient bounce, rim light highlights on metallic epaulets and soft facial shadows",
  "texture": "Visible brushwork, canvas weave, fabric stitching, metallic reflections, skin pores",
  "contrast": "Luminance ratio between highlights and deep shadows",
  "qr_region_analysis": {
    "target_surface": "Exact physical area where QR is placed (e.g. embroidered navy jacket chest with diagonal sash)",
    "local_textures": "Texture features in this specific crop (gold rope braids, medal ribbons, velvet folds)",
    "camouflage_technique": "How to weave QR modules naturally into fabric folds, golden embroidery threads, and chiaroscuro shadows"
  },
  "integration_strategy": [
    "Carve dark QR finder eyes and modules into the deep shadows of the fabric folds and navy cloth",
    "Transform light QR modules into shimmering gold thread highlights and reflection points",
    "Blend QR borders naturally along the curves of the cords and ribbons"
  ],
  "patch_prompt": "Intricate gold bullion embroidery, military uniform woven fabric texture, heavy navy cloth with gold threads and crimson sash, dramatic studio lighting, sharp contrasting weave, highly detailed masterwork craft",
  "generated_prompt": "Masterpiece portrait preserving the exact subject, posture, gold braided military uniform, and atmospheric lighting of the artwork with the QR code seamlessly woven into the embroidery and shadows"
}`

	userPrompt := "Analyze this reference image and target region for QR embedding. Image 1 is the full artwork. "
	userContents := []any{
		map[string]any{"type": "text", "text": userPrompt},
		map[string]any{
			"type": "image_url",
			"image_url": map[string]string{
				"url": fmt.Sprintf("data:%s;base64,%s", mimeType, base64Img),
			},
		},
	}
	if patchBase64 != "" {
		userContents = append(userContents,
			map[string]any{"type": "text", "text": "Image 2 is the magnified crop of the target insertion region for micro-pixel analysis:"},
			map[string]any{
				"type": "image_url",
				"image_url": map[string]string{
					"url": fmt.Sprintf("data:image/jpeg;base64,%s", patchBase64),
				},
			},
		)
	}

	requestBody := map[string]any{
		"model": a.getModel(),
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": systemInstruction,
			},
			{
				"role":    "user",
				"content": userContents,
			},
		},
		"temperature":     0.2,
		"response_format": map[string]string{"type": "json_object"},
	}

	payloadBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	keys := a.getKeys()
	if len(keys) == 0 {
		return nil, errors.New("không tìm thấy UPSTREAM_API_KEYS trong cấu hình .env")
	}

	client := &http.Client{Timeout: 45 * time.Second}
	var lastErr error

	for _, key := range keys {
		reqURL := a.getBaseURL() + "/chat/completions"
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payloadBytes))
		if reqErr != nil {
			lastErr = reqErr
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)

		resp, doErr := client.Do(req)
		if doErr != nil {
			lastErr = doErr
			continue
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("xKiro vision returned status %d: %s", resp.StatusCode, string(bodyBytes))
			continue
		}

		var chatResp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}

		if unmarshalErr := json.Unmarshal(bodyBytes, &chatResp); unmarshalErr != nil {
			lastErr = unmarshalErr
			continue
		}

		if len(chatResp.Choices) == 0 || chatResp.Choices[0].Message.Content == "" {
			lastErr = errors.New("empty response content from vision model")
			continue
		}

		rawJSON := chatResp.Choices[0].Message.Content
		rawJSON = strings.TrimSpace(rawJSON)
		if strings.HasPrefix(rawJSON, "```json") {
			rawJSON = strings.TrimPrefix(rawJSON, "```json")
			rawJSON = strings.TrimSuffix(rawJSON, "```")
			rawJSON = strings.TrimSpace(rawJSON)
		} else if strings.HasPrefix(rawJSON, "```") {
			rawJSON = strings.TrimPrefix(rawJSON, "```")
			rawJSON = strings.TrimSuffix(rawJSON, "```")
			rawJSON = strings.TrimSpace(rawJSON)
		}

		var result StyleAnalysisResult
		if parseErr := json.Unmarshal([]byte(rawJSON), &result); parseErr != nil {
			lastErr = parseErr
			continue
		}

		result.RawJSON = rawJSON
		if result.PatchPrompt == "" {
			result.PatchPrompt = fmt.Sprintf("Intricate %s texture, woven embroidery, subtle fabric folds, dramatic lighting highlights, masterwork craft, sharp contrast", result.Texture)
		}

		return &result, nil
	}

	return nil, lastErr
}
