package qr

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
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

// DecodeQRCode reads image bytes, decodes the QR code with gozxing, and returns normalized PNG & metadata
func DecodeQRCode(raw []byte) (*DecodedQR, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty image data")
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("unsupported image format")
	}
	if config.Width < 64 || config.Height < 64 || config.Width > 4096 || config.Height > 4096 {
		return nil, errors.New("invalid QR image dimensions (must be between 64x64 and 4096x4096)")
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("failed to decode image data")
	}

	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return nil, errors.New("failed to parse binary bitmap from image")
	}

	hints := map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_TRY_HARDER: true,
	}

	result, err := qrcode.NewQRCodeReader().Decode(bitmap, hints)
	if err != nil {
		return nil, errors.New("không thể quét thấy mã QR trong ảnh tải lên")
	}

	payload := strings.TrimSpace(result.GetText())
	if payload == "" {
		return nil, errors.New("mã QR không chứa nội dung")
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
