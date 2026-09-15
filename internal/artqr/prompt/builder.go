package prompt

import (
	"fmt"
	"strings"

	"xkiro-backend/internal/artqr/model"
	"xkiro-backend/internal/artqr/vision"
)

var StandardNegativePrompt = "white lace, white cloth, white dots, white beads, white paper, square sticker, barcode border, digital qr stamp, paper card, flat, blurry, low quality, pixelated, washed out, unscannable QR, corrupted QR, incorrect QR modules, distorted finder patterns, missing modules, extra modules, warped grid, visible border, text, logo, watermark"

const DoraemonBreadPrompt = `ABSOLUTE TOP PRIORITY — PRESERVE THE EXACT POSITION, SIZE, SHAPE, SPACING, AND DARK/LIGHT STATE OF EVERY QR MODULE FROM THE SECOND PROVIDED IMAGE. DO NOT MOVE, REDRAW, REGENERATE, WARP, MERGE, SPLIT, THIN, THICKEN, ROUND, BLUR, OR RESTRUCTURE ANY MODULE. ONLY CHANGE THE VISUAL SURFACE EFFECT INSIDE THE EXISTING MODULES.

The first provided image is a Doraemon-style cartoon scene showing a blue arm and a white rounded hand holding a square slice of bread in front of a red-and-yellow burst background.

The second provided image is the cleaned original valid QR code and is the ONLY authoritative QR structural source.

The QR module positions from the second provided image must remain unchanged.

TASK:
Place the exact QR module pattern from the second provided image onto the front face of the bread.

Make the QR look naturally toasted into the bread.

The module geometry must remain locked.

Preserve exactly:
- every module position
- every module size
- every module square boundary
- every dark/light state
- every finder pattern
- every timing pattern
- every alignment pattern
- every format/data region
- complete QR topology

DO NOT:
- generate a new QR
- redraw the QR
- approximate the QR
- move modules
- add modules
- remove modules
- merge modules
- split modules
- warp the QR
- bend the QR
- curve the QR
- skew the QR
- perspective-distort the QR
- blur module edges
- round module corners

ONLY THE VISUAL MATERIAL EFFECT MAY CHANGE.

For existing dark QR modules:
change their appearance from flat black into dark toasted-brown bread.

Allowed effects INSIDE existing dark module boundaries:
- dark golden-brown toast coloration
- deep toasted brown
- subtle baked color variation
- very mild bread grain
- tiny subtle bread pores
- subtle internal shading
- mild toasted surface texture

All material effects must remain strictly inside the existing dark module boundaries.

Do not allow texture to modify the module silhouette.

Do not allow:
- holes inside dark modules
- light gaps inside dark modules
- random burned marks outside dark modules
- connections between modules that were not connected originally
- texture that destroys module edges

Each dark module must still visually read as a solid dark QR module.

For light modules:
use the natural lighter bread surface.

Do not add dark texture that changes the light module state.

The QR should not look like a flat black sticker.

The QR should look like it belongs to the bread material.

However:
do not create the 3D effect through QR geometry.

Do not:
- emboss
- extrude
- raise modules
- deeply engrave modules
- curve the module grid

Create the integrated effect only using:
- color
- material
- lighting
- subtle internal shading

Keep the QR flush with the bread surface.

Maintain strong contrast between dark toasted modules and light bread.

Keep finder patterns especially clean and high-contrast.

Preserve the first image scene:
- blue arm
- white rounded hand
- bread shape
- bread position
- red-and-yellow burst background
- cartoon/anime style
- framing
- composition

Center the QR on the bread and keep even margins.

If visual aesthetics conflict with QR preservation, preserve QR structure.

If texture conflicts with module boundaries, reduce or remove the texture.

The target appearance is:

EXACT ORIGINAL QR MODULE POSITIONS
+
EXACT ORIGINAL DARK/LIGHT MAP
+
TOASTED BREAD MATERIAL INSIDE DARK MODULES ONLY
+
NO MODULE RESTRUCTURING`

