package compositor

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"xkiro-backend/internal/artqr/model"
	"xkiro-backend/internal/artqr/qr"
)

func TestSafetyNetProducesScannerValidDenseQRFromDarkScene(t *testing.T) {
	payload := "00020101021138560010A0000007270126000697041501121058868743300208QRIBFTTA53037045802VN63047B1E"
	placement := model.Placement{X: 0.39, Y: 0.26, Size: 0.32}
	mask, err := qr.BuildBinaryQRMaskFromPayload(payload, 1024, 1024, placement, 4)
	if err != nil {
		t.Fatal(err)
	}
	base := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	for y := 0; y < 1024; y++ {
		for x := 0; x < 1024; x++ {
			base.Set(x, y, color.RGBA{R: 5, G: 5, B: 5, A: 255})
		}
	}
	var basePNG bytes.Buffer
	if err := png.Encode(&basePNG, base); err != nil {
		t.Fatal(err)
	}
	result, err := RestoreAndComposite(basePNG.Bytes(), nil, mask,
		model.ArtQRPreset{DarkColor: "#3a1a0a"},
		SafetyConfig{TextureStrength: 0, MaxDarkLuminance: 30, MinLightLuminance: 235,
			SolidifyFinderPattern: true, CleanQuietZone: true})
	if err != nil {
		t.Fatal(err)
	}
	validation := qr.ValidateGeneratedQRWithPlacement(result, payload, placement)
	if !validation.Valid || validation.DecodedPayload != payload {
		t.Fatalf("safety-net output was not scanner-valid: %#v", validation)
	}
}
