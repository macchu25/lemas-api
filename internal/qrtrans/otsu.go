package qrtrans

import (
	"image"
)

// CalculateOtsuThreshold computes the optimal binarization threshold for an image
// using Otsu's method (maximizing inter-class variance).
func CalculateOtsuThreshold(img image.Image) uint8 {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	totalPixels := width * height
	if totalPixels == 0 {
		return 215 // default fallback
	}

	var hist [256]int
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			// Standard luminance formula in 8-bit scale
			lum := (299*uint32(r>>8) + 587*uint32(g>>8) + 114*uint32(b>>8) + 500) / 1000
			if lum > 255 {
				lum = 255
			}
			hist[lum]++
		}
	}

	var sumTotal float64
	for i := 0; i < 256; i++ {
		sumTotal += float64(i * hist[i])
	}

	var sumB float64
	var weightB float64
	var maxVariance float64
	optimalThreshold := 215 // default initial value

	for t := 0; t < 256; t++ {
		weightB += float64(hist[t])
		if weightB == 0 {
			continue
		}

		weightF := float64(totalPixels) - weightB
		if weightF == 0 {
			break
		}

		sumB += float64(t * hist[t])
		meanB := sumB / weightB
		meanF := (sumTotal - sumB) / weightF

		// Inter-class variance
		variance := weightB * weightF * (meanB - meanF) * (meanB - meanF)
		if variance > maxVariance {
			maxVariance = variance
			optimalThreshold = t
		}
	}

	// For QR codes, we generally want threshold high enough not to clip modules,
	// but lower than white background.
	if optimalThreshold < 120 {
		optimalThreshold = 180
	} else if optimalThreshold > 245 {
		optimalThreshold = 230
	}

	return uint8(optimalThreshold)
}
