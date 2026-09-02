package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"xkiro-backend/internal/artqr"
	"xkiro-backend/internal/artqr/model"
)

var defaultArtQRService = artqr.NewService()

func jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

// ArtQRPresetsHandler returns the list of active presets
func ArtQRPresetsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Phương thức không được hỗ trợ", http.StatusMethodNotAllowed)
		return
	}

	presets := defaultArtQRService.GetPresets()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"presets": presets,
	})
}

// GenerateArtQRHandler handles POST /api/art-qr/generate
func GenerateArtQRHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Phương thức không được hỗ trợ", http.StatusMethodNotAllowed)
		return
	}

	// 10MB upload limit
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		jsonError(w, "Dữ liệu tải lên không hợp lệ: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 1. Extract QR image (Required)
	qrFile, _, err := r.FormFile("qr_image")
	if err != nil {
		jsonError(w, "Trường qr_image là bắt buộc", http.StatusBadRequest)
		return
	}
	defer qrFile.Close()

	qrBytes, err := io.ReadAll(io.LimitReader(qrFile, 10<<20))
	if err != nil || len(qrBytes) == 0 {
		jsonError(w, "Không thể đọc dữ liệu qr_image", http.StatusBadRequest)
		return
	}

	// 2. Extract Reference image (Optional)
	var refBytes []byte
	if refFile, _, err := r.FormFile("reference_image"); err == nil {
		defer refFile.Close()
		refBytes, _ = io.ReadAll(io.LimitReader(refFile, 10<<20))
	}

	// 3. Extract Preset ID (Optional)
	presetID := strings.TrimSpace(r.FormValue("preset_id"))

	// 4. Extract & Validate Placement
	placement := model.DefaultPlacement()
	if rawPlacement := r.FormValue("placement"); rawPlacement != "" {
		_ = json.Unmarshal([]byte(rawPlacement), &placement)
	} else {
		// Fallback to individual form fields
		if xStr := r.FormValue("placement_x"); xStr != "" {
			if val, err := strconv.ParseFloat(xStr, 64); err == nil {
				placement.X = val
			}
		}
		if yStr := r.FormValue("placement_y"); yStr != "" {
			if val, err := strconv.ParseFloat(yStr, 64); err == nil {
				placement.Y = val
			}
		}
		if sizeStr := r.FormValue("placement_size"); sizeStr != "" {
			if val, err := strconv.ParseFloat(sizeStr, 64); err == nil {
				placement.Size = val
			}
		}
	}

	if !placement.IsValid() {
		jsonError(w, "Tọa độ placement không hợp lệ (x, y >= 0 và x+size <= 1, y+size <= 1)", http.StatusBadRequest)
		return
	}

	// Get User ID from context if available
	userID, _ := r.Context().Value(UserContextKey).(string)

	// Create job
	job, err := defaultArtQRService.CreateJob(r.Context(), artqr.CreateJobParams{
		UserID:         userID,
		QRPNGBytes:     qrBytes,
		ReferenceBytes: refBytes,
		PresetID:       presetID,
		Placement:      placement,
	})
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jobId":    job.ID,
		"status":   job.Status,
		"progress": job.Progress,
	})
}

// GetArtQRJobHandler handles GET /api/art-qr/jobs/:id
func GetArtQRJobHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Phương thức không được hỗ trợ", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Path
	jobID := strings.TrimPrefix(path, "/api/art-qr/jobs/")
	jobID = strings.TrimPrefix(jobID, "/api/art-qr/jobs")
	jobID = strings.TrimPrefix(jobID, "/")

	if jobID == "" {
		jsonError(w, "Thiếu mã tác vụ job_id", http.StatusBadRequest)
		return
	}

	job, exists := defaultArtQRService.GetJob(jobID)
	if !exists {
		jsonError(w, "Không tìm thấy tác vụ", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(job)
}
