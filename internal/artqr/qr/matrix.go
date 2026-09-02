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
