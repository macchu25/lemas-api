package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

	// Check if synchronous execution is requested
	isSync := r.URL.Query().Get("sync") == "true" || r.FormValue("sync") == "true"

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

	// 3. Extract Preset ID & Custom Prompt (Optional)
	presetID := strings.TrimSpace(r.FormValue("preset_id"))
	if presetID == "" {
		presetID = "bread_toast"
	}
	customPrompt := strings.TrimSpace(r.FormValue("custom_prompt"))

	// 4. Extract & Validate Placement
	hasCustomPlacement := false
	placement := model.DefaultPlacement()
	if rawPlacement := r.FormValue("placement"); rawPlacement != "" {
		if err := json.Unmarshal([]byte(rawPlacement), &placement); err == nil && placement.IsValid() {
			hasCustomPlacement = true
		}
	} else {
		// Fallback to individual form fields
		if xStr := r.FormValue("placement_x"); xStr != "" {
			if val, err := strconv.ParseFloat(xStr, 64); err == nil {
				placement.X = val
				hasCustomPlacement = true
			}
		}
		if yStr := r.FormValue("placement_y"); yStr != "" {
			if val, err := strconv.ParseFloat(yStr, 64); err == nil {
				placement.Y = val
				hasCustomPlacement = true
			}
		}
		if sizeStr := r.FormValue("placement_size"); sizeStr != "" {
			if val, err := strconv.ParseFloat(sizeStr, 64); err == nil {
				placement.Size = val
				hasCustomPlacement = true
			}
		}
	}

	// If no explicit placement was supplied, and a preset with custom placement exists, use preset's placement
	if !hasCustomPlacement && presetID != "" {
		if p, ok := defaultArtQRService.GetPreset(presetID); ok && p != nil && p.Placement != nil && p.Placement.IsValid() {
			placement = *p.Placement
		}
	}

	if !placement.IsValid() {
		jsonError(w, "Tọa độ placement không hợp lệ (x, y >= 0 và x+size <= 1, y+size <= 1)", http.StatusBadRequest)
		return
	}

	// Get User ID from context if available
	userID, _ := r.Context().Value(UserContextKey).(string)

	params := artqr.CreateJobParams{
		UserID:         userID,
		QRPNGBytes:     qrBytes,
		ReferenceBytes: refBytes,
		PresetID:       presetID,
		CustomPrompt:   customPrompt,
		Placement:      placement,
	}

	// Handle synchronous generation request
	if isSync {
		result, err := defaultArtQRService.GenerateArtQR(r.Context(), params)
		w.Header().Set("Content-Type", "application/json")
		if err != nil || (result != nil && !result.Success) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			if result != nil {
				_ = json.NewEncoder(w).Encode(result)
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success":  false,
					"qr_valid": false,
					"error":    err.Error(),
				})
			}
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result)
		return
	}

	// Handle async job queueing
	job, err := defaultArtQRService.CreateJob(r.Context(), params)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jobId":              job.ID,
		"status":             job.Status,
		"progress":           job.Progress,
		"expected_payload":   job.OriginalPayload,
		"preset_id":          job.PresetID,
		"background_removed": job.BackgroundRemoved,
		"fallback_mode":      job.FallbackMode,
	})
}


// AnalyzeStyleHandler handles POST /api/art-qr/analyze-style
func AnalyzeStyleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Phương thức không được hỗ trợ", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		jsonError(w, "Dữ liệu tải lên không hợp lệ", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("reference_image")
	if err != nil {
		jsonError(w, "Trường reference_image là bắt buộc", http.StatusBadRequest)
		return
	}
	defer file.Close()

	refBytes, err := io.ReadAll(io.LimitReader(file, 10<<20))
	if err != nil || len(refBytes) == 0 {
		jsonError(w, "Không thể đọc ảnh tham chiếu", http.StatusBadRequest)
		return
	}

	placement := model.DefaultPlacement()
	if rawPlacement := r.FormValue("placement"); rawPlacement != "" {
		_ = json.Unmarshal([]byte(rawPlacement), &placement)
	}

	result, err := defaultArtQRService.AnalyzeStyle(r.Context(), refBytes, placement)
	if err != nil || result == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"style":    "Chân dung nghệ thuật / Tác phẩm gốc",
			"palette":  []string{"#8b0000", "#ffd700", "#1a202c", "#f5d0a9"},
			"lighting": "Ánh sáng studio cinematic",
			"texture":  "Vân vải & chi tiết tự nhiên",
			"prompt":   "Masterpiece portrait preserving the exact subject, clothing, and background of the image with ornate golden bullion embroidery cords and brass medals",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"style":                result.Style,
		"palette":              result.Palette,
		"subject_details":      result.SubjectDetails,
		"composition":          result.Composition,
		"lighting":             result.Lighting,
		"texture":              result.Texture,
		"contrast":             result.Contrast,
		"qr_region_analysis":   result.QRRegionAnalysis,
		"integration_strategy": result.IntegrationStrategy,
		"patch_prompt":         result.PatchPrompt,
		"prompt":               result.GeneratedPrompt,
		"raw_json":             result.RawJSON,
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
	_ = json.NewEncoder(w).Encode(job.Snapshot())
}

