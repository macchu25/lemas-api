package prompt

import (
	"fmt"
	"strings"

	"xkiro-backend/internal/artqr/model"
	"xkiro-backend/internal/artqr/vision"
)

var StandardNegativePrompt = "white lace, white cloth, white dots, white beads, white paper, square sticker, barcode border, digital qr stamp, paper card, flat, blurry, low quality, pixelated, washed out, unscannable QR, corrupted QR, incorrect QR modules, distorted finder patterns, missing modules, extra modules, warped grid, visible border, text, logo, watermark"

var DefaultPresets = []model.ArtQRPreset{
	{
		ID:          "starry-night",
		Slug:        "starry-night",
		Name:        "Starry Night",
		Description: "Sơn dầu cobalt huyền thoại, bầu trời xoáy và ánh sao vàng rực rỡ",
		PreviewURL:  "https://images.unsplash.com/photo-1579783900882-c0d3dad7b119?w=600&auto=format&fit=crop&q=80",
		Colors:      []string{"#0b1e3f", "#1d4ed8", "#eab308"},
		Prompt:      "Van Gogh Starry Night, swirling deep blue night sky, luminous golden stars, crescent moon, cypress tree, masterpiece oil painting",
		NegativePrompt:    "bad quality, low resolution, blurry, distorted, messy, low contrast, text, watermark, white card, white box, sticker, label, frame",
		ConditioningScale: 1.45,
		GuidanceScale:     7.5,
		Width:             1024,
		Height:            1024,
		Enabled:           true,
	},
	{
		ID:          "cyberpunk",
		Slug:        "cyberpunk",
		Name:        "Cyberpunk Metropolis",
		Description: "Thành phố tương lai đêm mưa, biển neon rực rỡ và ánh phản chiếu công nghệ cao",
		PreviewURL:  "https://images.unsplash.com/photo-1542751371-adc38448a05e?w=600&auto=format&fit=crop&q=80",
		Colors:      []string{"#090d16", "#06b6d4", "#ec4899"},
		Prompt:      "Futuristic cyberpunk metropolis at night, rainy street reflections, glowing cyan and magenta neon signs, cinematic volumetric lighting",
		NegativePrompt:    "bad quality, low resolution, blurry, low contrast, text, watermark, white card, white box, sticker, frame",
		ConditioningScale: 1.45,
		GuidanceScale:     7.5,
		Width:             1024,
		Height:            1024,
		Enabled:           true,
	},
	{
		ID:          "watercolor",
		Slug:        "watercolor",
		Name:        "Botanical Watercolor",
		Description: "Lá cây ngọc bích, sắc hoa mềm mại trên nền giấy mỹ thuật cổ điển",
		PreviewURL:  "https://images.unsplash.com/photo-1579783902614-a3fb3927b675?w=600&auto=format&fit=crop&q=80",
		Colors:      []string{"#fbf9f5", "#047857", "#6366f1"},
		Prompt:      "Botanical watercolor artwork, soft emerald green leaves, indigo floral blooms, gentle pastel washes, fine art paper",
		NegativePrompt:    "bad quality, blurry, dark, low contrast, digital artifacts, text, watermark, white box, card, sticker",
		ConditioningScale: 1.40,
		GuidanceScale:     7.0,
		Width:             1024,
		Height:            1024,
		Enabled:           true,
	},
	{
		ID:          "forest",
		Slug:        "forest",
		Name:        "Mystic Forest",
		Description: "Rừng sương mù huyền bí, đại cổ thụ ngàn năm và ánh nắng vàng xuyên tán",
		PreviewURL:  "https://images.unsplash.com/photo-1448375240586-882707db888b?w=600&auto=format&fit=crop&q=80",
		Colors:      []string{"#061a14", "#15803d", "#ca8a04"},
		Prompt:      "((a small solitary human traveler standing from behind in awe:1.35)), ((lush emerald green forest floor with vibrant red wildflowers:1.3)), a majestic gigantic ancient tree with mossy twisting branches, volumetric golden god rays breaking through lush canopy, soft glowing pale mist, rich atmospheric fantasy realism, vibrant colorful masterpiece, 8k resolution",
		NegativePrompt: "((monochrome, 2d silhouette, plain black and white silhouette, flat yellow sky, flat background:1.4)), visible QR overlay, obvious black and white QR blocks, pasted QR code, barcode look, flat geometric grid, artificial square pattern, broken finder patterns, text, watermark, logo, cartoon, anime, blurry details, flat lighting",
		Placement: &model.Placement{
			X:    0.166,
			Y:    0.171,
			Size: 0.654,
		},
		ConditioningScale: 1.40,
		GuidanceScale:     7.5,
		Width:             1024,
		Height:            1024,
		Enabled:           true,
	},
	{
		ID:          "enchanted-ancient-tree",
		Slug:        "enchanted-ancient-tree",
		Name:        "Cổ Thụ Thần Thoại",
		Description: "Rừng sương mù điện ảnh, đại cổ thụ ngàn năm, luồng sáng vàng và thảm hoa dại đỏ bí ẩn",
		PreviewURL:  "https://images.unsplash.com/photo-1511497584788-87676104235f?w=600&auto=format&fit=crop&q=80",
		Colors:      []string{"#0d2319", "#d97706", "#dc2626"},
		Prompt:      "((a small solitary human traveler standing from behind in awe:1.35)), ((lush emerald green forest floor with vibrant red wildflowers:1.3)), a monumental ancient tree with mossy bark, dramatic volumetric golden god rays breaking through canopy, pale glowing mist, rich natural colors, cinematic fantasy realism, 8k resolution",
		NegativePrompt: "((monochrome, 2d silhouette, plain black and white silhouette, flat yellow sky, sunset:1.4)), visible QR overlay, obvious black and white QR blocks, pasted QR code, barcode look, flat geometric grid, artificial square pattern, broken finder patterns, text, watermark, logo, cartoon, anime, blurry details, flat lighting",
		Placement: &model.Placement{
			X:    0.166,
			Y:    0.171,
			Size: 0.654,
		},
		ConditioningScale: 1.40,
		GuidanceScale:     7.5,
		Width:             1024,
		Height:            1024,
		Enabled:           true,
	},
}

// BuildPrompt creates a complete placement-aware prompt for the QR generation
func BuildPrompt(preset *model.ArtQRPreset, analysis *vision.StyleAnalysisResult, placement model.Placement) (string, string) {
	var basePrompt string
	negativePrompt := StandardNegativePrompt

	if analysis != nil && analysis.GeneratedPrompt != "" {
		paletteStr := strings.Join(analysis.Palette, ", ")
		basePrompt = fmt.Sprintf(
			"Masterpiece %s composition, %s. Color palette: %s. Texture: %s. Lighting: %s. "+
				"Preserve the exact character, person, clothes, textures, folds, and details of the image.",
			analysis.Style,
			analysis.GeneratedPrompt,
			paletteStr,
			analysis.Texture,
			analysis.Lighting,
		)
	} else if preset != nil {
		basePrompt = preset.Prompt
		if preset.NegativePrompt != "" {
			negativePrompt = preset.NegativePrompt
		}
	} else {
		basePrompt = DefaultPresets[0].Prompt
	}

	fullPrompt := strings.TrimSpace(basePrompt)
	return fullPrompt, negativePrompt
}
