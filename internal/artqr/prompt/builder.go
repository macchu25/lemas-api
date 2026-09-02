package prompt

import (
	"fmt"
	"strings"

	"xkiro-backend/internal/artqr/model"
	"xkiro-backend/internal/artqr/vision"
)

var StandardNegativePrompt = "unscannable QR, corrupted QR, incorrect QR modules, distorted finder patterns, missing modules, extra modules, warped grid, QR sticker, QR overlay, white QR box, visible border, text, logo, watermark, blurry QR structure, low contrast, oversaturated noise, cropped composition"

var DefaultPresets = []model.ArtQRPreset{
	{
		ID:          "starry-night",
		Slug:        "starry-night",
		Name:        "Starry Night",
		Description: "Sơn dầu cobalt huyền thoại, bầu trời xoáy và ánh sao vàng rực rỡ",
		PreviewURL:  "https://images.unsplash.com/photo-1579783900882-c0d3dad7b119?w=600&auto=format&fit=crop&q=80",
		Colors:      []string{"#0b1e3f", "#1d4ed8", "#eab308"},
		Prompt: `Create an artistic QR code seamlessly integrated into a Starry Night inspired oil painting.

The QR code structure must remain geometrically accurate and fully machine-scannable. Preserve the exact QR module positions and all three finder patterns from the control image.

Do NOT draw a QR code on top of a painting. Instead, make the painting itself form the QR code.

Transform dark QR modules naturally into deep navy and ultramarine brush strokes, shadows, cypress branches, rooftops, hills and dark parts of the night sky.

Transform light QR areas into swirling blue-white clouds, glowing golden stars, moonlight and illuminated brush strokes.

Continuous flowing brush strokes should pass naturally through and around the QR structure so that the square grid is visually disguised.

Integrate the three QR finder patterns into the composition as natural visual structures while preserving their exact geometry and strong contrast for scanning.

Composition:
- dramatic swirling night sky
- large flowing spiral across the center
- luminous golden stars
- bright moon in the upper-right
- tall dark cypress on the left
- small European village along the bottom
- church steeple near the center
- rolling blue hills in the distance

Thick expressive oil paint, visible impasto texture, energetic curved brush strokes, deep cobalt blue, ultramarine, navy, turquoise, cream and luminous yellow.

From a distance it should look primarily like an expressive night landscape painting.
The QR structure should only become noticeable on closer inspection.

CRITICAL:
Preserve the control QR geometry.
Do not invent, move, remove, merge or add QR modules.
Maintain sufficient luminance contrast between dark and light QR regions.
Prioritize QR scan reliability while maximizing artistic integration.
No text, no logos, no borders, no separate QR card.`,
		NegativePrompt:    StandardNegativePrompt,
		ConditioningScale: 1.35,
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
		Prompt: `Create an artistic QR code seamlessly integrated into a cinematic cyberpunk city at night.
The QR code structure must remain geometrically accurate and fully machine-scannable. Preserve the exact QR module positions and all three finder patterns from the control image.
Do NOT draw a QR code on top of a painting. Make the futuristic architecture form the QR code naturally.
Transform dark QR modules into dark skyscraper silhouettes, tinted glass windows, structural girders, and wet asphalt shadows.
Transform light QR areas into vibrant electric cyan, magenta neon billboards, glowing holographic signs, and illuminated street level storefronts.
Composition: towering futuristic skyscrapers, neon reflections on wet streets, high-tech flying vehicles in the background, octane render 8k.`,
		NegativePrompt:    StandardNegativePrompt,
		ConditioningScale: 1.38,
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
		Prompt: `Create an artistic QR code seamlessly integrated into a delicate botanical watercolor artwork.
The QR code structure must remain geometrically accurate and fully machine-scannable. Preserve the exact QR module positions and all three finder patterns from the control image.
Do NOT draw a QR code on top of a painting. Form the QR structure organically out of natural botanical elements.
Transform dark QR modules into rich emerald leaves, dark indigo petals, botanical branches, and concentrated watercolor pigment blooms.
Transform light QR areas into soft pastel washes, delicate cream floral highlights, and warm textured cotton paper background.
Visible natural watercolor granulation, soft bleeding edges, elegant organic composition.`,
		NegativePrompt:    StandardNegativePrompt,
		ConditioningScale: 1.36,
		GuidanceScale:     7.0,
		Width:             1024,
		Height:            1024,
		Enabled:           true,
	},
	{
		ID:          "forest",
		Slug:        "forest",
		Name:        "Mystic Forest",
		Description: "Rừng sương mù huyền bí, ánh nắng xuyên tán cây cổ thụ",
		PreviewURL:  "https://images.unsplash.com/photo-1448375240586-882707db888b?w=600&auto=format&fit=crop&q=80",
		Colors:      []string{"#061a14", "#15803d", "#ca8a04"},
		Prompt: `Create an artistic QR code seamlessly integrated into an ethereal enchanted ancient forest.
The QR code structure must remain geometrically accurate and fully machine-scannable. Preserve the exact QR module positions and all three finder patterns from the control image.
Do NOT draw a QR code on top of a painting.
Transform dark QR modules into mossy ancient oak bark, deep forest shadows, ferns, and dark stone paths.
Transform light QR areas into glowing morning sunbeams, golden fireflies, floating pollen, and illuminated misty canopy openings.
Lush deep greens, earthy umber, radiant golden volumetric god-rays, cinematic atmosphere.`,
		NegativePrompt:    StandardNegativePrompt,
		ConditioningScale: 1.35,
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
			"Create an artistic QR code seamlessly integrated into a %s style composition. "+
				"%s. Color palette: %s. Texture: %s. Lighting: %s. "+
				"The QR structure must remain geometrically accurate and fully machine-scannable. "+
				"Do NOT draw a separate QR card or sticker on top. Disguise the modules organically into the artwork.",
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

	// Placement context enrichment
	var placementContext string
	if placement.Size < 0.85 {
		posX := "center"
		if placement.X < 0.33 {
			posX = "left"
		} else if placement.X > 0.60 {
			posX = "right"
		}

		posY := "center"
		if placement.Y < 0.33 {
			posY = "upper"
		} else if placement.Y > 0.60 {
			posY = "lower"
		}

		placementContext = fmt.Sprintf(
			" Placement awareness: The QR code is situated in the %s-%s section of the frame. "+
				"Harmoniously integrate the QR modules into the surrounding scene elements in that specific area without moving the grid coordinates.",
			posY,
			posX,
		)
	}

	fullPrompt := strings.TrimSpace(basePrompt + placementContext)
	return fullPrompt, negativePrompt
}
