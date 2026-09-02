package model

import (
	"time"
)

type Placement struct {
	X    float64 `json:"x" bson:"x"`
	Y    float64 `json:"y" bson:"y"`
	Size float64 `json:"size" bson:"size"`
}

func (p Placement) IsValid() bool {
	if p.Size <= 0 || p.Size > 1.0 {
		return false
	}
	if p.X < 0 || p.Y < 0 {
		return false
	}
	if p.X+p.Size > 1.0001 || p.Y+p.Size > 1.0001 {
		return false
	}
	return true
}

func DefaultPlacement() Placement {
	return Placement{
		X:    0.25,
		Y:    0.25,
		Size: 0.50,
	}
}

type ArtQRPreset struct {
	ID                string    `json:"id" bson:"_id"`
	Slug              string    `json:"slug" bson:"slug"`
	Name              string    `json:"name" bson:"name"`
	Description       string    `json:"description" bson:"description"`
	PreviewURL        string    `json:"preview_url" bson:"preview_url"`
	Colors            []string  `json:"colors" bson:"colors"`
	Prompt            string    `json:"prompt" bson:"prompt"`
	NegativePrompt    string    `json:"negative_prompt" bson:"negative_prompt"`
	ConditioningScale float64   `json:"conditioning_scale" bson:"conditioning_scale"`
	GuidanceScale     float64   `json:"guidance_scale" bson:"guidance_scale"`
	Width             int       `json:"width" bson:"width"`
	Height            int       `json:"height" bson:"height"`
	Enabled           bool      `json:"enabled" bson:"enabled"`
	CreatedAt         time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" bson:"updated_at"`
}
