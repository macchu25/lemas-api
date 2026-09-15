package vision

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"sort"
	"xkiro-backend/internal/artqr/model"
)

// colorBucket helper for dominant color extraction
type colorBucket struct {
	r, g, b uint32
	count   int
	hex     string
}

// AnalyzeImageLocally performs forensic pixel-level computer vision analysis
// on the input image without requiring external network APIs.
func AnalyzeImageLocally(imgBytes []byte, fallbackPlacement model.Placement) *StyleAnalysisResult {
	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return DefaultFallbackResult(fallbackPlacement)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return DefaultFallbackResult(fallbackPlacement)
	}

	// 1. Extract Dominant Color Palette (quantized histogram)
	palette := extractDominantPalette(img, 5)

	// 2. Compute 3x3 Grid Luminance and Detail/Edge Variance to locate best surface
	bestRegion, avgBrightness, contrastRatio := findOptimalSurfaceRegion(img, w, h)

	placement := fallbackPlacement
	if !placement.IsValid() || (placement.X == 0.25 && placement.Y == 0.25 && placement.Size == 0.50) {
		placement = bestRegion
	}

	// 3. Determine lighting and surface texture description based on pixel statistics
	lightingDesc := "Ánh sáng tự nhiên, cân bằng sáng tối hài hòa"
	if avgBrightness > 180 {
		lightingDesc = "Ánh sáng ngoài trời rực rỡ, độ tương phản cao, bóng đổ sắc nét"
	} else if avgBrightness < 80 {
		lightingDesc = "Ánh sáng mờ nghệ thuật (low-key / cinematic), tương phản sâu"
	} else if contrastRatio > 2.5 {
		lightingDesc = "Ánh sáng studio định hướng, tương phản nổi bật giữa mảng sáng và mảng tối"
	}

	surfaceName := "bề mặt phẳng chính trong ảnh"
	if placement.X >= 0.40 {
		surfaceName = "mảng bề mặt lớn bên phải khung hình (tường/vật thể nổi bật)"
	} else if placement.X <= 0.15 {
		surfaceName = "mảng bề mặt phẳng bên trái khung hình"
	}

	darkModStyle := []string{
		fmt.Sprintf("Màu sắc tương thích với bảng màu ảnh (%s), hòa vào chất liệu vật thể", palette[0]),
		"Bóng đổ và viền tiếp giáp tự nhiên theo chiều sáng",
	}

	return &StyleAnalysisResult{
		Style:            "Tác phẩm nghệ thuật / Nhiếp ảnh thực tế",
		SceneDescription: fmt.Sprintf("Ảnh tham chiếu với độ phân giải %dx%d, bảng màu chủ đạo %s, ánh sáng tự nhiên.", w, h, palette[0]),
		TargetSurface:    surfaceName,
		OptimalPlacement: &placement,
		DarkModuleStyle:  darkModStyle,
		LightModuleStyle: "Màu sáng tự nhiên giữ nguyên độ nhám và chất liệu của bề mặt ảnh",
		SurfaceState:     "Bề mặt phẳng vững chắc, các module QR được ép chìm vào vân chất liệu vật thể.",
		Palette:          palette,
		Lighting:         lightingDesc,
		Texture:          "Vân bề mặt tự nhiên, hạt chi tiết sắc nét",
		Contrast:         fmt.Sprintf("Tương phản sáng/tối %.1f:1", contrastRatio),
		IntegrationStrategy: []string{"ControlNet QR Lock 90% - Khóa cứng tuyệt đối ma trận QR vào tọa độ bề mặt"},
	}
}

func extractDominantPalette(img image.Image, topN int) []string {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	stepX := int(math.Max(1, float64(w)/100))
	stepY := int(math.Max(1, float64(h)/100))

	colorCounts := make(map[string]*colorBucket)

	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			r16, g16, b16, _ := img.At(x, y).RGBA()
			r := uint8((r16 >> 8) / 32 * 32)
			g := uint8((g16 >> 8) / 32 * 32)
			b := uint8((b16 >> 8) / 32 * 32)
			hex := fmt.Sprintf("#%02x%02x%02x", r, g, b)

			if bkt, ok := colorCounts[hex]; ok {
				bkt.count++
			} else {
				colorCounts[hex] = &colorBucket{
					r:     uint32(r),
					g:     uint32(g),
					b:     uint32(b),
					count: 1,
					hex:   hex,
				}
			}
		}
	}

	buckets := make([]*colorBucket, 0, len(colorCounts))
	for _, bkt := range colorCounts {
		buckets = append(buckets, bkt)
	}

	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].count > buckets[j].count
	})

	palette := make([]string, 0, topN)
	for i := 0; i < len(buckets) && len(palette) < topN; i++ {
		palette = append(palette, buckets[i].hex)
	}

	if len(palette) == 0 {
		palette = []string{"#1a202c", "#f5d0a9", "#ffd700", "#8b0000"}
	}
	return palette
}

