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
		if rect, ok := FindDarkModuleBounds(img); ok {
			return rect, nil
		}
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

// FindDarkModuleBounds locates the tight bounding box of dark QR modules by scanning pixels
func FindDarkModuleBounds(img image.Image) (image.Rectangle, bool) {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h <= 0 {
		return image.Rectangle{}, false
	}
	minX, minY, maxX, maxY := bounds.Max.X, bounds.Max.Y, bounds.Min.X, bounds.Min.Y
	found := false

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if (a >> 8) < 128 {
				continue
			}
			lum := (r*299 + g*587 + b*114) / 1000 >> 8
			if lum < 140 { // Dark module pixel
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
				found = true
			}
		}
	}

	if !found || maxX <= minX || maxY <= minY {
		return image.Rectangle{}, false
	}

	// Add minimal 2-module quiet margin (~5% of width)
	pad := int(float64(maxX-minX) * 0.05)
	if pad < 4 {
		pad = 4
	}

	x0 := minX - pad
	if x0 < bounds.Min.X {
		x0 = bounds.Min.X
	}
	y0 := minY - pad
	if y0 < bounds.Min.Y {
		y0 = bounds.Min.Y
	}
	x1 := maxX + pad + 1
	if x1 > bounds.Max.X {
		x1 = bounds.Max.X
	}
	y1 := maxY + pad + 1
	if y1 > bounds.Max.Y {
		y1 = bounds.Max.Y
	}

	return image.Rect(x0, y0, x1, y1), true
}
