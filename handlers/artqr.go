package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

const (
	replicateQRVersion = "628e604e13cf63d8ec58bd4d238474e8986b054bc5e1326e50995fdbc851c557"
	maxQRUploadSize    = 5 << 20
	maxArtQRAttempts   = 3
)

type artQRStyle struct {
	Prompt            string
	NegativePrompt    string
	ConditioningScale float64
}

var artQRStyles = map[string]artQRStyle{
	"starry-night": {
		Prompt:            "Van Gogh inspired starry night landscape, luminous cobalt and ultramarine sky, swirling golden stars, expressive oil impasto brushwork, the QR structure naturally formed by windows, stars and painted architectural details, centered square composition, museum quality",
		NegativePrompt:    "text, letters, watermark, logo, frame, blurry, low contrast, deformed QR finder patterns, cropped QR code, unreadable code",
		ConditioningScale: 1.35,
	},
	"cyberpunk": {
		Prompt:            "futuristic cyberpunk city at night, QR modules integrated as neon windows and dense city blocks, electric cyan and magenta signs, wet reflective streets, cinematic atmosphere, centered square composition, highly detailed digital art",
		NegativePrompt:    "text, letters, watermark, logo, daylight, pastel, blurry, low contrast, deformed QR finder patterns, cropped QR code, unreadable code",
		ConditioningScale: 1.4,
	},
	"watercolor": {
		Prompt:            "delicate watercolor botanical garden, QR modules formed by leaves, petals and small branches, soft emerald and indigo pigments on warm paper, visible watercolor blooms, elegant handcrafted illustration, centered square composition",
		NegativePrompt:    "text, letters, watermark, logo, photorealistic, harsh shadows, blurry, low contrast, deformed QR finder patterns, cropped QR code, unreadable code",
		ConditioningScale: 1.45,
	},
}

type artQRJob struct {
	mu              sync.Mutex
	ID              string
	UserID          string
	OriginalContent string
	Style           string
	PredictionID    string
	Status          string
	Attempt         int
	ValidImages     []artQRImage
	RejectedCount   int
	Error           string
	CreatedAt       time.Time
}

type artQRImage struct {
	URL       string `json:"url"`
	Scannable bool   `json:"scannable"`
	Decoded   string `json:"decoded,omitempty"`
}

type replicatePrediction struct {
	ID     string   `json:"id"`
	Status string   `json:"status"`
	Output []string `json:"output"`
	Error  any      `json:"error"`
}

var artQRJobs = struct {
	sync.RWMutex
	items map[string]*artQRJob
}{items: make(map[string]*artQRJob)}

func decodeQRCode(img image.Image) (string, error) {
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	result, err := qrcode.NewQRCodeReader().Decode(bitmap, nil)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(result.GetText())
	if value == "" {
		return "", errors.New("empty QR payload")
	}
	return value, nil
}

