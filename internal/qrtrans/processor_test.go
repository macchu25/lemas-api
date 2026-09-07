package qrtrans

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	qrcode "github.com/skip2/go-qrcode"
)

func TestQRTransWorkflow(t *testing.T) {
	// Generate a clean test QR
	text := "https://nornai.com/test-qr-trans"
	qrPng, err := qrcode.Encode(text, qrcode.Medium, 256)
	if err != nil {
		t.Fatalf("failed to encode QR: %v", err)
	}

	img, err := png.Decode(bytes.NewReader(qrPng))
	if err != nil {
		t.Fatalf("failed to decode generated QR: %v", err)
	}

	// 1. Test ProcessImage with Default Options
	res, err := ProcessImage(img, nil)
	if err != nil {
		t.Fatalf("ProcessImage failed: %v", err)
	}

	if !res.QRValid {
		t.Errorf("Expected QR to be valid, got false")
	}
	if res.OutputPayload != text {
		t.Errorf("Expected payload %q, got %q", text, res.OutputPayload)
	}
	if res.Width != 256 || res.Height != 256 {
		t.Errorf("Unexpected dimensions: %dx%d", res.Width, res.Height)
	}

	// Check that top-left corner (quiet zone) is transparent
	cornerColor := res.OutputImage.At(0, 0)
	_, _, _, a := cornerColor.RGBA()
	if a != 0 {
		t.Errorf("Expected quiet zone pixel to be transparent, got alpha=%d", a)
	}
}

func TestQRTransCropAndMask(t *testing.T) {
	text := "https://example.com/crop-test"
	qrPng, err := qrcode.Encode(text, qrcode.Medium, 200)
	if err != nil {
		t.Fatalf("failed to generate QR: %v", err)
	}
	qrImg, _ := png.Decode(bytes.NewReader(qrPng))

	// Create larger canvas (e.g. 400x400) and place QR in center with some mock border
	canvas := image.NewRGBA(image.Rect(0, 0, 400, 400))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(100, 100, 300, 300), qrImg, image.Point{}, draw.Src)

	// Test Crop mode
	cropRes, err := ProcessImage(canvas, &Options{
		CropMode:   CropModeCrop,
		ValidateQR: true,
	})
	if err != nil {
		t.Fatalf("ProcessImage with Crop failed: %v", err)
	}
	if !cropRes.QRValid {
		t.Errorf("Crop mode QR validation failed")
	}
	if cropRes.Width > 350 || cropRes.Height > 350 {
		t.Errorf("Crop didn't reduce dimensions properly: %dx%d", cropRes.Width, cropRes.Height)
	}

	// Test Mask mode
	maskRes, err := ProcessImage(canvas, &Options{
		CropMode:   CropModeMask,
		ValidateQR: true,
	})
	if err != nil {
		t.Fatalf("ProcessImage with Mask failed: %v", err)
	}
	if maskRes.Width != 400 || maskRes.Height != 400 {
		t.Errorf("Mask mode should preserve canvas size: %dx%d", maskRes.Width, maskRes.Height)
	}
}
