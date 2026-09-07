package qrtrans

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"time"
)

// DefaultThreshold is the default cutoff for distinguishing QR modules from background.
// Values in range [200, 230] work effectively for clean and slightly compressed images.
const DefaultThreshold uint8 = 215

// FallbackThresholds is the ordered list of thresholds to try if QR validation fails.
var FallbackThresholds = []uint8{215, 200, 225, 195, 230, 180, 240, 170}

// CropMode defines how the QR region is isolated from non-QR content (logos, text, banners).
type CropMode string

const (
	// CropModeNone processes the whole image as-is.
	CropModeNone CropMode = "none"
	// CropModeCrop detects the QR code and crops the output image to the QR bounding box.
	CropModeCrop CropMode = "crop"
	// CropModeMask keeps the original dimensions but clears all pixels outside the QR code to transparent.
	CropModeMask CropMode = "mask"
)

// Options allows configuring the QR background removal process.
type Options struct {
	// Threshold overrides the default threshold if non-zero.
	Threshold uint8
	// ValidateQR specifies whether to validate input and output QR payloads.
	// Default is true.
	ValidateQR bool
	// FallbackOnValidationFailure enables auto-retrying with other thresholds if validation fails.
	FallbackOnValidationFailure bool
	// CropMode controls whether to isolate the QR code and remove other parts (card borders, text, logos).
	CropMode CropMode
}

// Result holds the processed image and metadata.
type Result struct {
	OutputImage   *image.NRGBA
	PNGData       []byte
	Width         int
	Height        int
	ThresholdUsed uint8
	InputPayload  string
	OutputPayload string
	QRValid       bool
	Retries       int
	Duration      time.Duration
	QRBounds      *image.Rectangle
}

// ProcessImageReader reads an image from an io.Reader and removes the white background.
func ProcessImageReader(r io.Reader, opts *Options) (*Result, error) {
	img, format, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("failed to decode input image (%s): %w", format, err)
	}

	return ProcessImage(img, opts)
}

// ProcessImage converts the white/light background of a QR code image to transparent RGBA.
// It preserves exact dimensions, keeps QR modules solid black (0,0,0,255),
// and turns background pixels transparent (0,0,0,0).
func ProcessImage(img image.Image, opts *Options) (*Result, error) {
	start := time.Now()

	if opts == nil {
		opts = &Options{
			Threshold:                    DefaultThreshold,
			ValidateQR:                   true,
			FallbackOnValidationFailure: true,
		}
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid image dimensions: %dx%d", width, height)
	}

	// Detect QR bounds if crop or mask is requested
	var qrBounds *image.Rectangle
	if opts.CropMode == CropModeCrop || opts.CropMode == CropModeMask {
		if rect, err := DetectQRBounds(img); err == nil {
			qrBounds = &rect
			if opts.CropMode == CropModeCrop {
				// Crop to QR rectangle
				cropped := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
				draw.Draw(cropped, cropped.Bounds(), img, rect.Min, draw.Src)
				img = cropped
				bounds = img.Bounds()
				width = bounds.Dx()
				height = bounds.Dy()
			}
		}
	}

	// 1. Decode original input payload if validation is requested
	var inputPayload string
	var inputValid bool
	if opts.ValidateQR {
		if payload, err := DecodeQR(img); err == nil && payload != "" {
			inputPayload = payload
			inputValid = true
		}
	}

	// 2. Determine candidate thresholds
	var candidates []uint8
	if opts.Threshold > 0 {
		candidates = append(candidates, opts.Threshold)
	} else {
		candidates = append(candidates, DefaultThreshold)
	}

	if opts.FallbackOnValidationFailure && inputValid {
		// Include Otsu dynamic threshold calculation
		otsu := CalculateOtsuThreshold(img)
		hasOtsu := false
		for _, c := range candidates {
			if c == otsu {
				hasOtsu = true
				break
			}
		}
		if !hasOtsu {
			candidates = append(candidates, otsu)
		}

		for _, fb := range FallbackThresholds {
			alreadyIn := false
			for _, c := range candidates {
				if c == fb {
					alreadyIn = true
					break
				}
			}
			if !alreadyIn {
				candidates = append(candidates, fb)
			}
		}
	}

	// 3. Process image with candidate thresholds and validate
	var bestResult *image.NRGBA
	var bestThreshold uint8
	var outputPayload string
	var qrValid bool
	var retriesCount int

	var maskRect *image.Rectangle
	if opts.CropMode == CropModeMask && qrBounds != nil {
		maskRect = qrBounds
	}

	for i, thresh := range candidates {
		processed := RemoveBackgroundWithThreshold(img, thresh, maskRect)
		if bestResult == nil {
			bestResult = processed
			bestThreshold = thresh
		}

		if !opts.ValidateQR || !inputValid {
			// No validation needed or input was not decodable
			bestResult = processed
			bestThreshold = thresh
			break
		}

		// Validate output by compositing over white canvas to emulate scanning
		comp := CompositeOnWhite(processed)
		outPay, err := DecodeQR(comp)
		if err == nil && outPay == inputPayload {
			bestResult = processed
			bestThreshold = thresh
			outputPayload = outPay
			qrValid = true
			retriesCount = i
			break
		}
	}

	// If QR valid check didn't pass through candidates, do a final check on bestResult
	if inputValid && !qrValid {
		comp := CompositeOnWhite(bestResult)
		if outPay, err := DecodeQR(comp); err == nil && outPay == inputPayload {
			outputPayload = outPay
			qrValid = true
		}
	}

	// 4. Encode to PNG buffer
	var buf bytes.Buffer
	enc := &png.Encoder{
		CompressionLevel: png.BestSpeed,
	}
	if err := enc.Encode(&buf, bestResult); err != nil {
		return nil, fmt.Errorf("failed to encode result PNG: %w", err)
	}

	return &Result{
		OutputImage:   bestResult,
		PNGData:       buf.Bytes(),
		Width:         width,
		Height:        height,
		ThresholdUsed: bestThreshold,
		InputPayload:  inputPayload,
		OutputPayload: outputPayload,
		QRValid:       qrValid,
		Retries:       retriesCount,
		Duration:      time.Since(start),
		QRBounds:      qrBounds,
	}, nil
}

