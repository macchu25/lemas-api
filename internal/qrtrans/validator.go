package qrtrans

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// DecodeQR decodes a QR code from any image.Image.
func DecodeQR(img image.Image) (string, error) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", fmt.Errorf("failed to create binary bitmap: %w", err)
	}

	qrReader := qrcode.NewQRCodeReader()
	result, err := qrReader.Decode(bmp, nil)
	if err != nil {
		return "", fmt.Errorf("qr decode error: %w", err)
	}

	return result.GetText(), nil
}

// CompositeOnWhite blends a transparent (or any) image on top of an opaque white background.
// This simulates scanning a transparent QR code placed on a white surface / paper / screen.
func CompositeOnWhite(img image.Image) image.Image {
	bounds := img.Bounds()
	whiteBg := image.NewRGBA(bounds)
	// Fill white
	draw.Draw(whiteBg, bounds, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	// Draw transparent image over white
	draw.Draw(whiteBg, bounds, img, bounds.Min, draw.Over)
	return whiteBg
}
