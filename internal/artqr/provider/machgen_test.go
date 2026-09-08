package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestXKiroUsesMultipartEditAndPollsResult(t *testing.T) {
	var host string
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/edits":
			if r.Method != http.MethodPost {
				t.Errorf("method=%s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer secret" {
				t.Error("missing auth")
			}
			if err := r.ParseMultipartForm(2 << 20); err != nil {
				t.Fatal(err)
			}
			file, _, err := r.FormFile("image")
			if err != nil {
				t.Fatal(err)
			}
			got, _ := io.ReadAll(file)
			_ = file.Close()
			if string(got) != "guide-image" {
				t.Errorf("source image=%q", got)
			}
			if r.FormValue("prompt") != "integrate QR" || r.FormValue("model") != "gpt-image" {
				t.Errorf("form=%v", r.MultipartForm.Value)
			}
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"id":"job-1","status":"processing"}`)
		case "/v1/images/generations/job-1":
			polls++
			fmt.Fprintf(w, `{"status":"succeeded","data":[{"url":%q}]}`, host+"/result.png")
		case "/result.png":
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "result-image")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	host = srv.URL
	m := &MachGenProvider{baseURL: host + "/v1", apiKey: "secret", model: "gpt-image-2", httpClient: &http.Client{Timeout: 5 * time.Second}}
	// Call the branch directly; GenerateWithTwoReferences dispatch is covered separately.
	got, err := m.callXKiroImageEdit(context.Background(), []byte("guide-image"), "integrate QR", "gpt-image-2", 1024, 1024)
	if err != nil || string(got) != "result-image" || polls != 1 {
		t.Fatalf("got=%q polls=%d err=%v", got, polls, err)
	}
}
