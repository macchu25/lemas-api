package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xkiro-backend/db"
)

func TestIntermediateQRGenerationAndRedirection(t *testing.T) {
	// Initialize memory store for testing
	db.InitDB()

	// 1. Test creating intermediate QR from a long URL (over 100 characters)
	longURL := "https://example.com/very/long/deep/nested/path/with/multiple/query/parameters?user_id=1234567890&session_token=abcdef0123456789&referrer=social_media_campaign"
	reqBody := map[string]string{
		"payload": longURL,
	}
	bodyJSON, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/art-qr/intermediate", bytes.NewBuffer(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	CreateIntermediateQRHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp IntermediateQRResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}

	if !resp.Success {
		t.Fatalf("expected success true, got false. error: %s", resp.Error)
	}

	if resp.ID == "" {
		t.Fatalf("expected non-empty slug ID")
	}

	t.Logf("resp.ShortURL = %s (length %d)", resp.ShortURL, len(resp.ShortURL))
	t.Logf("Result: original=%dx%d (%d modules) -> reduced=%dx%d (%d modules) | Reduction: %.1f%%",
		resp.OriginalModules, resp.OriginalModules, resp.OriginalModules*resp.OriginalModules,
		resp.ReducedModules, resp.ReducedModules, resp.ReducedModules*resp.ReducedModules,
		resp.ReductionPct)

	if resp.ReducedModules != 21 {
		t.Errorf("expected reduced modules == 21 (Version 1 minimum), got %d", resp.ReducedModules)
	}

	if resp.OriginalModules <= resp.ReducedModules {
		t.Errorf("expected original modules (%d) > reduced modules (%d)", resp.OriginalModules, resp.ReducedModules)
	}

	if resp.ReductionPct < 70.0 {
		t.Errorf("expected module reduction >= 70%%, got %.1f%%", resp.ReductionPct)
	}

	if !strings.HasPrefix(resp.TransparentDataURL, "data:image/png;base64,") {
		t.Errorf("expected valid data URL for transparent QR")
	}

	// 2. Test Redirecting to the original URL via GET /r/{slug}
	redirectReq := httptest.NewRequest(http.MethodGet, "/r/"+resp.ID, nil)
	redirectW := httptest.NewRecorder()

	RedirectIntermediateQRHandler(redirectW, redirectReq)

	if redirectW.Code != http.StatusFound {
		t.Fatalf("expected 302 Found, got %d: %s", redirectW.Code, redirectW.Body.String())
	}

	loc := redirectW.Header().Get("Location")
	if loc != longURL {
		t.Fatalf("expected redirect Location %q, got %q", longURL, loc)
	}

	// 3. Test Plain Text / VietQR string
	vietQRText := "00020101021238540010A00000072701240006970422011009876543210208QRIBFTTA5204599953037045802VN5909NGUYEN VAN A6005HANOI62140810UNG HO DONG6304E1F2"
	txtReqBody := map[string]string{"payload": vietQRText}
	txtBodyJSON, _ := json.Marshal(txtReqBody)
	txtReq := httptest.NewRequest(http.MethodPost, "/api/art-qr/intermediate", bytes.NewBuffer(txtBodyJSON))
	txtReq.Header.Set("Content-Type", "application/json")
	txtW := httptest.NewRecorder()

	CreateIntermediateQRHandler(txtW, txtReq)
	if txtW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for VietQR text, got %d: %s", txtW.Code, txtW.Body.String())
	}

	var txtResp IntermediateQRResponse
	_ = json.Unmarshal(txtW.Body.Bytes(), &txtResp)

	if txtResp.ReducedModules != 21 {
		t.Logf("VietQR reduced modules: %d (original: %d)", txtResp.ReducedModules, txtResp.OriginalModules)
	}

	// Test GET /r/{slug} for non-URL returns HTML landing page
	txtRedirectReq := httptest.NewRequest(http.MethodGet, "/r/"+txtResp.ID, nil)
	txtRedirectW := httptest.NewRecorder()

	RedirectIntermediateQRHandler(txtRedirectW, txtRedirectReq)

	if txtRedirectW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for text landing page, got %d", txtRedirectW.Code)
	}

	if !strings.Contains(txtRedirectW.Body.String(), "NGUYEN VAN A") {
		t.Errorf("expected HTML landing page to contain decoded VietQR content")
	}
}
