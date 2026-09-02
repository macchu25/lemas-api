package provider

import (
	"context"

	"xkiro-backend/internal/artqr/model"
)

type GenerationRequest struct {
	Payload             string
	Prompt              string
	NegativePrompt      string
	QRControlImagePNG   []byte
	ReferenceImageBytes []byte
	Placement           model.Placement
	ConditioningScale   float64
	ReferenceStrength   float64
	GuidanceScale       float64
	Seed                int
	Width               int
	Height              int
	NumOutputs          int
}

type GeneratedImage struct {
	URL      string
	PNGBytes []byte
	Seed     int
}

type ArtQRProvider interface {
	Name() string
	Generate(ctx context.Context, req *GenerationRequest) ([]GeneratedImage, error)
}
