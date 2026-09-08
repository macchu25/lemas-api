package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"xkiro-backend/db"
	"xkiro-backend/internal/artqr/qr"
	"xkiro-backend/models"

	qrcode "github.com/skip2/go-qrcode"
)

// generateShortSlug produces a compact 3-character slug for maximum QR brevity (25 chars total URL -> Version 1 = 21x21 modules!)
func generateShortSlug() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

// GenerateLowestVersionQR finds the lowest possible QR Version (down to Version 1 = 21 modules)
func GenerateLowestVersionQR(content string, level qrcode.RecoveryLevel) (*qrcode.QRCode, error) {
	var bestQR *qrcode.QRCode
	minModules := 9999

	var candidates []string
	if strings.HasPrefix(strings.ToLower(content), "http://") || strings.HasPrefix(strings.ToLower(content), "https://") {
		// Uppercase triggers QR Alphanumeric Mode (up to 25 chars in Version 1 = 21x21 modules!)
		candidates = []string{strings.ToUpper(content), content}
	} else {
		candidates = []string{content}
	}

	for _, cand := range candidates {
		for v := 1; v <= 40; v++ {
			q, err := qrcode.NewWithForcedVersion(cand, v, level)
			if err == nil {
				q.DisableBorder = true
				modules := len(q.Bitmap())
				if modules < minModules {
					minModules = modules
					bestQR = q
				}
				break
			}
		}
	}

	if bestQR != nil {
		bestQR.DisableBorder = true
		return bestQR, nil
	}
	defaultQ, err := qrcode.New(content, level)
	if err == nil {
		defaultQ.DisableBorder = true
	}
	return defaultQ, err
}

// CalculateQRModuleCount returns the exact grid dimension (modules per side) for a given text
func CalculateQRModuleCount(text string, level qrcode.RecoveryLevel) int {
	q, err := qrcode.New(text, level)
	if err != nil {
		return 21 // Fallback default
	}
	q.DisableBorder = true
	bm := q.Bitmap()
	if len(bm) > 0 {
		return len(bm)
	}
	return 21
}

// GenerateTransparentQR renders a 2D QR bitmap into a transparent PNG
func GenerateTransparentQR(bm [][]bool, moduleSize int, borderModules int) ([]byte, error) {
	gridSize := len(bm)
	if gridSize == 0 {
		return nil, fmt.Errorf("empty qr bitmap")
	}
	totalModules := gridSize + 2*borderModules
	imgSize := totalModules * moduleSize

	img := image.NewNRGBA(image.Rect(0, 0, imgSize, imgSize))

	// All background pixels are transparent (alpha = 0)
	for y := 0; y < gridSize; y++ {
		for x := 0; x < gridSize; x++ {
			if bm[y][x] {
				startX := (x + borderModules) * moduleSize
				startY := (y + borderModules) * moduleSize
				for py := 0; py < moduleSize; py++ {
					for px := 0; px < moduleSize; px++ {
						img.SetNRGBA(startX+px, startY+py, color.NRGBA{R: 0, G: 0, B: 0, A: 255})
					}
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type IntermediateQRRequest struct {
	Payload string `json:"payload"`
}

type IntermediateQRResponse struct {
	Success            bool    `json:"success"`
	ID                 string  `json:"id"`
	Slug               string  `json:"slug"`
	ShortURL           string  `json:"short_url"`
	TargetURL          string  `json:"target_url"`
	OriginalPayload    string  `json:"original_payload"`
	IsURL              bool    `json:"is_url"`
	QRVersion          int     `json:"qr_version"`
	OriginalDimension  int     `json:"original_dimension"`
	ReducedDimension   int     `json:"reduced_dimension"`
	ModuleCount        int     `json:"module_count"`
	OriginalModules    int     `json:"original_modules"`
	ReducedModules     int     `json:"reduced_modules"`
	ReductionPct       float64 `json:"reduction_pct"`
	ReductionPercent   float64 `json:"reduction_percent"`
	DataURL            string  `json:"data_url"`
	TransparentDataURL string  `json:"transparent_data_url"`
	CleanDataURL       string  `json:"clean_data_url"`
	Message            string  `json:"message,omitempty"`
	Error              string  `json:"error,omitempty"`
}

// CreateIntermediateQRHandler generates an intermediate redirect QR with minimal module count
// POST /api/art-qr/intermediate
func CreateIntermediateQRHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var originalPayload string
	contentType := r.Header.Get("Content-Type")

	if strings.Contains(contentType, "multipart/form-data") {
		_ = r.ParseMultipartForm(10 << 20)
		if text := r.FormValue("payload"); strings.TrimSpace(text) != "" {
			originalPayload = strings.TrimSpace(text)
		} else {
			file, _, err := r.FormFile("image")
			if err == nil {
				defer file.Close()
				data, readErr := io.ReadAll(file)
				if readErr == nil && len(data) > 0 {
					decoded, decErr := qr.DecodeQRCode(data)
					if decErr == nil && decoded != nil && decoded.Payload != "" {
						originalPayload = decoded.Payload
					}
				}
			}
		}
	} else if strings.Contains(contentType, "application/json") {
		var req IntermediateQRRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			originalPayload = strings.TrimSpace(req.Payload)
		}
	}

	if originalPayload == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Vui lòng tải lên ảnh chứa mã QR hợp lệ hoặc nhập nội dung/URL cần tạo mã QR",
		})
		return
	}

	// Determine host domain for short intermediate link
	baseURL := os.Getenv("INTERMEDIATE_BASE_URL")
	if baseURL == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		host := r.Host
		if host == "" {
			host = "localhost:8080"
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, host)
	}

	resp, err := GenerateIntermediateQR(r.Context(), originalPayload, baseURL)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(resp)
}

