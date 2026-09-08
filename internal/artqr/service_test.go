package artqr

import (
	"context"
	"testing"

	"time"

	qrcode "github.com/skip2/go-qrcode"
	"xkiro-backend/internal/artqr/model"
	"xkiro-backend/internal/artqr/provider"
	"xkiro-backend/internal/artqr/qr"
)

type invalidProvider struct{}

func (invalidProvider) Name() string { return "test" }
func (invalidProvider) Generate(_ context.Context, req *provider.GenerationRequest) ([]provider.GeneratedImage, error) {
	return []provider.GeneratedImage{{URL: "https://example.com/not-a-qr.png", PNGBytes: []byte("invalid image")}}, nil
}

func TestInvalidCandidateNeverBecomesVerifiedOutput(t *testing.T) {
	s := NewService()
	s.provider = invalidProvider{}
	job := &model.ArtQRJob{OriginalPayload: "test-payload", MaxAttempts: 1, Placement: model.DefaultPlacement()}
	preset := s.presets["bread_toast"]
	mask, _ := qr.BuildBinaryQRMask([]byte("dummy"), 1024, 1024, model.DefaultPlacement(), 4)
	s.processJob(job, mask, preset)
	snap := job.Snapshot()
	if len(snap.Images) != 0 || snap.Status != "failed" {
		t.Fatalf("invalid candidate accepted: %+v", snap)
	}
}

func TestArtQRPipelineWithMandatoryBackgroundRemovalAndDeterministicRestoration(t *testing.T) {
	expectedPayload := "https://lemas.io.vn/art-qr-verified"
	qrObj, err := qrcode.New(expectedPayload, qrcode.Highest)
	if err != nil {
		t.Fatalf("failed to create test QR: %v", err)
	}
	rawPNG, err := qrObj.PNG(512)
	if err != nil {
		t.Fatalf("failed to encode test QR: %v", err)
	}

	s := NewService()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	params := CreateJobParams{
		UserID:     "unit-tester",
		QRPNGBytes: rawPNG,
		PresetID:   "bread_toast",
	}

	// 1. Test CreateJob mandatory background removal execution
	job, err := s.CreateJob(ctx, params)
	if err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	if job.OriginalPayload != expectedPayload {
		t.Errorf("expected original payload %q, got %q", expectedPayload, job.OriginalPayload)
	}

	// Mandatory requirement: Background removal must be invoked automatically
	if !job.BackgroundRemoved {
		t.Errorf("mandatory existing QR background removal was not marked as succeeded")
	}

	if len(job.CleanedQRPNG) == 0 {
		t.Fatalf("cleaned QR PNG is empty")
	}

	// 2. Test synchronous execution and final verification contract
	result, err := s.GenerateArtQR(ctx, params)
	if err != nil {
		t.Fatalf("GenerateArtQR failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("expected success=true, got error: %s", result.Error)
	}

	if !result.QRValid {
		t.Errorf("expected qr_valid=true")
	}

	if result.ExpectedPayload != expectedPayload {
		t.Errorf("expected payload mismatch: expected %s, got %s", expectedPayload, result.ExpectedPayload)
	}

	if result.DecodedPayload != expectedPayload {
		t.Errorf("decoded payload mismatch: expected %s, got %s", expectedPayload, result.DecodedPayload)
	}

	if !result.BackgroundRemoved {
		t.Errorf("expected background_removed=true in result")
	}

	if result.Preset != "bread_toast" {
		t.Errorf("expected preset bread_toast, got %s", result.Preset)
	}
}


