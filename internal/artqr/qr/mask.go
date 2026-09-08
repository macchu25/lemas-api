package qr

import (
	"bytes"
	"errors"
	_ "golang.org/x/image/webp"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/makiuchi-d/gozxing/qrcode/decoder"
	"github.com/makiuchi-d/gozxing/qrcode/encoder"
	"xkiro-backend/internal/artqr/model"
)

// BinaryQRMask contains locked binary masks for QR reconstruction
type BinaryQRMask struct {
	Width            int
	Height           int
	DarkMask         [][]bool // true = dark module/pixel, false = light/transparent
	ModuleDim        int      // e.g. 21, 25, 29, 33 (QR grid modules)
	ModuleGrid       [][]bool // [r][c] true if dark module
	QuietZoneModules int      // default 4
	Placement        model.Placement
	TargetRect       image.Rectangle
}

// BuildBinaryQRMaskFromPayload regenerates the authoritative QR module matrix
// from the decoded payload. This avoids guessing module boundaries from an
// uploaded screenshot or a background-removed/anti-aliased image.
func BuildBinaryQRMaskFromPayload(payload string, targetWidth, targetHeight int, p model.Placement, quietZone int) (*BinaryQRMask, error) {
	if payload == "" {
		return nil, errors.New("empty QR payload")
	}
	if targetWidth <= 0 {
		targetWidth = 1024
	}
	if targetHeight <= 0 {
		targetHeight = 1024
	}
	if quietZone <= 0 {
		quietZone = 4
	}
	if !p.IsValid() {
		p = model.DefaultPlacement()
	}

	code, err := encoder.Encoder_encode(payload, decoder.ErrorCorrectionLevel_H, nil)
	if err != nil || code.GetMatrix() == nil {
		return nil, errors.New("cannot regenerate QR module matrix")
	}
	matrix := code.GetMatrix()
	modDim := matrix.GetWidth()
	moduleGrid := make([][]bool, modDim)
	for row := 0; row < modDim; row++ {
		moduleGrid[row] = make([]bool, modDim)
		for col := 0; col < modDim; col++ {
			moduleGrid[row][col] = matrix.Get(col, row) == 1
		}
	}

	minDim := targetWidth
	if targetHeight < minDim {
		minDim = targetHeight
	}
	pSize := int(p.Size * float64(minDim))
	px := int(p.X * float64(targetWidth))
	py := int(p.Y * float64(targetHeight))
	if px < 0 {
		px = 0
	}
	if py < 0 {
		py = 0
	}
	if px+pSize > targetWidth {
		pSize = targetWidth - px
	}
	if py+pSize > targetHeight {
		pSize = targetHeight - py
	}
	if pSize < modDim {
		return nil, errors.New("QR placement is too small for its payload")
	}
	targetRect := image.Rect(px, py, px+pSize, py+pSize)
	darkMask := make([][]bool, targetHeight)
	for y := range darkMask {
		darkMask[y] = make([]bool, targetWidth)
	}
	modulePixels := float64(pSize) / float64(modDim)
	for row := 0; row < modDim; row++ {
		y0, y1 := py+int(float64(row)*modulePixels), py+int(float64(row+1)*modulePixels)
		for col := 0; col < modDim; col++ {
			if !moduleGrid[row][col] {
				continue
			}
			x0, x1 := px+int(float64(col)*modulePixels), px+int(float64(col+1)*modulePixels)
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					darkMask[y][x] = true
				}
			}
		}
	}
	return &BinaryQRMask{Width: targetWidth, Height: targetHeight, DarkMask: darkMask,
		ModuleDim: modDim, ModuleGrid: moduleGrid, QuietZoneModules: quietZone,
		Placement: p, TargetRect: targetRect}, nil
}

