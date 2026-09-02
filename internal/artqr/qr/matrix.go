package qr

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"

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

	// 3. Blend QR modules into canvas using Multiply luminosity blending
	scaledQR := scaleImageNearest(qrImg, pSize, pSize)
	qrBounds := scaledQR.Bounds()

	for y := 0; y < pSize; y++ {
		for x := 0; x < pSize; x++ {
			qrColor := scaledQR.At(qrBounds.Min.X+x, qrBounds.Min.Y+y)
			rQ, gQ, bQ, aQ := qrColor.RGBA()
			if aQ == 0 {
				continue
			}

			// Luminance of QR pixel
			lumQ := (rQ*299 + gQ*587 + bQ*114) / 1000 >> 8

			destX := px + x
			destY := py + y
			if destX >= canvasSize || destY >= canvasSize {
				continue
			}

			// Check if pixel is within one of the 3 Finder Pattern corner zones
			cornerSize := int(float64(pSize) * 0.28)
			isTopLeft := x < cornerSize && y < cornerSize
			isTopRight := x >= pSize-cornerSize && y < cornerSize
			isBottomLeft := x < cornerSize && y >= pSize-cornerSize
			isFinderZone := isTopLeft || isTopRight || isBottomLeft

			origColor := canvas.At(destX, destY)
			rO, gO, bO, _ := origColor.RGBA()
			r8, g8, b8 := uint8(rO>>8), uint8(gO>>8), uint8(bO>>8)

			if isFinderZone {
				// High-contrast preservation for camera scanners
				if lumQ < 128 {
					canvas.Set(destX, destY, color.RGBA{R: r8 / 5, G: g8 / 5, B: b8 / 5, A: 255})
				} else {
					canvas.Set(destX, destY, color.RGBA{R: clamp255(int(r8) + 80), G: clamp255(int(g8) + 80), B: clamp255(int(b8) + 80), A: 255})
				}
			} else {
				// Artistic multiply blending
				if lumQ < 130 {
					// Dark module: darken artwork
					canvas.Set(destX, destY, color.RGBA{
						R: uint8(int(r8) * 35 / 100),
						G: uint8(int(g8) * 35 / 100),
						B: uint8(int(b8) * 35 / 100),
						A: 255,
					})
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
