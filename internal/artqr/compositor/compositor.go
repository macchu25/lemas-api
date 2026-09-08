package compositor

import (
	"bytes"
	"encoding/hex"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strings"

	"xkiro-backend/internal/artqr/model"
	"xkiro-backend/internal/artqr/qr"
)

// SafetyConfig controls the deterministic restoration aggressiveness and scannability guards
type SafetyConfig struct {
	TextureStrength       float64 // 0.0 (pure dark color) to 1.0 (raw MachGen texture)
	ContrastMultiplier    float64 // > 1.0 increases contrast between dark & light
	MaxDarkLuminance      uint8   // Upper limit on dark module pixel brightness (prevents holes)
	MinLightLuminance     uint8   // Lower limit on light module pixel brightness (ensures scannability)
	SolidifyFinderPattern bool    // Force 100% crisp dark tone on finder pattern eyes
	CleanQuietZone        bool    // Wipe any stray artifacts in the 4-module quiet zone margin
}

func DefaultSafetyConfig(preset model.ArtQRPreset) SafetyConfig {
	texStr := preset.TextureStrength
	if texStr <= 0 {
		texStr = 0.12
	}
	contrast := preset.ContrastStrength
	if contrast <= 0 {
		contrast = 0.85
	}

	maxLum := uint8(115)
	if contrast > 0.80 {
		maxLum = 95
	}

	return SafetyConfig{
		TextureStrength:       texStr,
		ContrastMultiplier:    contrast,
		MaxDarkLuminance:      maxLum,
		MinLightLuminance:     185,
		SolidifyFinderPattern: true,
		CleanQuietZone:        true,
	}
}

// ParseHexColor parses a color hex string like "#6b3218" or "6b3218" into RGB
func ParseHexColor(hexStr string) (r, g, b uint8) {
	hexStr = strings.TrimPrefix(hexStr, "#")
	if len(hexStr) == 3 {
		hexStr = fmt.Sprintf("%c%c%c%c%c%c", hexStr[0], hexStr[0], hexStr[1], hexStr[1], hexStr[2], hexStr[2])
	}
	if len(hexStr) != 6 {
		// Default to rich toasted brown
		return 107, 50, 24
	}
	bytes, err := hex.DecodeString(hexStr)
	if err != nil || len(bytes) < 3 {
		return 107, 50, 24
	}
	return bytes[0], bytes[1], bytes[2]
}

