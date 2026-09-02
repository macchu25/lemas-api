package artqr

import (
	"context"
	"testing"

	"xkiro-backend/internal/artqr/model"
	"xkiro-backend/internal/artqr/provider"
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
	s.processJob(job)
	snap := job.Snapshot()
	if len(snap.Images) != 0 || snap.Status != "failed" {
		t.Fatalf("invalid candidate accepted: %+v", snap)
	}
}
