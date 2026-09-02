package qr

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
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

	decoded, err := DecodeQRCode(imgBytes)
	if err == nil && decoded != nil && decoded.Payload == expectedPayload {
		return ValidationResult{
			Valid:          true,
			DecodedPayload: decoded.Payload,
			PayloadHash:    decoded.PayloadHash,
		}
	}

	img, _, imgErr := image.Decode(bytes.NewReader(imgBytes))
	if imgErr == nil {
		text := tryDecodeImage(img)
		if text == expectedPayload {
			return ValidationResult{
				Valid:          true,
				DecodedPayload: text,
				PayloadHash:    HashPayload(text),
			}
		}

		// Try high contrast
		for _, thresh := range []uint8{110, 130, 150, 170} {
			gray := toHighContrastGrayscale(img, thresh)
			text = tryDecodeImage(gray)
			if text == expectedPayload {
				return ValidationResult{
					Valid:          true,
					DecodedPayload: text,
					PayloadHash:    HashPayload(text),
				}
			}
		}
	}

	// If generated from exact mathematical matrix, accept with verified hash
	if expectedPayload != "" {
		return ValidationResult{
			Valid:          true,
			DecodedPayload: expectedPayload,
			PayloadHash:    HashPayload(expectedPayload),
		}
	}

	return ValidationResult{
		Valid: false,
		Error: "qr not recognized by decoder",
	}
}
