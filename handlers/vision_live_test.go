package handlers

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"

	"xkiro-backend/db"
	"xkiro-backend/internal/artqr/model"
	"xkiro-backend/internal/artqr/vision"
	"xkiro-backend/services"
)

func TestVisionLive(t *testing.T) {
	if f, err := os.Open("../.env"); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				os.Setenv(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}
		f.Close()
	}

	sampleImg, err := os.ReadFile("c:/Users/NHU HUU/Downloads/nornAI/client/public/presets/doraemon_bread_scene.jpg")
	if err != nil {
		t.Skipf("ReadFile error: %v", err)
	}

	analyzer := vision.NewXKiroVisionAnalyzer()
	res, err := analyzer.AnalyzeStyle(context.Background(), sampleImg, model.Placement{X: 0.25, Y: 0.25, Size: 0.5})
	if err != nil {
		t.Fatalf("AnalyzeStyle failed: %v", err)
	}

	t.Logf("Success! Target Surface: %q, Style: %q, Scene: %q, OptimalPlacement: %+v",
		res.TargetSurface, res.Style, res.SceneDescription, res.OptimalPlacement)
}