// BuildBinaryQRMask builds an authoritative pixel-locked binary mask from the cleaned QR image
func BuildBinaryQRMask(cleanQRPNG []byte, targetWidth, targetHeight int, p model.Placement, quietZone int) (*BinaryQRMask, error) {
	if len(cleanQRPNG) == 0 {
		return nil, errors.New("empty clean QR image")
	}

	srcImg, _, err := image.Decode(bytes.NewReader(cleanQRPNG))
	if err != nil {
		return nil, err
	}

	srcBounds := srcImg.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return nil, errors.New("invalid QR dimensions")
	}

	if targetWidth <= 0 {
		targetWidth = 1024
	}
	if targetHeight <= 0 {
		targetHeight = 1024
	}
	if quietZone <= 0 {
		quietZone = 4
	}

	if !p.IsValid() {
		p = model.DefaultPlacement()
	}

	// 1. Detect QR module dimension by scanning timing pattern row/column
	// First convert srcImg to 2D bool grid
	srcGrid := make([][]bool, srcH)
	for y := 0; y < srcH; y++ {
		srcGrid[y] = make([]bool, srcW)
		sY := srcBounds.Min.Y + y
		for x := 0; x < srcW; x++ {
			sX := srcBounds.Min.X + x
			r, g, b, a := srcImg.At(sX, sY).RGBA()
			a8 := a >> 8
			if a8 < 128 {
				// Transparent background = light
				srcGrid[y][x] = false
				continue
			}
			lum := (r*299 + g*587 + b*114) / 1000 >> 8
			// Dark module if lum < 128
			srcGrid[y][x] = lum < 128
		}
	}

	// 2. Find bounding box of actual QR modules in the clean image
	minX, minY, maxX, maxY := srcW, srcH, -1, -1
	for y := 0; y < srcH; y++ {
		for x := 0; x < srcW; x++ {
			if srcGrid[y][x] {
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
			}
		}
	}

	if minX > maxX || minY > maxY {
		return nil, errors.New("no dark modules found in cleaned QR")
	}

	qrContentW := maxX - minX + 1
	qrContentH := maxY - minY + 1

	// Estimate module count (version 1: 21, v2: 25, v3: 29, v4: 33...)
	// In standard QR codes, finder pattern is 7x7 modules.
	// We can estimate module size from finder pattern top-left
	estimatedModSize := 1.0
	// Find top-left finder pattern width
	finderBlackCount := 0
	for x := minX; x <= maxX; x++ {
		if srcGrid[minY][x] {
			finderBlackCount++
		} else {
			break
		}
	}
	if finderBlackCount >= 5 {
		// Finder outer bar is 7 modules wide in standard QR
		estimatedModSize = float64(finderBlackCount) / 7.0
	} else {
		estimatedModSize = float64(qrContentW) / 29.0
	}
	if estimatedModSize < 1.0 {
		estimatedModSize = 1.0
	}

	modCountFloat := float64(qrContentW) / estimatedModSize
	// Round to nearest valid QR dimension (4*N + 17 for N >= 1, e.g. 21, 25, 29, 33, 37...)
	modDim := 21
	bestDiff := 999.0
	for n := 1; n <= 40; n++ {
		dim := 4*n + 17
		diff := modCountFloat - float64(dim)
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff = diff
			modDim = dim
		}
	}

	// Build exact 2D module grid [r][c]
	moduleGrid := make([][]bool, modDim)
	for r := 0; r < modDim; r++ {
		moduleGrid[r] = make([]bool, modDim)
		centerSrcY := minY + int((float64(r)+0.5)*float64(qrContentH)/float64(modDim))
		if centerSrcY >= srcH {
			centerSrcY = srcH - 1
		}
		for c := 0; c < modDim; c++ {
			centerSrcX := minX + int((float64(c)+0.5)*float64(qrContentW)/float64(modDim))
			if centerSrcX >= srcW {
				centerSrcX = srcW - 1
			}
			moduleGrid[r][c] = srcGrid[centerSrcY][centerSrcX]
		}
	}

	// 3. Compute target canvas placement
	minDim := targetWidth
	if targetHeight < minDim {
		minDim = targetHeight
	}
	pSize := int(p.Size * float64(minDim))
	if pSize < 32 {
		pSize = 32
	}

	px := int(p.X * float64(targetWidth))
	py := int(p.Y * float64(targetHeight))

	if px < 0 {
		px = 0
	}
	if py < 0 {
		py = 0
	}
	if px+pSize > targetWidth {
		pSize = targetWidth - px
	}
	if py+pSize > targetHeight {
		pSize = targetHeight - py
	}

	targetRect := image.Rect(px, py, px+pSize, py+pSize)

	// 4. Build pixel-perfect binary dark_mask on target dimensions using Nearest-Neighbor scaling
	darkMask := make([][]bool, targetHeight)
	for y := 0; y < targetHeight; y++ {
		darkMask[y] = make([]bool, targetWidth)
	}

	modPixelSize := float64(pSize) / float64(modDim)
	for r := 0; r < modDim; r++ {
		yStart := py + int(float64(r)*modPixelSize)
		yEnd := py + int(float64(r+1)*modPixelSize)
		if yEnd > targetHeight {
			yEnd = targetHeight
		}

		for c := 0; c < modDim; c++ {
			if !moduleGrid[r][c] {
				continue // light module
			}
			xStart := px + int(float64(c)*modPixelSize)
			xEnd := px + int(float64(c+1)*modPixelSize)
			if xEnd > targetWidth {
				xEnd = targetWidth
			}

			// Strictly fill the exact square boundary with zero anti-aliasing
			for pyy := yStart; pyy < yEnd; pyy++ {
				for pxx := xStart; pxx < xEnd; pxx++ {
					darkMask[pyy][pxx] = true
				}
			}
		}
	}

	return &BinaryQRMask{
		Width:            targetWidth,
		Height:           targetHeight,
		DarkMask:         darkMask,
		ModuleDim:        modDim,
		ModuleGrid:       moduleGrid,
		QuietZoneModules: quietZone,
		Placement:        p,
		TargetRect:       targetRect,
	}, nil
}

// IsQuietZone returns true if coordinates (x, y) fall inside the clean quiet zone margin around QR
func (m *BinaryQRMask) IsQuietZone(x, y int) bool {
	if m.TargetRect.Empty() {
		return false
	}
	modPixelSize := float64(m.TargetRect.Dx()) / float64(m.ModuleDim)
	marginPixels := int(float64(m.QuietZoneModules) * modPixelSize)

	qzMinX := m.TargetRect.Min.X - marginPixels
	qzMinY := m.TargetRect.Min.Y - marginPixels
	qzMaxX := m.TargetRect.Max.X + marginPixels
	qzMaxY := m.TargetRect.Max.Y + marginPixels

	// Inside extended bounding box but outside QR modules
	if x >= qzMinX && x < qzMaxX && y >= qzMinY && y < qzMaxY {
		// Inside quiet zone boundary
		if x < m.TargetRect.Min.X || x >= m.TargetRect.Max.X || y < m.TargetRect.Min.Y || y >= m.TargetRect.Max.Y {
			return true
		}
	}
	return false
}

// ToDebugMaskImage produces an 8-bit black & white PNG representation of the binary mask
func (m *BinaryQRMask) ToDebugMaskImage() *image.Gray {
	gray := image.NewGray(image.Rect(0, 0, m.Width, m.Height))
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			if m.DarkMask[y][x] {
				gray.SetGray(x, y, color.Gray{Y: 0}) // black module
			} else {
				gray.SetGray(x, y, color.Gray{Y: 255}) // white background
			}
		}
	}
	return gray
}