func createReplicateQRPrediction(ctx context.Context, token, content string, style artQRStyle) (*replicatePrediction, error) {
	payload := map[string]any{
		"version": replicateQRVersion,
		"input": map[string]any{
			"url": content, "prompt": style.Prompt, "negative_prompt": style.NegativePrompt,
			"qr_conditioning_scale": style.ConditioningScale, "num_outputs": 4,
			"image_resolution": 768, "num_inference_steps": 30, "guidance_scale": 9,
			"scheduler": "DPMSolverMultistep", "disable_safety_check": false,
		},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.replicate.com/v1/predictions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("Replicate returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	var prediction replicatePrediction
	if err := json.NewDecoder(resp.Body).Decode(&prediction); err != nil {
		return nil, err
	}
	return &prediction, nil
}

func getReplicatePrediction(ctx context.Context, token, id string) (*replicatePrediction, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.replicate.com/v1/predictions/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Replicate status returned %d", resp.StatusCode)
	}
	var prediction replicatePrediction
	if err := json.NewDecoder(resp.Body).Decode(&prediction); err != nil {
		return nil, err
	}
	return &prediction, nil
}

func validateRemoteQR(ctx context.Context, rawURL, expected string) artQRImage {
	result := artQRImage{URL: rawURL}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return result
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return result
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > 12<<20 {
		return result
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return result
	}
	decoded, err := decodeQRCode(img)
	result.Decoded = decoded
	result.Scannable = err == nil && decoded == expected
	return result
}

// ArtQRCreateHandler decodes an uploaded QR and starts a QR-ControlNet generation job.
func ArtQRCreateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimSpace(os.Getenv("REPLICATE_API_TOKEN"))
	if token == "" {
		writeJSONError(w, "Art QR chưa được cấu hình REPLICATE_API_TOKEN", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxQRUploadSize+1<<20)
	if err := r.ParseMultipartForm(maxQRUploadSize); err != nil {
		writeJSONError(w, "Ảnh QR vượt quá 5 MB hoặc dữ liệu upload không hợp lệ", http.StatusBadRequest)
		return
	}
	styleID := r.FormValue("style")
	style, ok := artQRStyles[styleID]
	if !ok {
		writeJSONError(w, "Phong cách Art QR không hợp lệ", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("qr")
	if err != nil {
		writeJSONError(w, "Vui lòng tải lên ảnh QR", http.StatusBadRequest)
		return
	}
	defer file.Close()
	img, _, err := image.Decode(io.LimitReader(file, maxQRUploadSize))
	if err != nil {
		writeJSONError(w, "Không đọc được ảnh QR. Hỗ trợ PNG, JPG và GIF", http.StatusBadRequest)
		return
	}
	content, err := decodeQRCode(img)
	if err != nil {
		writeJSONError(w, "Không tìm thấy QR có thể quét trong ảnh", http.StatusUnprocessableEntity)
		return
	}
	prediction, err := createReplicateQRPrediction(r.Context(), token, content, style)
	if err != nil {
		writeJSONError(w, "Không thể khởi tạo GPU worker: "+err.Error(), http.StatusBadGateway)
		return
	}
	userID, _ := r.Context().Value(UserContextKey).(string)
	job := &artQRJob{ID: uuid.NewString(), UserID: userID, OriginalContent: content, Style: styleID, PredictionID: prediction.ID, Status: prediction.Status, Attempt: 1, CreatedAt: time.Now()}
	// Even a fast prediction must pass through our validator before it is terminal.
	if job.Status == "succeeded" {
		job.Status = "processing"
	}
	artQRJobs.Lock()
	for id, existing := range artQRJobs.items {
		if time.Since(existing.CreatedAt) > 6*time.Hour {
			delete(artQRJobs.items, id)
		}
	}
	artQRJobs.items[job.ID] = job
	artQRJobs.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"job_id": job.ID, "status": job.Status, "decoded_content": content, "attempt": job.Attempt, "max_attempts": maxArtQRAttempts})
}

// ArtQRStatusHandler polls Replicate, validates every result, and retries failed scans automatically.
func ArtQRStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	userID, _ := r.Context().Value(UserContextKey).(string)
	jobID := strings.TrimSpace(r.URL.Query().Get("id"))
	artQRJobs.RLock()
	job, ok := artQRJobs.items[jobID]
	artQRJobs.RUnlock()
	if !ok || job.UserID != userID {
		writeJSONError(w, "Không tìm thấy Art QR job", http.StatusNotFound)
		return
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.Status != "succeeded" && job.Status != "failed" {
		token := strings.TrimSpace(os.Getenv("REPLICATE_API_TOKEN"))
		prediction, err := getReplicatePrediction(r.Context(), token, job.PredictionID)
		if err != nil {
			writeJSONError(w, "Không thể đọc trạng thái GPU worker", http.StatusBadGateway)
			return
		}
		job.Status = prediction.Status
		if prediction.Status == "failed" || prediction.Status == "canceled" {
			job.Status, job.Error = "failed", fmt.Sprint(prediction.Error)
		} else if prediction.Status == "succeeded" {
			for _, output := range prediction.Output {
				candidate := validateRemoteQR(r.Context(), output, job.OriginalContent)
				if candidate.Scannable {
					job.ValidImages = append(job.ValidImages, candidate)
				} else {
					job.RejectedCount++
				}
			}
			if len(job.ValidImages) == 0 && job.Attempt < maxArtQRAttempts {
				style := artQRStyles[job.Style]
				style.ConditioningScale += float64(job.Attempt) * 0.12
				next, retryErr := createReplicateQRPrediction(r.Context(), token, job.OriginalContent, style)
				if retryErr == nil {
					job.PredictionID, job.Status = next.ID, next.Status
					job.Attempt++
				} else {
					job.Status, job.Error = "failed", retryErr.Error()
				}
			} else if len(job.ValidImages) == 0 {
				job.Status, job.Error = "failed", "AI đã tạo ảnh nhưng không có mẫu nào giải mã đúng QR gốc"
			}
		}
	}
	artQRJobs.Lock()
	artQRJobs.items[jobID] = job
	artQRJobs.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"job_id": job.ID, "status": job.Status, "attempt": job.Attempt, "max_attempts": maxArtQRAttempts, "images": job.ValidImages, "rejected_count": job.RejectedCount, "error": job.Error})
}

func writeJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
