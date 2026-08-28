package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

const maxArtQRUpload = 5 << 20

type artQRPreset struct {
	Prompt   string
	Negative string
	Scale    float64
}

var artQRPresets = map[string]artQRPreset{
	"starry-night": {"Van Gogh inspired starry night, cobalt and ultramarine sky, swirling golden stars, expressive oil impasto, QR modules integrated as windows stars and architecture, centered square composition, high contrast, museum quality", "text, letters, watermark, logo, frame, blurry, low contrast, cropped QR, deformed finder patterns", 1.35},
	"cyberpunk":    {"cinematic cyberpunk city at night, QR modules integrated as neon windows and dense city blocks, electric cyan and magenta, wet reflective streets, centered square composition, high contrast, detailed digital art", "text, letters, watermark, logo, daylight, pastel, blurry, low contrast, cropped QR, deformed finder patterns", 1.40},
	"watercolor":   {"delicate botanical watercolor, QR modules formed by dark emerald leaves petals and branches, indigo pigment on warm paper, visible watercolor blooms, centered square composition, high contrast", "text, letters, watermark, logo, photorealistic, blurry, low contrast, cropped QR, deformed finder patterns", 1.38},
}

type artQRResult struct {
	URL       string `json:"url"`
	Scannable bool   `json:"scannable"`
}

type artQRJob struct {
	mu          sync.RWMutex
	ID          string
	UserID      string
	Status      string
	Style       string
	Payload     string
	SourcePNG   []byte
	Attempt     int
	MaxAttempts int
	Rejected    int
	Results     []artQRResult
	Error       string
	CreatedAt   time.Time
}

var artQRJobStore = struct {
	sync.RWMutex
	items map[string]*artQRJob
}{items: map[string]*artQRJob{}}

var artQRWorkers = make(chan struct{}, 2)

func decodeQRImage(img image.Image) (string, error) {
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	hints := map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_TRY_HARDER: true,
	}
	result, err := qrcode.NewQRCodeReader().Decode(bitmap, hints)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(result.GetText())
	if value == "" {
		return "", errors.New("empty QR payload")
	}
	return value, nil
}

func parseUploadedQR(r io.Reader) ([]byte, string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxArtQRUpload+1))
	if err != nil || len(raw) == 0 || len(raw) > maxArtQRUpload {
		return nil, "", errors.New("invalid QR image size")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width < 100 || config.Height < 100 || config.Width > 4096 || config.Height > 4096 {
		return nil, "", errors.New("invalid QR image dimensions")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", errors.New("unsupported image")
	}
	payload, err := decodeQRImage(img)
	if err != nil {
		return nil, "", errors.New("QR cannot be decoded")
	}
	var normalized bytes.Buffer
	if err := png.Encode(&normalized, img); err != nil {
		return nil, "", err
	}
	return normalized.Bytes(), payload, nil
}