// ArtQRPresetItemHandler handles GET /api/art-qr/presets/:id
func ArtQRPresetItemHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Phương thức không được hỗ trợ", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/art-qr/presets/")
	id = strings.TrimPrefix(id, "/")
	if id == "" {
		ArtQRPresetsHandler(w, r)
		return
	}
	preset, ok := defaultArtQRService.GetPreset(id)
	if !ok {
		jsonError(w, "Không tìm thấy phong cách", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(preset)
}

// AdminArtQRPresetsHandler handles full admin CRUD for Art QR presets
// POST /api/art-qr/admin/presets (Create/Update)
// DELETE /api/art-qr/admin/presets/:id (Delete)
func AdminArtQRPresetsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		presets := defaultArtQRService.GetPresets()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"presets": presets,
		})
		return
	}

	if r.Method == http.MethodDelete {
		path := r.URL.Path
		id := strings.TrimPrefix(path, "/api/art-qr/admin/presets/")
		id = strings.TrimPrefix(id, "/")
		if id == "" {
			id = r.URL.Query().Get("id")
		}
		if id == "" {
			jsonError(w, "Thiếu ID phong cách cần xóa", http.StatusBadRequest)
			return
		}
		if err := defaultArtQRService.DeletePreset(id); err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"message": "Đã xóa phong cách thành công",
		})
		return
	}

	if r.Method == http.MethodPost {
		var preset model.ArtQRPreset
		if err := json.NewDecoder(r.Body).Decode(&preset); err != nil {
			jsonError(w, "Dữ liệu JSON không hợp lệ: "+err.Error(), http.StatusBadRequest)
			return
		}

		if strings.TrimSpace(preset.Name) == "" {
			jsonError(w, "Tên phong cách không được để trống", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(preset.ID) == "" {
			preset.ID = strings.ToLower(strings.ReplaceAll(preset.Name, " ", "_"))
		}

		if err := defaultArtQRService.CreateOrUpdatePreset(preset); err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"message": "Đã lưu phong cách thành công",
			"preset":  preset,
		})
		return
	}

	jsonError(w, "Phương thức không được hỗ trợ", http.StatusMethodNotAllowed)
}

// AdminUploadSceneHandler handles uploading a reference scene image for Art QR presets
// POST /api/art-qr/admin/upload-scene
func AdminUploadSceneHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Phương thức không được hỗ trợ", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(15 << 20); err != nil {
		jsonError(w, "Ảnh tải lên vượt quá dung lượng cho phép (15MB)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("scene_image")
	if err != nil {
		jsonError(w, "Trường scene_image là bắt buộc", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 15<<20))
	if err != nil || len(data) == 0 {
		jsonError(w, "Không thể đọc dữ liệu ảnh", http.StatusBadRequest)
		return
	}

	// Determine file extension
	ext := ".jpg"
	origName := strings.ToLower(header.Filename)
	if strings.HasSuffix(origName, ".png") {
		ext = ".png"
	} else if strings.HasSuffix(origName, ".webp") {
		ext = ".webp"
	}

	fileName := fmt.Sprintf("scene_%d%s", time.Now().UnixNano(), ext)

	// Save to server assets directory
	_ = os.MkdirAll("assets", 0755)
	serverPath := filepath.Join("assets", fileName)
	if err := os.WriteFile(serverPath, data, 0644); err != nil {
		jsonError(w, "Lỗi lưu ảnh vào server assets: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Also copy to client public/presets directory if accessible
	clientPresetsDir := filepath.Join("..", "client", "public", "presets")
	if _, statErr := os.Stat(clientPresetsDir); statErr == nil {
		_ = os.WriteFile(filepath.Join(clientPresetsDir, fileName), data, 0644)
	}

	publicURL := "/presets/" + fileName

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  true,
		"url":      publicURL,
		"filename": fileName,
		"size":     len(data),
	})
}

