package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// TTSHandler streams high-quality Google Voice audio (MP3) with sub-second latency
func TTSHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	text := r.URL.Query().Get("text")
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "vi"
	}
	if lang == "en" {
		lang = "en"
	} else if lang == "zh" {
		lang = "zh-CN"
	} else {
		lang = "vi"
	}

	text = strings.TrimSpace(text)
	if text == "" {
		http.Error(w, "Text is required", http.StatusBadRequest)
		return
	}

	// Limit single chunk length to 200 chars for optimal voice stream
	if len(text) > 300 {
		text = text[:300]
	}

	ttsURL := fmt.Sprintf("https://translate.google.com/translate_tts?ie=UTF-8&tl=%s&client=tw-ob&q=%s",
		url.QueryEscape(lang),
		url.QueryEscape(text),
	)

	req, err := http.NewRequest(http.MethodGet, ttsURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://translate.google.com/")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}
