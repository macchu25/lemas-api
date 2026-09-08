package model

import (
	"xkiro-backend/models"
)

type Placement = models.ArtQRPlacement
type ArtQRPreset = models.ArtQRPreset

func DefaultPlacement() Placement {
	return models.DefaultPlacement()
}

