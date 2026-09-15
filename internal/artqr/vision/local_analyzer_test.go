package vision

import (
	"os"
	"testing"
	"xkiro-backend/internal/artqr/model"
)

func TestLocalAnalyzer(t *testing.T) {
	imgPath := "c:/Users/NHU HUU/Downloads/nornAI/client/public/presets/doraemon_bread_scene.jpg"
	data, err := os.ReadFile(imgPath)
	if err != nil {
		t.Skipf("Preset image not found: %v", err)
	}

	result := AnalyzeImageLocally(data, model.DefaultPlacement())
	if result == nil {
		t.Fatal("Expected non-nil result from AnalyzeImageLocally")
	}

	t.Logf("Local Analyzer Result:")
	t.Logf("Style: %s", result.Style)
	t.Logf("Target Surface: %s", result.TargetSurface)
	t.Logf("Palette: %v", result.Palette)
	t.Logf("Lighting: %s", result.Lighting)
	t.Logf("Contrast: %s", result.Contrast)
	if result.OptimalPlacement != nil {
		t.Logf("Optimal Placement: X=%.2f, Y=%.2f, Size=%.2f",
			result.OptimalPlacement.X, result.OptimalPlacement.Y, result.OptimalPlacement.Size)
	}
}
