package qr

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

	"xkiro-backend/internal/artqr/model"
)

// BuildControlCanvas creates a 1024x1024 canvas with the QR code positioned according to the user's placement coordinates
func BuildControlCanvas(qrPNG []byte, placement model.Placement, canvasSize int) ([]byte, error) {
	if canvasSize <= 0 {
		canvasSize = 1024
	}

	qrImg, _, err := image.Decode(bytes.NewReader(qrPNG))
	if err != nil {
		return nil, err
	}

	// 1. Create solid white background canvas (ControlNet SD1.5 standard for QR is black modules on white canvas)
	canvas := image.NewRGBA(image.Rect(0, 0, canvasSize, canvasSize))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	// 2. Calculate pixel coordinates
	if !placement.IsValid() {
		placement = model.DefaultPlacement()
	}

	px := int(placement.X * float64(canvasSize))
	py := int(placement.Y * float64(canvasSize))
	pSize := int(placement.Size * float64(canvasSize))

	if px < 0 {
		px = 0
	}
	if py < 0 {
		py = 0
	}
	if px+pSize > canvasSize {
		pSize = canvasSize - px
	}
	if py+pSize > canvasSize {
		pSize = canvasSize - py
	}

	// 3. Scale and draw QR code at destination rectangle
	targetRect := image.Rect(px, py, px+pSize, py+pSize)
	scaledQR := scaleImageNearest(qrImg, pSize, pSize)
	draw.Draw(canvas, targetRect, scaledQR, image.Point{}, draw.Over)

	// 4. Encode to PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// CompositeArtQR blends the artistic background painting and the QR code into a finalized scannable Art QR
func CompositeArtQR(artBytes []byte, qrPNG []byte, placement model.Placement, canvasSize int) ([]byte, error) {
	return CompositeArtQRWithStyle(artBytes, qrPNG, placement, canvasSize, 0)
}

// CompositeArtQRWithStyle blends the painting and QR code with customizable artistic style profiles
func CompositeArtQRWithStyle(artBytes []byte, qrPNG []byte, placement model.Placement, canvasSize int, variation int) ([]byte, error) {
	if canvasSize <= 0 {
		canvasSize = 1024
	}

	artImg, _, err := image.Decode(bytes.NewReader(artBytes))
	if err != nil {
		return nil, err
	}

	qrImg, _, err := image.Decode(bytes.NewReader(qrPNG))
	if err != nil {
		return nil, err
	}

	if !placement.IsValid() {
		placement = model.DefaultPlacement()
	}

	// 1. Create 1024x1024 canvas and scale art image to fill
	canvas := image.NewRGBA(image.Rect(0, 0, canvasSize, canvasSize))
	scaledArt := scaleImageBilinear(artImg, canvasSize, canvasSize)
	draw.Draw(canvas, canvas.Bounds(), scaledArt, image.Point{}, draw.Src)

	// 2. Compute placement rectangle
	px := int(placement.X * float64(canvasSize))
	py := int(placement.Y * float64(canvasSize))
	pSize := int(placement.Size * float64(canvasSize))

	if px < 0 {
		px = 0
	}
	if py < 0 {
		py = 0
	}
	if px+pSize > canvasSize {
		pSize = canvasSize - px
	}
	if py+pSize > canvasSize {
		pSize = canvasSize - py
	}

	// 3. Detect QR Module Matrix Dimension
	qrGray := image.NewGray(qrImg.Bounds())
	draw.Draw(qrGray, qrGray.Bounds(), qrImg, image.Point{}, draw.Src)
	bounds := qrGray.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Find module count along width by scanning first row of top-left finder pattern
	moduleCount := 25
	if w > 0 {
		// Sample transitions
		transitions := 0
		lastVal := qrGray.GrayAt(bounds.Min.X, bounds.Min.Y).Y < 128
		for x := 1; x < w; x++ {
			curVal := qrGray.GrayAt(bounds.Min.X+x, bounds.Min.Y).Y < 128
			if curVal != lastVal {
				transitions++
				lastVal = curVal
			}
		}
		if transitions >= 6 {
			moduleCount = (transitions + 2) * 2
			if moduleCount < 21 {
				moduleCount = 21
			} else if moduleCount > 60 {
				moduleCount = 33
			}
		}
	}

	modSize := float64(pSize) / float64(moduleCount)
	if modSize < 4.0 {
		modSize = 4.0
	}

	// 4. Sample boolean matrix of QR modules
	matrix := make([][]bool, moduleCount)
	for r := 0; r < moduleCount; r++ {
		matrix[r] = make([]bool, moduleCount)
		sampleY := bounds.Min.Y + int((float64(r)+0.5)*float64(h)/float64(moduleCount))
		for c := 0; c < moduleCount; c++ {
			sampleX := bounds.Min.X + int((float64(c)+0.5)*float64(w)/float64(moduleCount))
			matrix[r][c] = qrGray.GrayAt(sampleX, sampleY).Y < 128
		}
	}

	// 5. Render Organic Painterly Brushstrokes & Glowing Highlights
	for r := 0; r < moduleCount; r++ {
		for c := 0; c < moduleCount; c++ {
			isDark := matrix[r][c]

			// Check if within 3 Finder Pattern zones (7x7 modules at top-left, top-right, bottom-left)
			isFinderTL := r < 7 && c < 7
			isFinderTR := r < 7 && c >= moduleCount-7
			isFinderBL := r >= moduleCount-7 && c < 7
			isFinderZone := isFinderTL || isFinderTR || isFinderBL

			centerX := float64(px) + (float64(c)+0.5)*modSize
			centerY := float64(py) + (float64(r)+0.5)*modSize
			radius := modSize * 0.48

			// Bounding box for this module's stroke
			minX := int(centerX - modSize*0.5)
			maxX := int(centerX + modSize*0.5)
			minY := int(centerY - modSize*0.5)
			maxY := int(centerY + modSize*0.5)

			for destY := minY; destY <= maxY; destY++ {
				for destX := minX; destX <= maxX; destX++ {
					if destX < 0 || destX >= canvasSize || destY < 0 || destY >= canvasSize {
						continue
					}

					dx := float64(destX) - centerX
					dy := float64(destY) - centerY
					dist := math.Hypot(dx, dy)

					origColor := canvas.At(destX, destY)
					rO, gO, bO, _ := origColor.RGBA()
					r8, g8, b8 := uint8(rO>>8), uint8(gO>>8), uint8(bO>>8)

					if isFinderZone {
						// Concentric Ornate Finder Rings with softened rounded corners
						if isDark {
							// Deep indigo/navy shadow matching painting
							canvas.Set(destX, destY, color.RGBA{
								R: uint8(float64(r8) * 0.10),
								G: uint8(float64(g8) * 0.10),
								B: uint8(float64(b8) * 0.10),
								A: 255,
							})
						} else {
							// Luminous celestial glow
							canvas.Set(destX, destY, color.RGBA{
								R: clamp255(int(r8)/4 + 195),
								G: clamp255(int(g8)/4 + 195),
								B: clamp255(int(b8)/4 + 195),
								A: 255,
							})
						}
					} else {
						// Organic painterly daub with soft Gaussian falloff
						weight := 1.0 - (dist / radius)
						if weight < 0 {
							weight = 0
						}
						if weight > 1 {
							weight = 1
						}

						if isDark {
							// Blend dark oil paint stroke with painting brushwork
							blendFactor := 1.0 - (0.80 * weight)
							canvas.Set(destX, destY, color.RGBA{
								R: uint8(float64(r8) * blendFactor),
								G: uint8(float64(g8) * blendFactor),
								B: uint8(float64(b8) * blendFactor),
								A: 255,
							})
						} else {
							// Blend radiant highlight / star-glow stroke
							glowAmount := int(185.0 * weight)
							canvas.Set(destX, destY, color.RGBA{
								R: clamp255(int(r8)*2/10 + glowAmount),
								G: clamp255(int(g8)*2/10 + glowAmount),
								B: clamp255(int(b8)*2/10 + glowAmount),
								A: 255,
							})
						}
					}
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func clamp255(v int) uint8 {
	if v > 255 {
		return 255
	}
	if v < 0 {
		return 0
	}
	return uint8(v)
}

func scaleImageBilinear(src image.Image, targetWidth, targetHeight int) image.Image {
	return scaleImageNearest(src, targetWidth, targetHeight)
}

// scaleImageNearest performs nearest-neighbor scaling to preserve crisp sharp QR module pixels
func scaleImageNearest(src image.Image, targetWidth, targetHeight int) image.Image {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))

	for y := 0; y < targetHeight; y++ {
		srcY := srcBounds.Min.Y + (y * srcH / targetHeight)
		for x := 0; x < targetWidth; x++ {
			srcX := srcBounds.Min.X + (x * srcW / targetWidth)
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}

	return dst
}
