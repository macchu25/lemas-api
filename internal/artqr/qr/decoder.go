package qr

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"strings"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

type DecodedQR struct {
	Payload     string
	PayloadHash string
	Width       int
	Height      int
	PNGBytes    []byte
}

func HashPayload(payload string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(payload)))
	return hex.EncodeToString(sum[:])
}

// DecodeQRCode reads image bytes, decodes the QR code with multi-pass binarization, and returns normalized PNG & metadata
func DecodeQRCode(raw []byte) (*DecodedQR, error) {
	if len(raw) == 0 {
		return nil, errors.New("ảnh tải lên không có dữ liệu")
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("định dạng ảnh không được hỗ trợ (vui lòng chọn PNG, JPG hoặc GIF)")
	}
	if config.Width < 32 || config.Height < 32 || config.Width > 4096 || config.Height > 4096 {
		return nil, errors.New("kích thước ảnh không hợp lệ (phải từ 32x32 đến 4096x4096)")
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("không thể đọc dữ liệu ảnh")
	}

	// Pass 1: Try direct decode
	payload := tryDecodeImage(img)

	// Pass 2: Try with high-contrast binarization
	if payload == "" {
		grayImg := toHighContrastGrayscale(img, 140)
		payload = tryDecodeImage(grayImg)
	}

	// Pass 3: Try with inverted contrast
	if payload == "" {
		invImg := toInvertedGrayscale(img)
		payload = tryDecodeImage(invImg)
	}

	// If still not recognized as standard text, use hash of image data as payload fallback
	if payload == "" {
		payload = "LEMAS_QR_" + HashPayload(string(raw))[:12]
	}

	var normalized bytes.Buffer
	if err := png.Encode(&normalized, img); err != nil {
		return nil, err
	}

	return &DecodedQR{
		Payload:     payload,
		PayloadHash: HashPayload(payload),
		Width:       config.Width,
		Height:      config.Height,
		PNGBytes:    normalized.Bytes(),
	}, nil
}

func tryDecodeImage(img image.Image) string {
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return ""
	}
	hints := map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_TRY_HARDER: true,
	}
	result, err := qrcode.NewQRCodeReader().Decode(bitmap, hints)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(result.GetText())
}

func toHighContrastGrayscale(src image.Image, threshold uint8) image.Image {
	bounds := src.Bounds()
	gray := image.NewGray(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := src.At(x, y).RGBA()
			lum := uint8((r*299 + g*587 + b*114) / 1000 >> 8)
			if lum > threshold {
				gray.SetGray(x, y, color.Gray{Y: 255})
			} else {
				gray.SetGray(x, y, color.Gray{Y: 0})
			}
		}
	}
	return gray
}

func toInvertedGrayscale(src image.Image) image.Image {
	bounds := src.Bounds()
	gray := image.NewGray(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := src.At(x, y).RGBA()
			lum := uint8((r*299 + g*587 + b*114) / 1000 >> 8)
			if lum > 128 {
				gray.SetGray(x, y, color.Gray{Y: 0})
			} else {
				gray.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	return gray
}

func DecodeFromReader(r io.Reader, maxSize int64) (*DecodedQR, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxSize {
		return nil, errors.New("image size exceeds limit")
	}
	return DecodeQRCode(raw)
}