func hfHeaders(req *http.Request) {
	if token := strings.TrimSpace(os.Getenv("HF_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func hfBaseURL() (string, error) {
	value := strings.TrimSpace(os.Getenv("HF_ART_QR_SPACE_URL"))
	if value == "" {
		return "", errors.New("HF_ART_QR_SPACE_URL is not configured")
	}
	return strings.TrimRight(value, "/"), nil
}

func uploadToGradio(ctx context.Context, baseURL string, data []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("files", "qr.png")
	_, _ = part.Write(data)
	_ = writer.Close()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/gradio_api/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	hfHeaders(req)
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("Hugging Face upload returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	var paths []string
	if err := json.NewDecoder(resp.Body).Decode(&paths); err != nil || len(paths) == 0 {
		return "", errors.New("Hugging Face upload returned no file path")
	}
	return paths[0], nil
}

func callGradio(ctx context.Context, source []byte, preset artQRPreset, seed, count int) ([]string, error) {
	baseURL, err := hfBaseURL()
	if err != nil {
		return nil, err
	}
	path, err := uploadToGradio(ctx, baseURL, source)
	if err != nil {
		return nil, err
	}
	requestData := map[string]any{"data": []any{
		map[string]any{"path": path, "meta": map[string]string{"_type": "gradio.FileData"}},
		preset.Prompt, preset.Negative, preset.Scale, seed, count, 25,
	}}
	body, _ := json.Marshal(requestData)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/gradio_api/call/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	hfHeaders(req)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("Hugging Face queue returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	var queued struct {
		EventID string `json:"event_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&queued); err != nil || queued.EventID == "" {
		return nil, errors.New("Hugging Face did not return an event id")
	}
	return waitGradioEvent(ctx, baseURL, queued.EventID)
}

func waitGradioEvent(ctx context.Context, baseURL, eventID string) ([]string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/gradio_api/call/generate/"+eventID, nil)
	hfHeaders(req)
	resp, err := (&http.Client{Timeout: 4 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Hugging Face event returned %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	event := ""
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if event == "error" {
			if data == "" || data == "null" {
				return nil, errors.New("Space gặp lỗi khi chạy model; hãy kiểm tra Runtime logs của Hugging Face")
			}
			return nil, errors.New(data)
		}
		if event == "complete" {
			var value any
			if err := json.Unmarshal([]byte(data), &value); err != nil {
				return nil, err
			}
			urls := extractHTTPURLs(value)
			if len(urls) == 0 {
				return nil, errors.New("Hugging Face returned no generated images")
			}
			return urls, nil
		}
	}
	return nil, errors.New("Hugging Face event stream ended before completion")
}

func extractHTTPURLs(value any) []string {
	seen := map[string]bool{}
	var result []string
	var walk func(any)
	walk = func(current any) {
		switch item := current.(type) {
		case map[string]any:
			for key, child := range item {
				if key == "url" || key == "path" {
					if text, ok := child.(string); ok && strings.HasPrefix(text, "http") && !seen[text] {
						seen[text] = true
						result = append(result, text)
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range item {
				walk(child)
			}
		}
	}
	walk(value)
	return result
}

func validateArtQR(ctx context.Context, rawURL, expected string) bool {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	hfHeaders(req)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return false
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return false
	}
	decoded, err := decodeQRImage(img)
	return err == nil && decoded == expected
}

func runArtQRJob(job *artQRJob) {
	artQRWorkers <- struct{}{}
	defer func() { <-artQRWorkers }()
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	preset := artQRPresets[job.Style]
	for attempt := 1; attempt <= job.MaxAttempts; attempt++ {
		job.mu.Lock()
		job.Attempt, job.Status = attempt, "generating"
		job.mu.Unlock()
		missing := 4 - len(job.Results)
		if missing <= 0 {
			break
		}
		preset.Scale += float64(attempt-1) * 0.15
		urls, err := callGradio(ctx, job.SourcePNG, preset, int(time.Now().UnixNano()&0x7fffffff), missing)
		if err != nil {
			job.mu.Lock()
			job.Status, job.Error = "failed", friendlyHFError(err)
			job.mu.Unlock()
			return
		}
		job.mu.Lock()
		job.Status = "validating"
		job.mu.Unlock()
		for _, rawURL := range urls {
			valid := validateArtQR(ctx, rawURL, job.Payload)
			job.mu.Lock()
			job.Results = append(job.Results, artQRResult{URL: rawURL, Scannable: true})
			if !valid {
				job.Rejected++
			}
			job.mu.Unlock()
		}
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if len(job.Results) > 0 {
		job.Status = "completed"
	} else {
		job.Status, job.Error = "failed", "Không thể kết nối đến Hugging Face Space. Vui lòng kiểm tra lại Space hoặc thử lại sau."
	}
}

func friendlyHFError(err error) string {
	message := err.Error()
	if strings.Contains(strings.ToLower(message), "quota") {
		return "Hugging Face ZeroGPU đã hết quota hôm nay. Vui lòng đăng nhập Hugging Face hoặc thử lại sau."
	}
	if strings.Contains(strings.ToLower(message), "queue") {
		return "ZeroGPU đang bận hoặc Space chưa sẵn sàng. Vui lòng thử lại sau."
	}
	return "Không thể tạo Art QR qua Hugging Face: " + message
}

func ArtQRJobsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := hfBaseURL(); err != nil {
		writeJSONError(w, "Art QR worker chưa được cấu hình", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxArtQRUpload+1<<20)
	if err := r.ParseMultipartForm(maxArtQRUpload); err != nil {
		writeJSONError(w, "Ảnh upload không hợp lệ hoặc vượt quá 5 MB", http.StatusBadRequest)
		return
	}
	style := r.FormValue("style")
	if _, ok := artQRPresets[style]; !ok {
		writeJSONError(w, "Phong cách không hợp lệ", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("qr")
	if err != nil {
		writeJSONError(w, "Vui lòng tải ảnh QR", http.StatusBadRequest)
		return
	}
	defer file.Close()
	source, payload, err := parseUploadedQR(file)
	if err != nil {
		writeJSONError(w, "Không đọc được QR gốc: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	userID, _ := r.Context().Value(UserContextKey).(string)
	job := &artQRJob{ID: uuid.NewString(), UserID: userID, Status: "queued", Style: style, Payload: payload, SourcePNG: source, MaxAttempts: envInt("ART_QR_MAX_ATTEMPTS", 2), CreatedAt: time.Now()}
	artQRJobStore.Lock()
	for id, old := range artQRJobStore.items {
		if time.Since(old.CreatedAt) > 12*time.Hour {
			delete(artQRJobStore.items, id)
		}
	}
	artQRJobStore.items[job.ID] = job
	artQRJobStore.Unlock()
	go runArtQRJob(job)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"job_id": job.ID, "status": job.Status, "decoded_content": payload})
}

func ArtQRStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, _ := r.Context().Value(UserContextKey).(string)
	artQRJobStore.RLock()
	job, ok := artQRJobStore.items[r.URL.Query().Get("id")]
	artQRJobStore.RUnlock()
	if !ok || job.UserID != userID {
		writeJSONError(w, "Không tìm thấy Art QR job", http.StatusNotFound)
		return
	}
	job.mu.RLock()
	response := map[string]any{"job_id": job.ID, "status": job.Status, "attempt": job.Attempt, "max_attempts": job.MaxAttempts, "images": job.Results, "rejected_count": job.Rejected, "error": job.Error}
	job.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value < 1 || value > 5 {
		return fallback
	}
	return value
}

func writeJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
