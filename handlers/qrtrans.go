package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"strings"

	"xkiro-backend/internal/artqr/qr"
	"xkiro-backend/internal/qrtrans"

	qrcode "github.com/skip2/go-qrcode"
	_ "golang.org/x/image/webp"
)

const maxQRTransUploadSize = 10 << 20 // 10MB

type BoundingBox struct {
	MinX int `json:"minX"`
	MinY int `json:"minY"`
	MaxX int `json:"maxX"`
	MaxY int `json:"maxY"`
}

type QRTransResponse struct {
	Success         bool         `json:"success"`
	Error           string       `json:"error,omitempty"`
	Width           int          `json:"width,omitempty"`
	Height          int          `json:"height,omitempty"`
	QRValid         bool         `json:"qrValid"`
	InputPayload    string       `json:"inputPayload,omitempty"`
	OutputPayload   string       `json:"outputPayload,omitempty"`
	ThresholdUsed   uint8        `json:"thresholdUsed,omitempty"`
	Retries         int          `json:"retries,omitempty"`
	ExecutionTimeMs float64      `json:"executionTimeMs,omitempty"`
	DataURL         string       `json:"dataUrl,omitempty"`
	QRBounds        *BoundingBox `json:"qrBounds,omitempty"`
}

type QRTransJSONRequest struct {
	ImageBase64 string `json:"image_base64"`
	Threshold   int    `json:"threshold"`
	CropMode    string `json:"crop_mode"`
	Validate    *bool  `json:"validate"`
}

// QRRemoveBackgroundHandler processes QR images to remove backgrounds, isolate modules, and make transparent.
// Requires Admin authentication.
func QRRemoveBackgroundHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"success":false,"error":"Method not allowed. Use POST."}`, http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var (
		fileBytes []byte
		thresh    int
		cropMode  string
		validate  = true
	)

	contentType := r.Header.Get("Content-Type")

	if strings.Contains(contentType, "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxQRTransUploadSize)
		if err := r.ParseMultipartForm(maxQRTransUploadSize); err != nil {
			sendJSONError(w, http.StatusBadRequest, fmt.Sprintf("Kích thước file vượt quá giới hạn cho phép (%d MB)", maxQRTransUploadSize>>20))
			return
		}

		file, _, err := r.FormFile("image")
		if err != nil {
			sendJSONError(w, http.StatusBadRequest, "Không tìm thấy trường 'image' trong yêu cầu tải lên")
			return
		}
		defer file.Close()

		fileBytes, err = io.ReadAll(file)
		if err != nil || len(fileBytes) == 0 {
			sendJSONError(w, http.StatusBadRequest, "Ảnh tải lên bị rỗng hoặc không đọc được")
			return
		}

		if tStr := r.FormValue("threshold"); tStr != "" {
			if v, err := strconv.Atoi(tStr); err == nil {
				thresh = v
			}
		}
		cropMode = r.FormValue("crop_mode")
		if vStr := r.FormValue("validate"); vStr != "" {
			if v, err := strconv.ParseBool(vStr); err == nil {
				validate = v
			}
		}
	} else if strings.Contains(contentType, "application/json") {
		var req QRTransJSONRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendJSONError(w, http.StatusBadRequest, "Dữ liệu JSON không hợp lệ")
			return
		}

		b64 := req.ImageBase64
		if idx := strings.Index(b64, ","); idx != -1 {
			b64 = b64[idx+1:]
		}
		var err error
		fileBytes, err = base64.StdEncoding.DecodeString(b64)
		if err != nil || len(fileBytes) == 0 {
			sendJSONError(w, http.StatusBadRequest, "Dữ liệu base64 ảnh không hợp lệ")
			return
		}

		thresh = req.Threshold
		cropMode = req.CropMode
		if req.Validate != nil {
			validate = *req.Validate
		}
	} else {
		sendJSONError(w, http.StatusBadRequest, "Yêu cầu phải ở định dạng multipart/form-data hoặc application/json")
		return
	}

	// Decode source image
	img, format, err := image.Decode(bytes.NewReader(fileBytes))
	if err != nil {
		sendJSONError(w, http.StatusBadRequest, fmt.Sprintf("Không thể giải mã định dạng ảnh (%s): %v", format, err))
		return
	}

	opts := &qrtrans.Options{
		Threshold:                    qrtrans.DefaultThreshold,
		ValidateQR:                   validate,
		FallbackOnValidationFailure: true,
		CropMode:                     qrtrans.CropModeNone,
	}

	if thresh > 0 && thresh <= 255 {
		opts.Threshold = uint8(thresh)
	}

	switch strings.ToLower(strings.TrimSpace(cropMode)) {
	case "crop":
		opts.CropMode = qrtrans.CropModeCrop
	case "mask":
		opts.CropMode = qrtrans.CropModeMask
	default:
		opts.CropMode = qrtrans.CropModeNone
	}

	result, err := qrtrans.ProcessImage(img, opts)
	if err != nil {
		sendJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Lỗi xử lý tách nền QR: %v", err))
		return
	}

	b64Data := base64.StdEncoding.EncodeToString(result.PNGData)
	resp := QRTransResponse{
		Success:         true,
		Width:           result.Width,
		Height:          result.Height,
		QRValid:         result.QRValid,
		InputPayload:    result.InputPayload,
		OutputPayload:   result.OutputPayload,
		ThresholdUsed:   result.ThresholdUsed,
		Retries:         result.Retries,
		ExecutionTimeMs: float64(result.Duration.Microseconds()) / 1000.0,
		DataURL:         "data:image/png;base64," + b64Data,
	}
	if result.QRBounds != nil {
		resp.QRBounds = &BoundingBox{
			MinX: result.QRBounds.Min.X,
			MinY: result.QRBounds.Min.Y,
			MaxX: result.QRBounds.Max.X,
			MaxY: result.QRBounds.Max.Y,
		}
	}

	if resp.OutputPayload == "" && len(fileBytes) > 0 {
		if decoded, decErr := qr.DecodeQRCode(fileBytes); decErr == nil && decoded != nil && decoded.Payload != "" {
			resp.OutputPayload = decoded.Payload
			resp.QRValid = true
		}
	}

	_ = json.NewEncoder(w).Encode(resp)
}

// SampleQRGenerateHandler generates a clean test QR for instant testing in the UI
func SampleQRGenerateHandler(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if text == "" {
		text = "https://nornai.com/art-qr"
	}
	size := 300
	if sVal, err := strconv.Atoi(r.URL.Query().Get("size")); err == nil && sVal >= 100 && sVal <= 1000 {
		size = sVal
	}

	pngBytes, err := qrcode.Encode(text, qrcode.Medium, size)
	if err != nil {
		http.Error(w, `{"error":"failed to generate sample"}`, http.StatusInternalServerError)
		return
	}

	b64 := base64.StdEncoding.EncodeToString(pngBytes)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"payload":  text,
		"data_url": "data:image/png;base64," + b64,
	})
}

func sendJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(QRTransResponse{
		Success: false,
		Error:   msg,
	})
}