// GenerateIntermediateQR creates a short redirection slug, saves it in DB, and generates minimal Version 1/2 QR
func GenerateIntermediateQR(ctx context.Context, originalPayload string, baseURL string) (*IntermediateQRResponse, error) {
	originalPayload = strings.TrimSpace(originalPayload)
	if originalPayload == "" {
		return nil, fmt.Errorf("payload không được để trống")
	}

	// Determine if original payload is a valid Web URL
	isURL := false
	parsed, err := url.ParseRequestURI(originalPayload)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		isURL = true
	}

	// Calculate original module dimension (typically Medium error correction)
	originalModules := CalculateQRModuleCount(originalPayload, qrcode.Medium)

	baseURL = strings.TrimRight(baseURL, "/")
	if strings.Contains(baseURL, "api.lemas.io.vn") {
		baseURL = "https://lemas.io.vn"
	}

	// Generate unique slug
	slug := generateShortSlug()
	if db.DB != nil {
		for i := 0; i < 5; i++ {
			if existing, _ := db.DB.GetIntermediateQR(ctx, slug); existing == nil {
				break
			}
			slug = generateShortSlug()
		}
	}

	shortURL := fmt.Sprintf("%s/r/%s", baseURL, slug)

	// Generate intermediate QR with Level L (Lowest possible module count -> Version 1 = 21x21 modules!)
	interQR, err := GenerateLowestVersionQR(shortURL, qrcode.Low)
	if err != nil {
		return nil, fmt.Errorf("không thể khởi tạo mã QR trung gian: %v", err)
	}

	bm := interQR.Bitmap()
	reducedModules := len(bm)
	if reducedModules == 0 {
		reducedModules = 21
	}

	// Calculate module count reduction percentage
	origArea := originalModules * originalModules
	redArea := reducedModules * reducedModules
	reductionPct := 0.0
	if origArea > redArea {
		reductionPct = (1.0 - float64(redArea)/float64(origArea)) * 100.0
	}

	// Render Transparent PNG (Module size = 16px, Border = 2 modules)
	transPNG, err := GenerateTransparentQR(bm, 16, 2)
	if err != nil {
		return nil, fmt.Errorf("lỗi khi render ảnh PNG trong suốt: %v", err)
	}

	// Render Solid White PNG
	solidPNG, _ := interQR.PNG(512)

	// Save to MongoDB / MemoryStore
	now := time.Now()
	qrRecord := &models.IntermediateQR{
		ID:              slug,
		OriginalPayload: originalPayload,
		ShortURL:        shortURL,
		IsURL:           isURL,
		OriginalModules: originalModules,
		ReducedModules:  reducedModules,
		ReductionPct:    reductionPct,
		Hits:            0,
		CreatedAt:       now,
	}

	if db.DB != nil {
		if err := db.DB.CreateIntermediateQR(ctx, qrRecord); err != nil {
			log.Printf("[IntermediateQR] ⚠️ DB save warning: %v", err)
		}
	}

	transDataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(transPNG)
	solidDataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(solidPNG)

	qrVersion := (reducedModules - 17) / 4
	if qrVersion < 1 {
		qrVersion = 1
	}

	return &IntermediateQRResponse{
		Success:            true,
		ID:                 slug,
		Slug:               slug,
		ShortURL:           shortURL,
		TargetURL:          originalPayload,
		OriginalPayload:    originalPayload,
		IsURL:              isURL,
		QRVersion:          qrVersion,
		OriginalDimension:  originalModules,
		ReducedDimension:   reducedModules,
		ModuleCount:        redArea,
		OriginalModules:    origArea,
		ReducedModules:     reducedModules,
		ReductionPct:       reductionPct,
		ReductionPercent:   reductionPct,
		DataURL:            transDataURL,
		TransparentDataURL: transDataURL,
		CleanDataURL:       solidDataURL,
		Message:            fmt.Sprintf("Đã nén từ %dx%d ô xuống %dx%d ô (Giảm %.1f%% modules)!", originalModules, originalModules, reducedModules, reducedModules, reductionPct),
	}, nil
}

