package qr

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"xkiro-backend/internal/artqr/model"
)

func testQR(t *testing.T, payload string) []byte {
	t.Helper()
	m, err := qrcode.NewQRCodeWriter().EncodeWithoutHint(payload, gozxing.BarcodeFormat_QR_CODE, 256, 256)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewGray(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			if !m.Get(x, y) {
				img.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPayloadIsPreservedExactly(t *testing.T) {
	payload := " https://example.com/test \n"
	raw := testQR(t, payload)
	decoded, err := DecodeQRCode(raw)
	if err != nil || decoded.Payload != payload {
		t.Fatalf("payload changed: %#v %v", decoded, err)
	}
	if !ValidateGeneratedQR(raw, payload).Valid {
		t.Fatal("exact QR rejected")
	}
	if ValidateGeneratedQR(raw, "https://example.com/test").Valid {
		t.Fatal("trimmed payload accepted")
	}
	if ValidateGeneratedQR(raw, "https://example.com/other").Valid {
		t.Fatal("wrong payload accepted")
	}
	if HashPayload(payload) == HashPayload("https://example.com/test") {
		t.Fatal("hash normalizes payload")
	}
}

func TestUnreadableQRIsNotInvented(t *testing.T) {
	var b bytes.Buffer
	png.Encode(&b, image.NewGray(image.Rect(0, 0, 128, 128)))
	if _, err := DecodeQRCode(b.Bytes()); err == nil {
		t.Fatal("blank image got invented payload")
	}
	if ValidateGeneratedQR(b.Bytes(), "test").Valid {
		t.Fatal("blank image verified")
	}
}

func TestValidateSeamlessImage(t *testing.T) {
	b, err := os.ReadFile("../../../perfect_seamless.png")
	if err != nil {
		t.Skipf("perfect_seamless.png not found: %v", err)
	}

	p := model.Placement{X: 0.28, Y: 0.06, Size: 0.42}
	res := ValidateGeneratedQRWithPlacement(b, "https://lemas.io.vn", p)
	t.Logf("Valid: %v, Decoded: %q, Error: %s", res.Valid, res.DecodedPayload, res.Error)
	if !res.Valid {
		t.Fatalf("Validation failed on perfect_seamless.png: %s", res.Error)
	}
}

