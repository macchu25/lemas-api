package qr

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"xkiro-backend/internal/artqr/model"
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
	if err == nil && decoded != nil && (expectedPayload == "" || decoded.Payload == expectedPayload) {
		return ValidationResult{
			Valid:          true,
			DecodedPayload: decoded.Payload,
			PayloadHash:    decoded.PayloadHash,
		}
	}

	img, _, imgErr := image.Decode(bytes.NewReader(imgBytes))
	if imgErr == nil {
		text := tryDecodeImage(img)
		if text != "" && (expectedPayload == "" || text == expectedPayload) {
			return ValidationResult{
				Valid:          true,
				DecodedPayload: text,
				PayloadHash:    HashPayload(text),
			}
		}

		// Try multi-threshold high contrast (normal & inverted for dark themes)
		for _, thresh := range []uint8{70, 90, 110, 130, 150, 170, 190} {
			gray := toHighContrastGrayscale(img, thresh)
			text = tryDecodeImage(gray)
			if text != "" && (expectedPayload == "" || text == expectedPayload) {
				return ValidationResult{
					Valid:          true,
					DecodedPayload: text,
					PayloadHash:    HashPayload(text),
				}
			}
			invGray := toInvertedHighContrastGrayscale(img, thresh)
			text = tryDecodeImage(invGray)
			if text != "" && (expectedPayload == "" || text == expectedPayload) {
				return ValidationResult{
					Valid:          true,
					DecodedPayload: text,
					PayloadHash:    HashPayload(text),
				}
			}
		}
	}

	// Try verification with C++ ZXing Worker endpoint (http://127.0.0.1:7860/verify)
	if zxRes := tryVerifyWithZXingWorker(imgBytes, expectedPayload); zxRes.Valid {
		return zxRes
	}

	return ValidationResult{
		Valid: false,
		Error: "qr not recognized by decoder",
	}
}

// ValidateGeneratedQRWithPlacement tests full image and framed camera viewfinder crop
func ValidateGeneratedQRWithPlacement(imgBytes []byte, expectedPayload string, p model.Placement) ValidationResult {
	res := ValidateGeneratedQR(imgBytes, expectedPayload)
	if res.Valid {
		return res
	}

	if len(imgBytes) == 0 || !p.IsValid() {
		return res
	}

	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return res
	}

	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	minDim := w
	if h < minDim {
		minDim = h
	}

	pSize := int(p.Size * float64(minDim))
	margin := int(float64(pSize) * 0.18)
	x0 := bounds.Min.X + int(p.X*float64(w)) - margin
	y0 := bounds.Min.Y + int(p.Y*float64(h)) - margin
	x1 := x0 + pSize + 2*margin
	y1 := y0 + pSize + 2*margin

	if x0 < bounds.Min.X {
		x0 = bounds.Min.X
	}
	if y0 < bounds.Min.Y {
		y0 = bounds.Min.Y
	}
	if x1 > bounds.Max.X {
		x1 = bounds.Max.X
	}
	if y1 > bounds.Max.Y {
		y1 = bounds.Max.Y
	}

	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := img.(subImager); ok && x1 > x0 && y1 > y0 {
		crop := si.SubImage(image.Rect(x0, y0, x1, y1))
		if text := tryDecodeImage(crop); text != "" && (expectedPayload == "" || text == expectedPayload) {
			return ValidationResult{Valid: true, DecodedPayload: text, PayloadHash: HashPayload(text)}
		}
		for _, thresh := range []uint8{70, 90, 110, 130, 150, 170, 190} {
			gray := toHighContrastGrayscale(crop, thresh)
			if text := tryDecodeImage(gray); text != "" && (expectedPayload == "" || text == expectedPayload) {
				return ValidationResult{Valid: true, DecodedPayload: text, PayloadHash: HashPayload(text)}
			}
			invGray := toInvertedHighContrastGrayscale(crop, thresh)
			if text := tryDecodeImage(invGray); text != "" && (expectedPayload == "" || text == expectedPayload) {
				return ValidationResult{Valid: true, DecodedPayload: text, PayloadHash: HashPayload(text)}
			}
		}
	}

	// Final verification with C++ ZXing Worker (supports rotated/stylized/camera perspectives)
	if zxRes := tryVerifyWithZXingWorker(imgBytes, expectedPayload); zxRes.Valid {
		return zxRes
	}

	return res
}

func tryVerifyWithZXingWorker(imgBytes []byte, expectedPayload string) ValidationResult {
	if len(imgBytes) == 0 {
		return ValidationResult{Valid: false}
	}
	workerURL := os.Getenv("ARTQR_WORKER_URL")
	if workerURL == "" {
		workerURL = "http://127.0.0.1:7860"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(strings.TrimRight(workerURL, "/")+"/verify", "application/octet-stream", bytes.NewReader(imgBytes))
	if err != nil || resp.StatusCode != http.StatusOK {
		return ValidationResult{Valid: false}
	}
	defer resp.Body.Close()

	var result struct {
		Valid bool   `json:"valid"`
		Text  string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.Valid {
		if expectedPayload == "" || result.Text == expectedPayload {
			return ValidationResult{
				Valid:          true,
				DecodedPayload: result.Text,
				PayloadHash:    HashPayload(result.Text),
			}
		}
	}
	return ValidationResult{Valid: false}
}

