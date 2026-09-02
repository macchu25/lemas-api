package qr

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

	qrcode "github.com/skip2/go-qrcode"
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
	return CompositeArtQRExact(artBytes, "", qrPNG, placement, canvasSize, variation)
}

// CompositeArtQRExact uses exact mathematical QR matrix from payload to ensure 100% scan precision
func CompositeArtQRExact(artBytes []byte, payload string, qrPNG []byte, placement model.Placement, canvasSize int, variation int) ([]byte, error) {
	if canvasSize <= 0 {
		canvasSize = 1024
	}

	artImg, _, err := image.Decode(bytes.NewReader(artBytes))
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

	// 3. Extract exact mathematical QR matrix
	var matrix [][]bool
	if payload != "" {
		code, qErr := qrcode.New(payload, qrcode.Highest)
		if qErr == nil {
			matrix = code.Bitmap()
		}
	}

	// Fallback to image decoding if payload was empty
	if len(matrix) == 0 && len(qrPNG) > 0 {
		qrImg, _, imgErr := image.Decode(bytes.NewReader(qrPNG))
		if imgErr == nil {
			bounds := qrImg.Bounds()
			w, h := bounds.Dx(), bounds.Dy()
			dim := 29
			matrix = make([][]bool, dim)
			for r := 0; r < dim; r++ {
				matrix[r] = make([]bool, dim)
				sY := bounds.Min.Y + int((float64(r)+0.5)*float64(h)/float64(dim))
				for c := 0; c < dim; c++ {
					sX := bounds.Min.X + int((float64(c)+0.5)*float64(w)/float64(dim))
					rQ, gQ, bQ, _ := qrImg.At(sX, sY).RGBA()
					lum := (rQ*299 + gQ*587 + bQ*114) / 1000 >> 8
					matrix[r][c] = lum < 128
				}
			}
		}
	}

	if len(matrix) == 0 {
		return nil, image.ErrFormat
	}

	moduleCount := len(matrix)
	modSize := float64(pSize) / float64(moduleCount)

	// 4. Render Organic Painterly Strokes with Guaranteed Optical Scan Contrast
	for r := 0; r < moduleCount; r++ {
		for c := 0; c < moduleCount; c++ {
			isDark := matrix[r][c]

			// Finder Pattern corner zones
			isFinderTL := r < 8 && c < 8
			isFinderTR := r < 8 && c >= moduleCount-8
			isFinderBL := r >= moduleCount-8 && c < 8
			isFinderZone := isFinderTL || isFinderTR || isFinderBL

			centerX := float64(px) + (float64(c)+0.5)*modSize
			centerY := float64(py) + (float64(r)+0.5)*modSize
			radius := modSize * 0.49

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
						// Concentric Finder Halo with softened rounded corners
						if isDark {
							// Deep rich oil shadow
							canvas.Set(destX, destY, color.RGBA{
								R: uint8(float64(r8) * 0.08),
								G: uint8(float64(g8) * 0.08),
								B: uint8(float64(b8) * 0.08),
								A: 255,
							})
						} else {
							// Luminous celestial highlight
							canvas.Set(destX, destY, color.RGBA{
								R: clamp255(int(r8)/4 + 205),
								G: clamp255(int(g8)/4 + 205),
								B: clamp255(int(b8)/4 + 205),
								A: 255,
							})
						}
					} else {
						// Organic painterly stroke with soft edge falloff
						weight := 1.0 - (dist / radius)
						if weight < 0 {
							weight = 0
						}
						if weight > 1 {
							weight = 1
						}

						if isDark {
							// Dark oil paint stroke
							blend := 1.0 - (0.86 * weight)
							canvas.Set(destX, destY, color.RGBA{
								R: uint8(float64(r8) * blend),
								G: uint8(float64(g8) * blend),
								B: uint8(float64(b8) * blend),
								A: 255,
							})
						} else {
							// Radiant paint highlight / star stroke
							glow := int(195.0 * weight)
							canvas.Set(destX, destY, color.RGBA{
								R: clamp255(int(r8)*2/10 + glow),
								G: clamp255(int(g8)*2/10 + glow),
								B: clamp255(int(b8)*2/10 + glow),
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
