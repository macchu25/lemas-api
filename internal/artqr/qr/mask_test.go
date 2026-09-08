package qr

import (
	"bytes"
	"image/png"
	"testing"

	"xkiro-backend/internal/artqr/model"
)

func TestBuildBinaryMaskFromDensePayloadIsDecodable(t *testing.T) {
	payload := "00020101021138560010A0000007270126000697041501121058868743300208QRIBFTTA53037045802VN63047B1E"
	p := model.Placement{X: 0.39, Y: 0.26, Size: 0.32}
	mask, err := BuildBinaryQRMaskFromPayload(payload, 1024, 1024, p, 4)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, mask.ToDebugMaskImage()); err != nil {
		t.Fatal(err)
	}
	got := ValidateGeneratedQRWithPlacement(out.Bytes(), payload, p)
	if !got.Valid || got.DecodedPayload != payload {
		t.Fatalf("regenerated dense QR did not validate: %#v", got)
	}
}