// RestoreAndComposite performs deterministic QR restoration on MachGen output
func RestoreAndComposite(
	baseSceneBytes []byte,
	machgenOutputBytes []byte,
	binaryMask *qr.BinaryQRMask,
	preset model.ArtQRPreset,
	safety SafetyConfig,
) ([]byte, error) {
	if binaryMask == nil {
		return nil, fmt.Errorf("binary mask is required")
	}

	targetW := binaryMask.Width
	targetH := binaryMask.Height

	// 1. Determine canvas foundation:
	// If MachGen generated an artistic image (machgenOutputBytes), use it as the primary canvas!
	// This ensures the AI's lighting, texture, and blended scene form the actual image,
	// rather than discarding the AI image and using a raw static photo.
	var canvas *image.RGBA
	var machgenImg image.Image

	if len(machgenOutputBytes) > 0 {
		mImg, _, err := image.Decode(bytes.NewReader(machgenOutputBytes))
		if err == nil {
			canvas = scaleImage(mImg, targetW, targetH)
			machgenImg = canvas
		}
	}

	if canvas == nil && len(baseSceneBytes) > 0 {
		baseImg, _, err := image.Decode(bytes.NewReader(baseSceneBytes))
		if err == nil {
			canvas = scaleImage(baseImg, targetW, targetH)
			machgenImg = canvas
		}
	}

	if canvas == nil {
		// Neutral warm canvas if no images available
		canvas = image.NewRGBA(image.Rect(0, 0, targetW, targetH))
		draw.Draw(canvas, canvas.Bounds(), &image.Uniform{color.RGBA{R: 245, G: 235, B: 220, A: 255}}, image.Point{}, draw.Src)
		machgenImg = canvas
	}

	targetDarkR, targetDarkG, targetDarkB := ParseHexColor(preset.DarkColor)
	modDim := binaryMask.ModuleDim
	modSize := float64(binaryMask.TargetRect.Dx()) / float64(modDim)
	px := binaryMask.TargetRect.Min.X
	py := binaryMask.TargetRect.Min.Y

	// Pre-calculate finder pattern bounding zones (top-left, top-right, bottom-left 7x7)
	isFinderPixel := func(x, y int) bool {
		if x < px || y < py || x >= binaryMask.TargetRect.Max.X || y >= binaryMask.TargetRect.Max.Y {
			return false
		}
		c := int(float64(x-px) / modSize)
		r := int(float64(y-py) / modSize)
		if c < 0 || c >= modDim || r < 0 || r >= modDim {
			return false
		}
		// Finder zones: (0..6, 0..6), (0..6, dim-7..dim-1), (dim-7..dim-1, 0..6)
		inTL := r <= 7 && c <= 7
		inTR := r <= 7 && c >= modDim-8
		inBL := r >= modDim-8 && c <= 7
		return inTL || inTR || inBL
	}

	// 3. Deterministic Module Restoration Loop
	for y := 0; y < targetH; y++ {
		for x := 0; x < targetW; x++ {
			isDark := binaryMask.DarkMask[y][x]

			// Case A: Dark QR Module Pixel
			if isDark {
				mR, mG, mB, _ := machgenImg.At(x, y).RGBA()
				mR8 := uint8(mR >> 8)
				mG8 := uint8(mG >> 8)
				mB8 := uint8(mB >> 8)

				// Module safety rule: Blend MachGen texture with target dark color
				// TextureStrength determines how much color variation is allowed
				blendFactor := safety.TextureStrength
				if blendFactor > 1.0 {
					blendFactor = 1.0
				}
				if blendFactor < 0.0 {
					blendFactor = 0.0
				}

				// Sampled color blended with authoritative preset dark tone
				finalR := float64(targetDarkR)*(1.0-blendFactor) + float64(mR8)*blendFactor
				finalG := float64(targetDarkG)*(1.0-blendFactor) + float64(mG8)*blendFactor
				finalB := float64(targetDarkB)*(1.0-blendFactor) + float64(mB8)*blendFactor

				// Check luminance safety constraint: Never let dark modules punch holes!
				currentLum := uint8((299*uint32(finalR) + 587*uint32(finalG) + 114*uint32(finalB)) / 1000)
				if currentLum > safety.MaxDarkLuminance {
					clampRatio := float64(safety.MaxDarkLuminance) / float64(currentLum)
					finalR *= clampRatio
					finalG *= clampRatio
					finalB *= clampRatio
				}

				// Special rule for Finder Patterns: keep extra crisp & high contrast
				if safety.SolidifyFinderPattern && isFinderPixel(x, y) {
					// Pull closer to deepest rich tone for finder reliability
					finderFactor := 0.25
					finalR = finalR*finderFactor + (float64(targetDarkR)*0.75)*(1.0-finderFactor)
					finalG = finalG*finderFactor + (float64(targetDarkG)*0.75)*(1.0-finderFactor)
					finalB = finalB*finderFactor + (float64(targetDarkB)*0.75)*(1.0-finderFactor)
				}

				canvas.Set(x, y, color.RGBA{
					R: clamp255(int(finalR)),
					G: clamp255(int(finalG)),
					B: clamp255(int(finalB)),
					A: 255,
				})
				continue
			}

			// Case B: Light QR Module or Quiet Zone
			inTarget := (x >= px && x < binaryMask.TargetRect.Max.X && y >= py && y < binaryMask.TargetRect.Max.Y)
			if inTarget {
				// Light QR Module inside the QR code matrix
				cR, cG, cB, _ := canvas.At(x, y).RGBA()
				cR8 := uint8(cR >> 8)
				cG8 := uint8(cG >> 8)
				cB8 := uint8(cB >> 8)
				cLum := uint8((299*uint32(cR8) + 587*uint32(cG8) + 114*uint32(cB8)) / 1000)

				targetMinLum := safety.MinLightLuminance
				if targetMinLum < 160 {
					targetMinLum = 160
				}
				// Finder pattern light separator ring requires maximum contrast
				if safety.SolidifyFinderPattern && isFinderPixel(x, y) {
					if targetMinLum < 220 {
						targetMinLum = 220
					}
				}

				if cLum < targetMinLum {
					// Guarantee the requested luminance. The old proportional boost
					// could still leave black scene pixels below the QR threshold.
					warmR := clamp255(int(targetMinLum) + 15)
					warmG := clamp255(int(targetMinLum) + 7)
					canvas.Set(x, y, color.RGBA{R: warmR, G: warmG, B: targetMinLum, A: 255})
				}
				continue
			}

			// Quiet zone luminance lift (prevents dark background from bleeding into finder patterns while preserving texture)
			if safety.CleanQuietZone && binaryMask.IsQuietZone(x, y) {
				cR, cG, cB, _ := canvas.At(x, y).RGBA()
				cR8 := uint8(cR >> 8)
				cG8 := uint8(cG >> 8)
				cB8 := uint8(cB >> 8)
				cLum := uint8((299*uint32(cR8) + 587*uint32(cG8) + 114*uint32(cB8)) / 1000)
				if cLum < 195 {
					liftFactor := float64(195-cLum) / 195.0 * 0.75
					newR := clamp255(int(float64(cR8)*(1-liftFactor) + 250*liftFactor))
					newG := clamp255(int(float64(cG8)*(1-liftFactor) + 245*liftFactor))
					newB := clamp255(int(float64(cB8)*(1-liftFactor) + 230*liftFactor))
					canvas.Set(x, y, color.RGBA{R: newR, G: newG, B: newB, A: 255})
				}
			}
		}
	}

	var buf bytes.Buffer
	enc := &png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, canvas); err != nil {
		return nil, fmt.Errorf("failed to encode composited Art QR: %w", err)
	}

	return buf.Bytes(), nil
}

