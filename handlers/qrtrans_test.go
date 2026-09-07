package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	qrcode "github.com/skip2/go-qrcode"
)

func TestQRRemoveBackgroundHandlerUnauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/admin/art-qr/isolate-transparent", nil)
	w := httptest.NewRecorder()

	handler := AdminAuthMiddleware(QRRemoveBackgroundHandler)
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized without token, got %d", w.Code)
	}
}

func TestQRRemoveBackgroundHandlerWithAdminToken(t *testing.T) {
	// Generate valid admin token
	token, err := GenerateAdminJWT("admin-test", "admin")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Create test QR PNG bytes
	qrBytes, err := qrcode.Encode("https://nornai.com/admin-test", qrcode.Medium, 200)
	if err != nil {
		t.Fatalf("failed to encode QR: %v", err)
	}

	// Prepare multipart request
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "test_qr.png")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	_, _ = part.Write(qrBytes)
	_ = writer.WriteField("crop_mode", "none")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/art-qr/isolate-transparent", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	handler := AdminAuthMiddleware(QRRemoveBackgroundHandler)
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp QRTransResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	if !resp.Success {
		t.Errorf("Expected success true, got false. Error: %s", resp.Error)
	}
	if !resp.QRValid {
		t.Errorf("Expected QRValid true")
	}
	if resp.DataURL == "" {
		t.Errorf("Expected non-empty dataUrl")
	}
}
