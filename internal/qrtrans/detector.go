package qrtrans

import (
	"fmt"
	"image"
	"math"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// DetectQRBounds locates the bounding rectangle of the QR code in the image
// using its finder patterns, adding an appropriate quiet-zone margin.
func DetectQRBounds(img image.Image) (image.Rectangle, error) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return image.Rectangle{}, fmt.Errorf("failed to create binary bitmap: %w", err)
	}

	reader := qrcode.NewQRCodeReader()
	res, err := reader.Decode(bmp, nil)
	if err != nil {
		return image.Rectangle{}, fmt.Errorf("qr not detected: %w", err)
	}

	pts := res.GetResultPoints()
	if len(pts) < 3 {
		return image.Rectangle{}, fmt.Errorf("insufficient finder pattern points (%d)", len(pts))
	}

	// In gozxing:
	// pt 0: Bottom-Left
	// pt 1: Top-Left
	// pt 2: Top-Right
	tl := pts[1]
	tr := pts[2]
	bl := pts[0]

	dxTop := float64(tr.GetX() - tl.GetX())
	dyTop := float64(tr.GetY() - tl.GetY())
	distTop := math.Hypot(dxTop, dyTop)

	dxVert := float64(bl.GetX() - tl.GetX())
	dyVert := float64(bl.GetY() - tl.GetY())
	distVert := math.Hypot(dxVert, dyVert)

	dist := (distTop + distVert) / 2.0
	if dist <= 0 {
		return img.Bounds(), nil
	}

	// Finder pattern center is at 3.5 modules from the outer boundary.
	// Margin includes the outer finder radius plus 1-2 modules quiet zone (~18% of center-to-center distance)
	margin := dist * 0.18

	minX := int(math.Floor(math.Min(math.Min(float64(tl.GetX()), float64(bl.GetX())), float64(tr.GetX())) - margin))
	minY := int(math.Floor(math.Min(math.Min(float64(tl.GetY()), float64(bl.GetY())), float64(tr.GetY())) - margin))
	maxX := int(math.Ceil(math.Max(math.Max(float64(tl.GetX()), float64(bl.GetX())), float64(tr.GetX())) + margin))
	maxY := int(math.Ceil(math.Max(math.Max(float64(tl.GetY()), float64(bl.GetY())), float64(tr.GetY())) + margin))

	bounds := img.Bounds()
	if minX < bounds.Min.X {
		minX = bounds.Min.X
	}
	if minY < bounds.Min.Y {
		minY = bounds.Min.Y
	}
	if maxX > bounds.Max.X {
		maxX = bounds.Max.X
	}
	if maxY > bounds.Max.Y {
		maxY = bounds.Max.Y
	}

	return image.Rect(minX, minY, maxX, maxY), nil
}
