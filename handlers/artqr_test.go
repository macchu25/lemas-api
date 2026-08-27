package handlers

import (
	"os"
	"reflect"
	"testing"
)

func TestExtractHTTPURLsFromGradioGallery(t *testing.T) {
	payload := []any{
		[]any{
			map[string]any{"image": map[string]any{"url": "https://worker.hf.space/file=a.png", "path": "/tmp/a.png"}},
			map[string]any{"image": map[string]any{"url": "https://worker.hf.space/file=b.png"}},
			map[string]any{"url": "https://worker.hf.space/file=a.png"},
		},
	}
	want := []string{"https://worker.hf.space/file=a.png", "https://worker.hf.space/file=b.png"}
	if got := extractHTTPURLs(payload); !reflect.DeepEqual(got, want) {
		t.Fatalf("extractHTTPURLs() = %#v, want %#v", got, want)
	}
}

func TestEnvIntBounds(t *testing.T) {
	const key = "TEST_ART_QR_ATTEMPTS"
	t.Cleanup(func() { _ = os.Unsetenv(key) })
	_ = os.Setenv(key, "3")
	if got := envInt(key, 2); got != 3 {
		t.Fatalf("envInt valid value = %d, want 3", got)
	}
	_ = os.Setenv(key, "20")
	if got := envInt(key, 2); got != 2 {
		t.Fatalf("envInt out-of-range value = %d, want fallback 2", got)
	}
}