// RedirectIntermediateQRHandler handles GET /r/{slug}
func RedirectIntermediateQRHandler(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/r/")
	slug = strings.ToLower(strings.TrimSpace(slug))

	if slug == "" {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	if db.DB == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		render404HTML(w, slug)
		return
	}

	record, err := db.DB.GetIntermediateQR(r.Context(), slug)
	if err != nil || record == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		render404HTML(w, slug)
		return
	}

	// Increment hits in background
	go func(id string) {
		_ = db.DB.IncrementIntermediateQRHits(r.Context(), id)
	}(slug)

	// If payload is a web URL, immediately 302 redirect!
	if record.IsURL || strings.HasPrefix(record.OriginalPayload, "http://") || strings.HasPrefix(record.OriginalPayload, "https://") {
		http.Redirect(w, r, record.OriginalPayload, http.StatusFound)
		return
	}

	// Otherwise, render sleek mobile landing page for VietQR or plain text
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	renderContentLandingHTML(w, record)
}

func render404HTML(w http.ResponseWriter, slug string) {
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="vi">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Mã QR Không Tồn Tại - Lemas AI</title>
	<style>
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #06080e; color: #fff; margin: 0; display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 20px; box-sizing: border-box; text-align: center; }
		.card { background: #0e1320; border: 1px solid rgba(255,255,255,0.1); border-radius: 24px; padding: 36px 28px; max-width: 420px; width: 100%%; box-shadow: 0 20px 60px rgba(0,0,0,0.6); }
		.icon { font-size: 48px; margin-bottom: 16px; }
		h1 { font-size: 20px; margin: 0 0 10px; color: #f87171; }
		p { font-size: 13px; color: #94a3b8; line-height: 1.6; margin: 0 0 24px; }
		.btn { display: inline-block; background: linear-gradient(135deg, #06b6d4, #3b82f6); color: #fff; text-decoration: none; font-weight: bold; font-size: 13px; padding: 12px 28px; border-radius: 12px; }
	</style>
</head>
<body>
	<div class="card">
		<div class="icon">⚠️</div>
		<h1>Mã QR Không Tồn Tại</h1>
		<p>Mã liên kết trung gian <code>/r/%s</code> không tìm thấy hoặc đã hết hạn trong hệ thống.</p>
		<a href="/" class="btn">Về Trang Chủ Lemas.AI</a>
	</div>
</body>
</html>`, html.EscapeString(slug))
}

func renderContentLandingHTML(w http.ResponseWriter, record *models.IntermediateQR) {
	escapedPayload := html.EscapeString(record.OriginalPayload)

	// Render original QR code image
	var origQRImgTag string
	var origQRDataURL string
	if origQ, err := qrcode.New(record.OriginalPayload, qrcode.Medium); err == nil {
		if pngBytes, pngErr := origQ.PNG(450); pngErr == nil {
			b64 := base64.StdEncoding.EncodeToString(pngBytes)
			origQRDataURL = "data:image/png;base64," + b64
			origQRImgTag = fmt.Sprintf(`<div class="qr-card"><img src="%s" alt="Mã QR Gốc" class="qr-img" /><p class="qr-sub">Mã QR Gốc Dùng Để Quét Thanh Toán / Nhận Dữ Liệu</p></div>`, origQRDataURL)
		}
	}

	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="vi">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Mã QR Gốc - Lemas Gateway</title>
	<style>
		* { box-sizing: border-box; margin: 0; padding: 0; }
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #070a12; color: #fff; display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 16px; }
		.card { background: #0d1222; border: 1px solid rgba(255,255,255,0.12); border-radius: 24px; padding: 28px 20px; max-width: 440px; width: 100%%; box-shadow: 0 24px 70px rgba(0,0,0,0.7); text-align: center; }
		.badge { display: inline-flex; align-items: center; gap: 6px; background: rgba(16,185,129,0.15); border: 1px solid rgba(16,185,129,0.3); color: #34d399; font-size: 11px; font-weight: bold; padding: 5px 14px; border-radius: 20px; margin-bottom: 14px; }
		h1 { font-size: 18px; font-weight: 800; margin-bottom: 6px; color: #fff; }
		p.desc { font-size: 12px; color: #94a3b8; margin-bottom: 18px; }
		.qr-card { background: #fff; border-radius: 18px; padding: 16px; display: inline-block; margin-bottom: 18px; box-shadow: 0 10px 30px rgba(0,0,0,0.4); max-width: 100%%; }
		.qr-img { width: 240px; height: 240px; max-width: 100%%; display: block; margin: 0 auto; object-contain: contain; }
		.qr-sub { font-size: 11px; font-weight: 700; color: #0f172a; margin-top: 10px; }
		.payload-box { background: #05070d; border: 1px solid rgba(255,255,255,0.08); border-radius: 14px; padding: 12px; font-family: monospace; font-size: 11px; color: #38bdf8; text-align: left; word-break: break-all; max-height: 100px; overflow-y: auto; margin-bottom: 16px; line-height: 1.5; }
		.actions { display: flex; flex-direction: column; gap: 10px; }
		.btn-primary { background: linear-gradient(135deg, #10b981, #06b6d4); color: #041017; font-weight: 800; font-size: 13px; padding: 13px; border-radius: 12px; border: none; cursor: pointer; text-decoration: none; display: flex; align-items: center; justify-content: center; gap: 6px; }
		.btn-secondary { background: rgba(255,255,255,0.08); color: #cbd5e1; font-weight: 600; font-size: 12px; padding: 11px; border-radius: 12px; border: 1px solid rgba(255,255,255,0.1); cursor: pointer; }
		.hint { font-size: 10px; color: #64748b; margin-top: 14px; }
	</style>
</head>
<body>
	<div class="card">
		<div class="badge">⚡ Đã Chuyển Về Mã QR Gốc</div>
		<h1>Mã QR Thanh Toán / Gốc</h1>
		<p class="desc">Bạn đã quét từ mã QR rút gọn tối giản (21×21 ô) của Lemas AI</p>
		
		%s

		<div class="payload-box" id="payloadText">%s</div>

		<div class="actions">
			<a href="%s" download="ma_qr_goc.png" class="btn-primary">💾 Tải Ảnh QR Gốc Vào Điện Thoại</a>
			<button class="btn-secondary" onclick="copyContent()">📋 Sao Chép Chuỗi Dữ Liệu</button>
		</div>

		<p class="hint">Bạn có thể dùng App Ngân Hàng chọn ảnh vừa tải để thanh toán tức thì.</p>
	</div>
	<script>
		function copyContent() {
			var text = document.getElementById('payloadText').innerText;
			navigator.clipboard.writeText(text).then(function() {
				alert('Đã sao chép nội dung vào khay nhớ tạm!');
			});
		}
	</script>
</body>
</html>`, origQRImgTag, escapedPayload, origQRDataURL)
}

// ResolveIntermediateQRHandler handles GET /api/r/resolve?slug={slug}
func ResolveIntermediateQRHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	slug := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("slug")))
	if slug == "" {
		slug = strings.TrimPrefix(r.URL.Path, "/api/r/resolve/")
	}
	if slug == "" {
		http.Error(w, `{"error":"slug parameter is required"}`, http.StatusBadRequest)
		return
	}

	if db.DB == nil {
		http.Error(w, `{"error":"database unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	record, err := db.DB.GetIntermediateQR(r.Context(), slug)
	if err != nil || record == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"slug":       record.ID,
		"target_url": record.OriginalPayload,
		"is_url":     record.IsURL,
	})
}