var DefaultPresets = []model.ArtQRPreset{
	{
		ID:                  "bread_toast",
		Slug:                "bread_toast",
		Name:                "Bánh Mì Nướng Doraemon (Bread Toast)",
		Description:         "Mã QR nướng tự nhiên lên mặt lát bánh mì gối Doraemon, giữ nguyên vị trí module tuyệt đối",
		PreviewURL:          "/presets/doraemon_bread_scene.jpg",
		ReferenceImageURL:   "/presets/doraemon_bread_scene.jpg",
		PriceCredits:        5,
		Material:            "toasted_bread",
		DarkColor:           "#6b3218",
		TextureStrength:     0.12,
		ContrastStrength:    0.80,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#6b3218", "#fbe8c5", "#d97706"},
		Prompt:              DoraemonBreadPrompt,
		NegativePrompt:      "white sticker, paper border, embossed 3d, missing modules, broken finder patterns, distorted qr grid, holes in modules, extra modules, blur",
		ConditioningScale:   1.50,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
		Placement: &model.Placement{
			X:    0.41,
			Y:    0.28,
			Size: 0.30,
		},
	},
	{
		ID:                  "stone_engrave",
		Slug:                "stone_engrave",
		Name:                "Khắc Đá Cổ Thạch (Stone Engrave)",
		Description:         "Khắc chạm tinh xảo trên phiến đá hoa cương cổ, bóng râm tự nhiên và độ tương phản cao",
		PreviewURL:          "https://images.unsplash.com/photo-1590402494587-44b71d7772f6?w=600&auto=format&fit=crop&q=80",
		ReferenceImageURL:   "https://images.unsplash.com/photo-1590402494587-44b71d7772f6?w=1024&auto=format&fit=crop&q=80",
		PriceCredits:        5,
		Material:            "engraved_stone",
		DarkColor:           "#1e293b",
		TextureStrength:     0.15,
		ContrastStrength:    0.85,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#0f172a", "#334155", "#cbd5e1"},
		Prompt:              "Ancient stone slab engraving, deep carved slate rock surface, mineral texture, natural chiseled relief, dramatic side lighting",
		NegativePrompt:      "blurry, glossy plastic, extra modules, missing corners, warped grid, flat sticker",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
		Placement: &model.Placement{
			X:    0.25,
			Y:    0.25,
			Size: 0.50,
		},
	},
	{
		ID:                  "burned_wood",
		Slug:                "burned_wood",
		Name:                "Gỗ Cháy Pyrography (Burned Wood)",
		Description:         "Nghệ thuật khò lửa & khắc nhiệt trên thớ gỗ sồi tự nhiên, màu hổ phách khói",
		PreviewURL:          "https://images.unsplash.com/photo-1546484396-fb3fc6f95f98?w=600&auto=format&fit=crop&q=80",
		ReferenceImageURL:   "https://images.unsplash.com/photo-1546484396-fb3fc6f95f98?w=1024&auto=format&fit=crop&q=80",
		PriceCredits:        5,
		Material:            "burned_wood",
		DarkColor:           "#3b1a0a",
		TextureStrength:     0.14,
		ContrastStrength:    0.82,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#271206", "#78350f", "#fef3c7"},
		Prompt:              "Pyrography woodburning art on raw oak wood plank, charred timber grain, deep roasted dark brown wood burn, natural organic knots",
		NegativePrompt:      "white square, plastic, glossy, broken finder patterns, unscannable",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
		Placement: &model.Placement{
			X:    0.25,
			Y:    0.25,
			Size: 0.50,
		},
	},
	{
		ID:                  "metal_etch",
		Slug:                "metal_etch",
		Name:                "Kim Loại Khắc Axit (Metal Etch)",
		Description:         "Kim loại phay xước titan và đồng thau đúc cổ điển, tương phản bóng râm cao",
		PreviewURL:          "https://images.unsplash.com/photo-1533090161767-e6ffed986c88?w=600&auto=format&fit=crop&q=80",
		ReferenceImageURL:   "https://images.unsplash.com/photo-1533090161767-e6ffed986c88?w=1024&auto=format&fit=crop&q=80",
		PriceCredits:        8,
		Material:            "etched_metal",
		DarkColor:           "#18181b",
		TextureStrength:     0.10,
		ContrastStrength:    0.88,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#18181b", "#71717a", "#e4e4e7"},
		Prompt:              "Brushed metal etching, precision laser cut dark metallic matte finish, brass and titanium alloy, studio reflection",
		NegativePrompt:      "scratched qr modules, missing finder, warped grid, blurry",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
		Placement: &model.Placement{
			X:    0.25,
			Y:    0.25,
			Size: 0.50,
		},
	},
	{
		ID:                  "fabric_pattern",
		Slug:                "fabric_pattern",
		Name:                "Thêu Dệt Thổ Cẩm (Fabric Pattern)",
		Description:         "Họa tiết thêu sợi chỉ tơ lụa chìm trên nền vải linen mộc cao cấp",
		PreviewURL:          "https://images.unsplash.com/photo-1528458973465-5d2091df8a1a?w=600&auto=format&fit=crop&q=80",
		ReferenceImageURL:   "https://images.unsplash.com/photo-1528458973465-5d2091df8a1a?w=1024&auto=format&fit=crop&q=80",
		PriceCredits:        5,
		Material:            "woven_fabric",
		DarkColor:           "#1c1917",
		TextureStrength:     0.12,
		ContrastStrength:    0.82,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#1c1917", "#44403c", "#f5f5f4"},
		Prompt:              "Woven fabric embroidery, textured dark indigo linen threads on natural cotton fabric, detailed micro weave fiber",
		NegativePrompt:      "white sticker, broken QR modules, distorted finder pattern",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
		Placement: &model.Placement{
			X:    0.25,
			Y:    0.25,
			Size: 0.50,
		},
	},
	{
		ID:                  "starry-night",
		Slug:                "starry-night",
		Name:                "Starry Night",
		Description:         "Sơn dầu cobalt huyền thoại, bầu trời xoáy và ánh sao vàng rực rỡ",
		PreviewURL:          "https://images.unsplash.com/photo-1579783900882-c0d3dad7b119?w=600&auto=format&fit=crop&q=80",
		Material:            "oil_painting",
		DarkColor:           "#0b1e3f",
		TextureStrength:     0.18,
		ContrastStrength:    0.80,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#0b1e3f", "#1d4ed8", "#eab308"},
		Prompt:              "Van Gogh Starry Night, swirling deep blue night sky, luminous golden stars, crescent moon, cypress tree, masterpiece oil painting",
		NegativePrompt:      "bad quality, low resolution, blurry, distorted, messy, low contrast, text, watermark, white card, white box, sticker, label, frame",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "cyberpunk",
		Slug:                "cyberpunk",
		Name:                "Cyberpunk Metropolis",
		Description:         "Thành phố tương lai đêm mưa, biển neon rực rỡ và ánh phản chiếu công nghệ cao",
		PreviewURL:          "https://images.unsplash.com/photo-1542751371-adc38448a05e?w=600&auto=format&fit=crop&q=80",
		Material:            "cyberpunk_neon",
		DarkColor:           "#090d16",
		TextureStrength:     0.15,
		ContrastStrength:    0.85,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#090d16", "#06b6d4", "#ec4899"},
		Prompt:              "Futuristic cyberpunk metropolis at night, rainy street reflections, glowing cyan and magenta neon signs, cinematic volumetric lighting",
		NegativePrompt:      "bad quality, low resolution, blurry, low contrast, text, watermark, white card, white box, sticker, frame",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "ukiyo_wave",
		Slug:                "ukiyo_wave",
		Name:                "Sóng Lừng Ukiyo-e (Great Wave)",
		Description:         "Mộc bản tranh khắc gỗ Nhật Bản phong cách Hokusai, ngọn sóng xanh chàm và bọt tuyết",
		PreviewURL:          "https://images.unsplash.com/photo-1578328819058-b69f3a3b0f6b?w=600&auto=format&fit=crop&q=80",
		Material:            "ukiyo_e_woodblock",
		DarkColor:           "#0f2b48",
		TextureStrength:     0.15,
		ContrastStrength:    0.84,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#0f2b48", "#1e5b88", "#f4ecd8"},
		Prompt:              "Traditional Japanese Ukiyo-e woodblock print, roaring Great Wave off Kanagawa, flowing indigo and deep ocean blue curved waves, white sea foam crests, Mount Fuji backdrop, masterpiece",
		NegativePrompt:      "modern photo, 3d render, glossy, missing QR modules, corrupted grid, blurry",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "kintsugi_gold",
		Slug:                "kintsugi_gold",
		Name:                "Cẩm Thạch Dát Vàng (Kintsugi Gold)",
		Description:         "Đá cẩm thạch đen hoàng gia với các đường rạn nứt hàn gắn bằng vàng lá 24K",
		PreviewURL:          "https://images.unsplash.com/photo-1618005182384-a83a8bd57fbe?w=600&auto=format&fit=crop&q=80",
		Material:            "kintsugi_marble",
		DarkColor:           "#121316",
		TextureStrength:     0.16,
		ContrastStrength:    0.86,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#121316", "#d4af37", "#fef3c7"},
		Prompt:              "Japanese Kintsugi art on polished black imperial marble, organic cracked gold leaf seams, molten 24k gold veins, luxurious studio lighting, cinematic 8k",
		NegativePrompt:      "plastic, cheap glitter, low contrast, distorted QR modules, unscannable",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "botanical_jungle",
		Slug:                "botanical_jungle",
		Name:                "Rừng Nhiệt Đới & Thảo Mộc (Botanical Vines)",
		Description:         "Dây leo xanh mướt, lá cây nhiệt đới tự nhiên đan xen tạo khối QR hữu cơ",
		PreviewURL:          "https://images.unsplash.com/photo-1518531933037-91b2f5f229cc?w=600&auto=format&fit=crop&q=80",
		Material:            "botanical_leaves",
		DarkColor:           "#062816",
		TextureStrength:     0.14,
		ContrastStrength:    0.83,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#062816", "#15803d", "#86efac"},
		Prompt:              "Lush tropical rainforest foliage, interlocking monstera and fern leaves, dew drops on deep emerald vines, sunbeams through misty canopy, macro photography",
		NegativePrompt:      "dead leaves, dry brown, broken QR modules, distorted finder pattern",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "latte_art",
		Slug:                "latte_art",
		Name:                "Bọt Sữa Cà Phê (Barista Latte Art)",
		Description:         "Họa tiết vẽ trên bọt sữa cà phê Espresso sánh mịn phong cách Barista thủ công",
		PreviewURL:          "https://images.unsplash.com/photo-1514432324607-a09d9b4aefdd?w=600&auto=format&fit=crop&q=80",
		Material:            "coffee_latte",
		DarkColor:           "#381e11",
		TextureStrength:     0.12,
		ContrastStrength:    0.80,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#381e11", "#78350f", "#fef9c3"},
		Prompt:              "Barista latte art in a ceramic coffee cup, dark roasted espresso crema swirls, velvety microfoam texture, warm cozy cafe ambient lighting",
		NegativePrompt:      "white sticker, pixelated, broken QR code, blurry cream",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "ink_blossom",
		Slug:                "ink_blossom",
		Name:                "Thủy Mặc Hoa Đào (Oriental Ink Wash)",
		Description:         "Tranh thủy mặc mực tàu và hoa anh đào hồng phấn trên nền giấy xuyến chỉ cổ",
		PreviewURL:          "https://images.unsplash.com/photo-1579783902614-a3fb3927b675?w=600&auto=format&fit=crop&q=80",
		Material:            "sumi_e_ink",
		DarkColor:           "#18181b",
		TextureStrength:     0.15,
		ContrastStrength:    0.85,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#18181b", "#fb7185", "#fafaf9"},
		Prompt:              "Traditional Chinese Sumi-e ink wash painting, flowing black calligraphy ink, delicate blooming pink sakura cherry blossoms on vintage rice paper, elegant minimalist",
		NegativePrompt:      "digital vector, flat cartoon, broken finder pattern, messy splashes outside grid",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "crystal_prism",
		Slug:                "crystal_prism",
		Name:                "Pha Lê Khối & Kim Cương (Crystal Prism)",
		Description:         "Khối pha lê thủy tinh đa giác phản chiếu ánh sáng tán sắc quang phổ rực rỡ",
		PreviewURL:          "https://images.unsplash.com/photo-1518709268805-4e9042af9f23?w=600&auto=format&fit=crop&q=80",
		Material:            "crystal_glass",
		DarkColor:           "#0f172a",
		TextureStrength:     0.13,
		ContrastStrength:    0.85,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#0f172a", "#38bdf8", "#c084fc"},
		Prompt:              "Faceted obsidian and diamond crystal prism, glowing chromatic dispersion caustics, sharp refractive glass geometric facets, luxury octane render",
		NegativePrompt:      "foggy, cloudy, low resolution, missing modules, warped corners",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "ming_porcelain",
		Slug:                "ming_porcelain",
		Name:                "Gốm Sứ Men Lam (Ming Porcelain)",
		Description:         "Họa tiết hoa văn men lam cobalt vẽ tay trên nền gốm sứ trắng bóng triều Minh",
		PreviewURL:          "https://images.unsplash.com/photo-1565193566173-7a0ee3dbe261?w=600&auto=format&fit=crop&q=80",
		Material:            "blue_white_porcelain",
		DarkColor:           "#1e3a8a",
		TextureStrength:     0.11,
		ContrastStrength:    0.87,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#1e3a8a", "#2563eb", "#ffffff"},
		Prompt:              "Antique Blue and White Ming dynasty ceramic porcelain plate, cobalt blue hand-painted floral glaze, glossy porcelain glaze reflection, museum artifact",
		NegativePrompt:      "cracked, dirty, low contrast, broken modules, unscannable",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "vintage_brick",
		Slug:                "vintage_brick",
		Name:                "Tường Gạch Nung Vintage (Terracotta Brick)",
		Description:         "Mảng tường gạch đất nung cổ điển với vết rêu phong và ánh nắng chiều xiên",
		PreviewURL:          "https://images.unsplash.com/photo-1584622650111-993a426fbf0a?w=600&auto=format&fit=crop&q=80",
		Material:            "terracotta_brick",
		DarkColor:           "#431407",
		TextureStrength:     0.15,
		ContrastStrength:    0.83,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#431407", "#9a3412", "#fed7aa"},
		Prompt:              "Old red brick wall texture, weathered terracotta bricks with mortar grooves, warm golden hour sunlight casting shadows, rustic architectural detail",
		NegativePrompt:      "smooth plastic, flat graphic, broken qr modules, distorted corners",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "cosmic_aurora",
		Slug:                "cosmic_aurora",
		Name:                "Vũ Trụ Cực Quang (Cosmic Aurora)",
		Description:         "Dải thiên hà cực quang xanh ngọc và tím thẫm phát sáng giữa biển sao vũ trụ",
		PreviewURL:          "https://images.unsplash.com/photo-1506703719100-a0f3a48c0f86?w=600&auto=format&fit=crop&q=80",
		Material:            "cosmic_nebula",
		DarkColor:           "#050814",
		TextureStrength:     0.16,
		ContrastStrength:    0.84,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#050814", "#22d3ee", "#a855f7"},
		Prompt:              "Spectacular Northern Lights Aurora Borealis over starry night sky, shimmering glowing teal and violet light curtains, stardust nebula, cinematic astro-photography",
		NegativePrompt:      "white box, blurry, low resolution, broken finder, unreadable",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "dark_chocolate",
		Slug:                "dark_chocolate",
		Name:                "Sô-cô-la Điêu Khắc (Artisan Chocolate)",
		Description:         "Thanh sô-cô-la đen nguyên chất 85% dập nổi họa tiết cacao thủ công Thụy Sĩ",
		PreviewURL:          "https://images.unsplash.com/photo-1548907040-4baa42d10919?w=600&auto=format&fit=crop&q=80",
		Material:            "dark_chocolate",
		DarkColor:           "#23120b",
		TextureStrength:     0.12,
		ContrastStrength:    0.85,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#23120b", "#451a03", "#d97706"},
		Prompt:              "Artisan gourmet dark chocolate bar relief, dusted with fine golden cacao powder, silky matte chocolate finish, delicious gourmet dessert photography",
		NegativePrompt:      "melted messy liquid, broken QR modules, distorted grid, blurry",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "papyrus_parchment",
		Slug:                "papyrus_parchment",
		Name:                "Giấy Cói Cổ Ai Cập (Papyrus Parchment)",
		Description:         "Văn thư giấy cói Pharaoh ngàn năm tuổi với thớ sợi dệt tự nhiên và mép cháy sém",
		PreviewURL:          "https://images.unsplash.com/photo-1589829085413-56de8ae18c73?w=600&auto=format&fit=crop&q=80",
		Material:            "ancient_papyrus",
		DarkColor:           "#29180b",
		TextureStrength:     0.14,
		ContrastStrength:    0.82,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#29180b", "#78350f", "#fef3c7"},
		Prompt:              "Ancient Egyptian papyrus scroll parchment, aged woven reed fibers, burnt deckle edges, hieroglyphic ink tone, warm archaeological artifact lighting",
		NegativePrompt:      "modern digital paper, white sticker, broken finder patterns, distorted",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "moss_bonsai",
		Slug:                "moss_bonsai",
		Name:                "Tiểu Cảnh Rêu & Bonsai (Terrarium Moss)",
		Description:         "Rêu nhung xanh mướt và thảm thực vật tí hon trên phiến đá thủy sinh Bonsai",
		PreviewURL:          "https://images.unsplash.com/photo-1509198397868-475647b2a1e5?w=600&auto=format&fit=crop&q=80",
		Material:            "velvet_moss",
		DarkColor:           "#052010",
		TextureStrength:     0.14,
		ContrastStrength:    0.83,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#052010", "#166534", "#4ade80"},
		Prompt:              "Miniature Japanese Zen terrarium, lush green velvet moss cushion, tiny dew droplets, Bonsai stone landscape, macro nature photography",
		NegativePrompt:      "dry grass, yellow dead leaves, broken QR code, unscannable",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
	{
		ID:                  "carbon_supercar",
		Slug:                "carbon_supercar",
		Name:                "Sợi Carbon Siêu Xe (Carbon Supercar)",
		Description:         "Vân sợi carbon dệt chéo 3D siêu nhẹ kết hợp dải đèn LED khí động học",
		PreviewURL:          "https://images.unsplash.com/photo-1617814076367-b759c7d7e738?w=600&auto=format&fit=crop&q=80",
		Material:            "carbon_fiber",
		DarkColor:           "#090a0f",
		TextureStrength:     0.12,
		ContrastStrength:    0.88,
		QuietZoneModules:    4,
		EdgeSoftness:        0.0,
		AllowGeometryChange: false,
		Colors:              []string{"#090a0f", "#3b82f6", "#e2e8f0"},
		Prompt:              "Hypercar aerodynamic carbon fiber twill weave body panel, matte black composite weave, sleek blue LED accent lighting, automotive studio reflection",
		NegativePrompt:      "plastic wrap, scratched surface, broken QR modules, distorted grid",
		ConditioningScale:   1.45,
		GuidanceScale:       7.5,
		Width:               1024,
		Height:              1024,
		Enabled:             true,
	},
}


// BuildPrompt creates a complete placement-aware prompt for the QR generation
func BuildPrompt(preset *model.ArtQRPreset, analysis *vision.StyleAnalysisResult, placement model.Placement) (string, string) {
	var basePrompt string
	negativePrompt := StandardNegativePrompt

	if analysis != nil && analysis.GeneratedPrompt != "" {
		return BuildCustomReferencePrompt(analysis, "", placement)
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

// BuildCustomReferencePrompt constructs an authoritative prompt for custom reference scenes,
// guaranteeing that AI blends the QR into the analyzed style while strictly preserving
// 100% of module coordinates, square boundaries, scaling, and finder patterns without alteration.
func BuildCustomReferencePrompt(analysis *vision.StyleAnalysisResult, rawCustomPrompt string, placement model.Placement) (string, string) {
	sceneDescription := "cảnh nghệ thuật với các chi tiết tự nhiên và bố cục đặc trưng"
	targetSurface := "mặt trước của vật thể trong ảnh"
	darkModuleStyleBullets := "- màu sắc đậm tương thích với vật liệu bề mặt\n- shading tự nhiên theo hướng sáng của ảnh\n- cảm giác chất liệu hòa nhập hữu cơ"
	darkModuleExample := "vật liệu bề mặt màu đậm"
	lightModuleExample := "màu sáng tự nhiên của bề mặt"
	surfaceStateDetails := "Bề mặt vật thể trong ảnh kết quả phải trông hoàn thiện, sắc nét, có chiều sâu ánh sáng và màu sắc hài hòa với phong cách tổng thể."

	if analysis != nil {
		if analysis.SceneDescription != "" {
			sceneDescription = analysis.SceneDescription
		} else if analysis.GeneratedPrompt != "" {
			sceneDescription = analysis.GeneratedPrompt
		}
		if analysis.TargetSurface != "" {
			targetSurface = analysis.TargetSurface
		}
		if len(analysis.DarkModuleStyle) > 0 {
			var bullets []string
			for _, s := range analysis.DarkModuleStyle {
				bullets = append(bullets, "- "+s)
			}
			darkModuleStyleBullets = strings.Join(bullets, "\n")
			darkModuleExample = analysis.DarkModuleStyle[0]
		} else if analysis.Texture != "" {
			darkModuleStyleBullets = fmt.Sprintf("- chất liệu %s màu đậm\n- vân %s tự nhiên\n- shading ánh sáng hài hòa", analysis.Texture, analysis.Texture)
			darkModuleExample = "chất liệu " + analysis.Texture + " màu đậm"
		}
		if analysis.LightModuleStyle != "" {
			lightModuleExample = analysis.LightModuleStyle
		}
		if analysis.SurfaceState != "" {
			surfaceStateDetails = analysis.SurfaceState
		} else if analysis.Style != "" {
			surfaceStateDetails = fmt.Sprintf("Vật thể trong ảnh phải giữ đúng phong cách %s, ánh sáng %s, bảng màu hài hòa và có chiều sâu thẩm mỹ cao.", analysis.Style, analysis.Lighting)
		}
	}

	if strings.TrimSpace(rawCustomPrompt) != "" {
		if !strings.Contains(rawCustomPrompt, "QUY TẮC QUAN TRỌNG NHẤT") {
			sceneDescription = strings.TrimSpace(rawCustomPrompt)
		}
	}

	prompt := fmt.Sprintf(`HÃY ĐỌC TOÀN BỘ YÊU CẦU TRƯỚC KHI TẠO ẢNH.

Ảnh thứ hai là mã QR gốc và là nguồn cấu trúc QR duy nhất.

Trong mã QR, mỗi ô nhỏ trong lưới được gọi là một module.

QUY TẮC QUAN TRỌNG NHẤT:

KHÔNG ĐƯỢC THAY ĐỔI VỊ TRÍ TƯƠNG ĐỐI CỦA BẤT KỲ MODULE NÀO SO VỚI TOÀN BỘ MA TRẬN QR.

Nghĩa là:
- module nào đang nằm ở hàng nào, cột nào thì vẫn phải nằm đúng hàng và cột đó
- không được chuyển một module sang vị trí khác
- không được đổi chỗ hai module
- không được làm lệch riêng một module
- không được làm một vùng QR trôi sang vị trí khác
- không được thay đổi mối quan hệ giữa các module

Toàn bộ ma trận QR phải giữ nguyên topology.

Tuy nhiên:

ĐƯỢC PHÉP THAY ĐỔI PHONG CÁCH HÌNH ẢNH CỦA MODULE.

Module tối không bắt buộc phải là ô đen phẳng.

Module tối có thể được thể hiện theo phong cách phù hợp với %s, ví dụ:
%s

Mục tiêu là làm cho QR có cùng phong cách hình ảnh với %s.

NHƯNG:

Dù module được stylize như thế nào, mỗi module vẫn phải chiếm đúng vị trí tương đối của nó trong ma trận QR.

Không được để styling làm:
- module dịch sang ô khác
- module biến mất
- module mới xuất hiện ở vị trí không có trong QR gốc
- hai module tách biệt bị nối nhầm vì hiệu ứng
- module tối trở thành quá sáng
- module sáng trở thành quá tối
- cấu trúc finder pattern bị thay đổi

Hãy hiểu như sau:

QR gốc cung cấp một BẢN ĐỒ VỊ TRÍ.

Bản đồ này bị khóa.

Model được phép thay đổi CÁCH HIỂN THỊ của từng vùng trong bản đồ, nhưng không được thay đổi VỊ TRÍ của vùng đó.

Ví dụ:

Một module tối trong QR gốc có thể trở thành một vùng %s.

Nhưng vùng đó phải vẫn nằm chính xác tại vị trí module gốc.

Một module sáng có thể sử dụng %s.

Nhưng nó vẫn phải giữ đúng vị trí sáng của QR gốc.

==================================================
BIẾN ĐỔI TOÀN BỘ QR
==================================================

Toàn bộ mã QR được phép được:
- scale theo kích thước %s
- di chuyển
- scale đồng đều
- xoay
- nghiêng nhẹ
- căn phối cảnh với mặt %s

NHƯNG chỉ như MỘT TẤM PHẲNG DUY NHẤT.

QUAN TRỌNG:
PHẢI PHÓNG TO TOÀN BỘ QR RÕ RỆT ĐỂ QR PHỦ GẦN HẾT PHẦN MẶT TRƯỚC CÓ THỂ SỬ DỤNG CỦA %s.

Không được giữ QR nhỏ ở giữa %s.

Không được hiểu yêu cầu giữ nguyên vị trí tương đối module là phải giữ nguyên kích thước tổng thể của QR.

Việc phóng to TOÀN BỘ QR được phép và BẮT BUỘC.

Hãy coi toàn bộ QR giống như một layer ảnh vuông duy nhất.

Khi cần làm QR lớn hơn:
- chỉ phóng to toàn bộ layer QR cùng lúc
- tất cả module lớn lên cùng một tỉ lệ
- finder pattern lớn lên cùng một tỉ lệ
- quiet zone lớn lên cùng một tỉ lệ
- không tái tạo lại QR
- không vẽ lại từng module

MỤC TIÊU KÍCH THƯỚC:

Xác định phần mặt trước của %s có thể sử dụng.

Sau đó phóng to toàn bộ QR sao cho mép ngoài của QR/quiet zone chỉ cách mép an toàn của %s khoảng 5%%.

Hiểu đơn giản:

- mặt %s sử dụng được = 100%%
- QR + quiet zone nên chiếm khoảng 90%% chiều rộng và 90%% chiều cao vùng đó
- chỉ chừa khoảng 5%% lề bên trái
- khoảng 5%% lề bên phải
- khoảng 5%% lề phía trên
- khoảng 5%% lề phía dưới

QR phải lớn hơn rõ rệt so với phiên bản nhỏ trước đó.

Ưu tiên QR lớn nhưng vẫn:
- nằm hoàn toàn trên %s
- không tràn ra ngoài viền
- không bị các chi tiết khác che khuất
- giữ quiet zone
- giữ khả năng quét

Nếu QR chưa gần chạm tới vùng lề an toàn 5%%, hãy tiếp tục phóng to TOÀN BỘ QR.

Tất cả module phải cùng nhận một phép biến đổi toàn cục.

Không được biến đổi từng module độc lập.

Không được làm một module nghiêng khác module bên cạnh.

Không được uốn cong cục bộ lưới QR.

Không được phóng to từng module riêng lẻ.

Không được tái tạo pattern QR để làm QR lớn hơn.

Hãy coi toàn bộ QR như một tấm vật liệu có in sẵn bố cục QR:
có thể phóng to, xoay hoặc nghiêng cả tấm,
nhưng bố cục bên trong tấm không được thay đổi.

==================================================
FINDER PATTERN
==================================================

Ba cụm hình vuông lớn ở ba góc QR phải giữ đúng vị trí và cấu trúc tương đối.

Có thể đổi màu/chất liệu để hòa với %s.

Nhưng không được thay đổi bố cục ô sáng/tối bên trong finder pattern.

==================================================
QUIET ZONE
==================================================

Vùng sáng bao quanh QR phải vẫn rõ và dễ phân biệt.

Có thể dùng %s thay cho trắng tinh.

Nhưng không được thêm quá nhiều vùng tối làm mất quiet zone.

Quiet zone phải được tính là một phần của toàn bộ QR khi phóng to.

Không được phóng QR đến mức quiet zone bị cắt mất.

==================================================
TRẠNG THÁI VẬT THỂ
==================================================

%s

==================================================
NHIỆM VỤ
==================================================

Ảnh thứ nhất là %s.

Đặt mã QR từ ảnh thứ hai lên %s.

BẮT BUỘC PHÓNG TO TOÀN BỘ QR để QR gần phủ hết phần mặt trước sử dụng được của %s, chỉ chừa khoảng 5%% lề an toàn xung quanh.

Không được giữ QR nhỏ như phiên bản trước.

Việc tăng kích thước phải thực hiện bằng cách phóng to TOÀN BỘ QR như một layer duy nhất, không được tái tạo lại cấu trúc QR.

Cho phép QR có phong cách đồng nhất với %s.

Module tối có thể mang:
%s

Module sáng có thể hòa với %s.

Mục tiêu là:
QR nhìn như một phần của %s,
không phải một QR đen trắng dán lên trên.

QR phải lớn, cân đối và sử dụng gần tối đa diện tích mặt phẳng có thể dùng.

Nhưng tuyệt đối không được thay đổi vị trí tương đối của các module trong ma trận QR.

Nếu cần đánh đổi:
ưu tiên giữ đúng bố cục QR trước,
sau đó ưu tiên QR đủ lớn,
sau đó tối ưu phong cách và trạng thái vật thể.`,
		targetSurface,          // 1
		darkModuleStyleBullets, // 2
		targetSurface,          // 3
		darkModuleExample,      // 4
		lightModuleExample,     // 5
		targetSurface,          // 6
		targetSurface,          // 7
		targetSurface,          // 8
		targetSurface,          // 9
		targetSurface,          // 10
		targetSurface,          // 11
		targetSurface,          // 12
		targetSurface,          // 13
		targetSurface,          // 14
		lightModuleExample,     // 15
		surfaceStateDetails,    // 16
		sceneDescription,       // 17
		targetSurface,          // 18
		targetSurface,          // 19
		targetSurface,          // 20
		darkModuleStyleBullets, // 21
		lightModuleExample,     // 22
		targetSurface,          // 23
	)

	return prompt, StandardNegativePrompt
}

