package qr

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

type ValidationResult struct {
	Valid          bool
	DecodedPayload string
	PayloadHash    string
	Error          string
}

// ValidateGeneratedQR inspects an AI-generated image and tests if its decoded payload strictly matches the expected original payload
func ValidateGeneratedQR(imgBytes []byte, expectedPayload string) ValidationResult {
	if len(imgBytes) == 0 {
		return ValidationResult{Valid: false, Error: "empty image"}
	}

	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return ValidationResult{Valid: false, Error: "cannot decode image format"}
	}

	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return ValidationResult{Valid: false, Error: "cannot create bitmap"}
	}

	hints := map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_TRY_HARDER: true,
	}

	result, err := qrcode.NewQRCodeReader().Decode(bitmap, hints)
	if err != nil {
		return ValidationResult{Valid: false, Error: "qr not recognized by decoder"}
	}

	decoded := strings.TrimSpace(result.GetText())
	expected := strings.TrimSpace(expectedPayload)

	if decoded == expected {
		return ValidationResult{
			Valid:          true,
			DecodedPayload: decoded,
			PayloadHash:    HashPayload(decoded),
		}
	}

	return ValidationResult{
		Valid:          false,
		DecodedPayload: decoded,
		PayloadHash:    HashPayload(decoded),
		Error:          "payload mismatch",
	}
}
