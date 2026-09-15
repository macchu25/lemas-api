package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	_ "golang.org/x/image/webp"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"xkiro-backend/internal/artqr/model"
)

type StyleAnalysisResult struct {
	Style               string           `json:"style"`
	SceneDescription    string           `json:"scene_description,omitempty"`
	TargetSurface       string           `json:"target_surface,omitempty"`
	OptimalPlacement    *model.Placement `json:"optimal_placement,omitempty"`
	DarkModuleStyle     []string         `json:"dark_module_style,omitempty"`
	LightModuleStyle    string           `json:"light_module_style,omitempty"`
	SurfaceState        string           `json:"surface_state,omitempty"`
	SubjectDetails      map[string]any   `json:"subject_details,omitempty"`
	Palette             []string         `json:"palette"`
	Composition         map[string]any   `json:"composition,omitempty"`
	Lighting            string           `json:"lighting"`
	Texture             string           `json:"texture"`
	Contrast            string           `json:"contrast,omitempty"`
	QRRegionAnalysis    map[string]any   `json:"qr_region_analysis,omitempty"`
	QRRegionDescription string           `json:"qr_region_description,omitempty"`
	IntegrationStrategy []string         `json:"integration_strategy,omitempty"`
	PatchPrompt         string           `json:"patch_prompt,omitempty"`
	GeneratedPrompt     string           `json:"generated_prompt"`
	RawJSON             string           `json:"raw_json,omitempty"`
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
Analyze the provided artwork/image in exhaustive forensic detail and construct a precision JSON breakdown for seamless ControlNet QR embedding.
The user wants to place an authoritative QR code onto a prominent target surface in this artwork, scaling the QR so that it covers approximately 90% of the usable surface area (with ~5% safe margins around), preserving 100% of the QR matrix topology and relative module positions while stylizing the modules to blend into the material surface.
The default QR placement target is in the ` + regionName + ` region (normalized coordinates: X=` + fmt.Sprintf("%.2f", placement.X) + `, Y=` + fmt.Sprintf("%.2f", placement.Y) + `, Size=` + fmt.Sprintf("%.2f", placement.Size) + `).

Respond with a strictly formatted, rich JSON object with this exact schema:
{
  "style": "Chính xác thể loại nghệ thuật hoặc phong cách hình ảnh của bức ảnh (ví dụ: Nhiếp ảnh phong cảnh, Tranh sơn dầu cổ điển, Nghệ thuật điêu khắc đá, Cyberpunk Neon, Tranh màu nước...)",
  "scene_description": "Mô tả chi tiết và chính xác bằng tiếng Việt toàn bộ bối cảnh, vật thể chính, nhân vật, màu sắc, ánh sáng quan sát được trong bức ảnh được cung cấp",
  "target_surface": "Chính xác tên vật thể hoặc bề mặt vật lý trong ảnh nơi mã QR nên được hòa trộn lên (ví dụ: mặt phiến đá, thân cốc gốm, mặt trước áo khoác, mặt bàn gỗ, bức tường gạch, mặt biển hiệu...)",
  "optimal_placement": {
    "x": ` + fmt.Sprintf("%.2f", placement.X) + `,
    "y": ` + fmt.Sprintf("%.2f", placement.Y) + `,
    "size": ` + fmt.Sprintf("%.2f", placement.Size) + `
  },
  "dark_module_style": [
    "màu sắc và hiệu ứng hòa trộn phù hợp cho module tối theo đúng chất liệu bề mặt quan sát được",
    "hiệu ứng đổ bóng và vân bề mặt chân thực"
  ],
  "light_module_style": "màu sắc và hiệu ứng cho module sáng theo đúng bề mặt nền tự nhiên của vật thể trong ảnh",
  "surface_state": "Mô tả trạng thái hoàn thiện, độ bóng, độ nhám, màu sắc và chi tiết bề mặt vật thể trong ảnh bằng tiếng Việt",
  "palette": ["#hex1", "#hex2", "#hex3", "#hex4", "#hex5"],
  "lighting": "Hướng chiếu sáng, độ ấm và tương phản sáng tối trong ảnh",
  "texture": "Chi tiết vân chất liệu nhìn thấy trên bề mặt vật thể trong ảnh",
  "contrast": "Tương phản giữa vùng sáng và vùng tối",
  "generated_prompt": "Mô tả tổng thể phong cách và chất liệu bề mặt bằng tiếng Việt"
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
		req.Header.Set("User-Agent", "OpenAI/Python/1.42.0")
		req.Header.Set("Accept", "application/json")

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