// RemoveBackgroundWithThreshold applies the core pixel-level thresholding.
// If maskRect is provided, any pixels outside maskRect are made fully transparent.
// Dimensions are strictly maintained (bounds.Dx(), bounds.Dy()).
// QR modules: RGB = 0, 0, 0, Alpha = 255.
// Background: RGB = 0, 0, 0, Alpha = 0.
func RemoveBackgroundWithThreshold(img image.Image, threshold uint8, maskRect *image.Rectangle) *image.NRGBA {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	pix := out.Pix
	thresh32 := uint32(threshold)

	for y := 0; y < height; y++ {
		srcY := bounds.Min.Y + y
		rowOffset := y * width * 4

		for x := 0; x < width; x++ {
			srcX := bounds.Min.X + x
			idx := rowOffset + x*4

			// If a maskRect is specified and pixel is outside, make it transparent
			if maskRect != nil && (srcX < maskRect.Min.X || srcX >= maskRect.Max.X || srcY < maskRect.Min.Y || srcY >= maskRect.Max.Y) {
				pix[idx] = 0
				pix[idx+1] = 0
				pix[idx+2] = 0
				pix[idx+3] = 0
				continue
			}

			r, g, b, a := img.At(srcX, srcY).RGBA()
			a8 := a >> 8

			// If pixel in input is already transparent, keep transparent
			if a8 < 128 {
				pix[idx] = 0
				pix[idx+1] = 0
				pix[idx+2] = 0
				pix[idx+3] = 0
				continue
			}

			// Convert to 8-bit RGB
			r8 := r >> 8
			g8 := g >> 8
			b8 := b >> 8

			// ITU-R BT.601 standard photometric luminance
			lum := (299*r8 + 587*g8 + 114*b8 + 500) / 1000

			if lum < thresh32 {
				// QR module: solid black with full opacity
				pix[idx] = 0
				pix[idx+1] = 0
				pix[idx+2] = 0
				pix[idx+3] = 255
			} else {
				// Background: transparent
				pix[idx] = 0
				pix[idx+1] = 0
				pix[idx+2] = 0
				pix[idx+3] = 0
			}
		}
	}

	return out
}