// BuildGenerationGuide places the already background-removed QR over the
// reference scene at the configured placement. The image model receives this
// single composed image as its edit source, so it can integrate the QR into the
// scene instead of ignoring a non-standard second reference field.
func BuildGenerationGuide(baseSceneBytes, cleanedQRBytes []byte, placement model.Placement) ([]byte, error) {
	base, _, err := image.Decode(bytes.NewReader(baseSceneBytes))
	if err != nil {
		return nil, fmt.Errorf("invalid reference scene: %w", err)
	}
	qrImage, _, err := image.Decode(bytes.NewReader(cleanedQRBytes))
	if err != nil {
		return nil, fmt.Errorf("invalid cleaned QR: %w", err)
	}
	if !placement.IsValid() {
		placement = model.DefaultPlacement()
	}
	canvas := scaleImage(base, 1024, 1024)
	size := int(placement.Size * 1024)
	x0, y0 := int(placement.X*1024), int(placement.Y*1024)
	qrBounds := qrImage.Bounds()
	if size <= 0 || qrBounds.Empty() {
		return nil, fmt.Errorf("invalid guide placement")
	}
	for y := 0; y < size && y0+y < 1024; y++ {
		sy := qrBounds.Min.Y + y*qrBounds.Dy()/size
		for x := 0; x < size && x0+x < 1024; x++ {
			sx := qrBounds.Min.X + x*qrBounds.Dx()/size
			r, g, b, a := qrImage.At(sx, sy).RGBA()
			if a == 0 {
				continue
			}
			alpha := float64(a) / 65535.0
			br, bg, bb, _ := canvas.At(x0+x, y0+y).RGBA()
			canvas.Set(x0+x, y0+y, color.RGBA{
				R: uint8((float64(r>>8) * alpha) + (float64(br>>8) * (1 - alpha))),
				G: uint8((float64(g>>8) * alpha) + (float64(bg>>8) * (1 - alpha))),
				B: uint8((float64(b>>8) * alpha) + (float64(bb>>8) * (1 - alpha))), A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
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

func scaleImage(src image.Image, targetWidth, targetHeight int) *image.RGBA {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	if srcW == targetWidth && srcH == targetHeight {
		draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
		return dst
	}

	for y := 0; y < targetHeight; y++ {
		srcY := bounds.Min.Y + (y * srcH / targetHeight)
		for x := 0; x < targetWidth; x++ {
			srcX := bounds.Min.X + (x * srcW / targetWidth)
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}

	return dst
}
