package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMachGenUploadsTwoImagesAndRunsI2I(t *testing.T) {
	uploads, polls := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing MachGen bearer token")
		}
		switch r.URL.Path {
		case "/api/v0/upload":
			uploads++
			if err := r.ParseMultipartForm(2 << 20); err != nil {
				t.Fatal(err)
			}
			if _, _, err := r.FormFile("file"); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(w, `{"artifact_path":"source-%d.png"}`, uploads)
		case "/api/v0/generate":
			var body struct {
				Model       string         `json:"model"`
				TaskType    string         `json:"task_type"`
				Sources     []string       `json:"src_image_urls"`
				ImageConfig map[string]int `json:"image_config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Model != "GPT-Image-2" || body.TaskType != "I2I" {
				t.Fatalf("wrong task: %+v", body)
			}
			if len(body.Sources) != 2 || body.Sources[0] != "@input/source-1.png" || body.Sources[1] != "@input/source-2.png" {
				t.Fatalf("wrong sources: %v", body.Sources)
			}
			if body.ImageConfig["width"] != 1280 || body.ImageConfig["height"] != 1280 {
				t.Fatalf("wrong size: %v", body.ImageConfig)
			}
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"task_id":"task-1"}`)
		case "/api/v0/tasks/task-1":
			polls++
			fmt.Fprint(w, `{"status":"COMPLETED","task_output":{"image":"/api/v0/assets/task-1"}}`)
		case "/api/v0/assets/task-1":
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "generated-image")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	m := &MachGenProvider{baseURL: srv.URL, apiKey: "secret", model: "gpt-image-2", httpClient: &http.Client{Timeout: 5 * time.Second}}
	got, err := m.callMachGenImageEdit(context.Background(), []byte("guide"), []byte("qr"), "integrate QR", "gpt-image-2")
	if err != nil || string(got) != "generated-image" || uploads != 2 || polls != 1 {
		t.Fatalf("got=%q uploads=%d polls=%d err=%v", got, uploads, polls, err)
	}
}
