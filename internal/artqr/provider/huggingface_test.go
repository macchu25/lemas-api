package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPayloadGenerateContract(t *testing.T) {
	var host string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing auth")
		}
		switch r.URL.Path {
		case "/gradio_api/call/generate":
			calls++
			var body struct {
				Data []any `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.Data) != 7 || body.Data[0] != " payload " || body.Data[4] != float64(123) || body.Data[5] != float64(1) || body.Data[6] != float64(25) {
				t.Errorf("wrong contract: %#v", body.Data)
			}
			fmt.Fprint(w, `{"event_id":"abc"}`)
		case "/gradio_api/call/generate/abc":
			fmt.Fprintf(w, "event: complete\ndata: [[{\"image\":{\"url\":%q}}]]\n\n", host+"/image.png")
		case "/image.png":
			w.Write([]byte("fixture-bytes"))
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	host = srv.URL
	h := &HuggingFaceProvider{BaseURL: host, Token: "test-token"}
	images, err := h.Generate(context.Background(), &GenerationRequest{Payload: " payload ", Seed: 123, NumOutputs: 1})
	if err != nil || len(images) != 1 || calls != 1 {
		t.Fatalf("Generate: %#v %v", images, err)
	}
}

func TestProviderFailsClosed(t *testing.T) {
	h := &HuggingFaceProvider{}
	if _, err := h.Generate(context.Background(), &GenerationRequest{Payload: "test"}); err == nil {
		t.Fatal("missing Space accepted")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "busy", 429) }))
	defer srv.Close()
	h.BaseURL = srv.URL
	if _, err := h.Generate(context.Background(), &GenerationRequest{Payload: "test"}); err == nil {
		t.Fatal("provider error hidden")
	}
	if _, err := h.downloadImage(context.Background(), "https://example.com/image.png"); err == nil {
		t.Fatal("external download accepted")
	}
}