func findOptimalSurfaceRegion(img image.Image, w, h int) (model.Placement, float64, float64) {
	bounds := img.Bounds()
	totalPixels := 0
	var sumBrightness float64
	minLum := 255.0
	maxLum := 0.0

	// Divide into 3x3 cells to evaluate surface flatness (lowest color variance = cleanest surface)
	type cellStat struct {
		row, col int
		variance float64
		meanLum  float64
	}
	var cells []cellStat

	cellW := w / 3
	cellH := h / 3

	for row := 0; row < 3; row++ {
		for col := 0; col < 3; col++ {
			startX := bounds.Min.X + col*cellW
			startY := bounds.Min.Y + row*cellH
			endX := startX + cellW
			endY := startY + cellH

			var cellSum, cellSqSum float64
			var count float64
			step := int(math.Max(1, float64(cellW)/20))

			for y := startY; y < endY; y += step {
				for x := startX; x < endX; x += step {
					r16, g16, b16, _ := img.At(x, y).RGBA()
					r := float64(r16 >> 8)
					g := float64(g16 >> 8)
					b := float64(b16 >> 8)
					lum := 0.299*r + 0.587*g + 0.114*b

					cellSum += lum
					cellSqSum += lum * lum
					count++

					sumBrightness += lum
					totalPixels++
					if lum < minLum {
						minLum = lum
					}
					if lum > maxLum {
						maxLum = lum
					}
				}
			}

			if count > 0 {
				mean := cellSum / count
				variance := (cellSqSum / count) - (mean * mean)
				cells = append(cells, cellStat{
					row:      row,
					col:      col,
					variance: variance,
					meanLum:  mean,
				})
			}
		}
	}

	avgBrightness := 128.0
	if totalPixels > 0 {
		avgBrightness = sumBrightness / float64(totalPixels)
	}
	contrastRatio := 1.5
	if minLum > 0 {
		contrastRatio = maxLum / math.Max(1, minLum)
	}

	// Sort cells by lowest variance (most uniform flat surface)
	sort.Slice(cells, func(i, j int) bool {
		return cells[i].variance < cells[j].variance
	})

	// Default fallback placement
	bestPlacement := model.Placement{X: 0.20, Y: 0.20, Size: 0.60}
	if len(cells) > 0 {
		best := cells[0]
		// Map 3x3 grid to normalized coordinates
		normX := float64(best.col) / 3.0
		normY := float64(best.row) / 3.0

		// Expand to a ~60% size bounding box centered around the clean area
		bestPlacement = model.Placement{
			X:    math.Max(0.05, math.Min(0.35, normX)),
			Y:    math.Max(0.05, math.Min(0.35, normY)),
			Size: 0.60,
		}
	}

	return bestPlacement, avgBrightness, contrastRatio
}

// DefaultFallbackResult provides safe defaults if image parsing completely fails
func DefaultFallbackResult(placement model.Placement) *StyleAnalysisResult {
	if !placement.IsValid() {
		placement = model.DefaultPlacement()
	}
	return &StyleAnalysisResult{
		Style:            "Tác phẩm nghệ thuật tự nhiên",
		SceneDescription: "bức ảnh tham chiếu với các chi tiết tự nhiên",
		TargetSurface:    "bề mặt vật thể chính trong ảnh",
		OptimalPlacement: &placement,
		Palette:          []string{"#8b0000", "#ffd700", "#1a202c", "#f5d0a9"},
		Lighting:         "Ánh sáng studio cinematic",
		Texture:          "Vân bề mặt và chi tiết tự nhiên",
		DarkModuleStyle:  []string{"màu sắc đậm tương thích với bề mặt", "shading tự nhiên theo hướng sáng"},
		LightModuleStyle: "màu sáng tự nhiên của bề mặt",
		SurfaceState:     "Bề mặt vật thể trong ảnh phải trông hoàn thiện, sắc nét và màu sắc hài hòa.",
	}
}
