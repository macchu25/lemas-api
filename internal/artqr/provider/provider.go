package provider

import (
	"context"
)

type GenerationRequest struct {
	Payload           string
	Prompt            string
	NegativePrompt    string
	QRControlImagePNG []byte
	ConditioningScale float64
	GuidanceScale     float64
	Seed              int
	Width             int
	Height            int
	NumOutputs        int
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
