package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminUpstreamKeyManagementSecurity(t *testing.T) {
	adminToken, err := GenerateAdminJWT("admin-test", "admin")
	if err != nil {
		t.Fatalf("failed to generate admin token: %v", err)
	}

	// 1. Mock upstream server that responds to test pings
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "Bearer sk-live-test-key-1234567890" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"message":{"content":"pong"}}]}`)
		} else {
			http.Error(w, `{"error":"invalid key"}`, http.StatusUnauthorized)
		}
	}))
	defer mockUpstream.Close()

	// 2. Test Live Key Testing Endpoint
	testReqBody := map[string]string{
		"key":      "sk-live-test-key-1234567890",
		"base_url": mockUpstream.URL,
		"model":    "test-model",
	}
	testJSON, _ := json.Marshal(testReqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/rotator/test-key", bytes.NewBuffer(testJSON))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	AdminAuthMiddleware(AdminTestUpstreamKeyHandler).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("TestSingleKey expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var testResp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &testResp)
	if testResp["success"] != true {
		t.Errorf("Expected success true, got %#v", testResp)
	}

	// 3. Add Key to Pool
	addReqBody := map[string]interface{}{
		"key":        "sk-live-test-key-1234567890",
		"provider":   "Mock Provider",
		"base_url":   mockUpstream.URL,
		"test_first": true,
	}
	addJSON, _ := json.Marshal(addReqBody)
	addReq := httptest.NewRequest(http.MethodPost, "/api/admin/rotator/keys/add", bytes.NewBuffer(addJSON))
	addReq.Header.Set("Authorization", "Bearer "+adminToken)
	addReq.Header.Set("Content-Type", "application/json")
	wAdd := httptest.NewRecorder()

	AdminAuthMiddleware(AdminAddUpstreamKeyHandler).ServeHTTP(wAdd, addReq)
	if wAdd.Code != http.StatusOK {
		t.Fatalf("AddKey expected 200, got %d: %s", wAdd.Code, wAdd.Body.String())
	}

	// CRITICAL SECURITY ASSERTION: Response MUST NOT contain the raw key!
	respStr := wAdd.Body.String()
	if bytes.Contains([]byte(respStr), []byte("sk-live-test-key-1234567890")) {
		t.Fatalf("CRITICAL SECURITY LEAK: Response contained raw secret key: %s", respStr)
	}

	var addResp map[string]interface{}
	_ = json.Unmarshal(wAdd.Body.Bytes(), &addResp)
	keyObj := addResp["key"].(map[string]interface{})
	keyID := keyObj["id"].(string)

	// 4. List Keys
	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/rotator/keys", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	wList := httptest.NewRecorder()
	AdminAuthMiddleware(AdminListUpstreamKeysHandler).ServeHTTP(wList, listReq)

	listStr := wList.Body.String()
	if bytes.Contains([]byte(listStr), []byte("sk-live-test-key-1234567890")) {
		t.Fatalf("CRITICAL SECURITY LEAK: List response contained raw secret key: %s", listStr)
	}

	// 5. Delete Key
	delReqBody := map[string]string{"id": keyID}
	delJSON, _ := json.Marshal(delReqBody)
	delReq := httptest.NewRequest(http.MethodPost, "/api/admin/rotator/keys/delete", bytes.NewBuffer(delJSON))
	delReq.Header.Set("Authorization", "Bearer "+adminToken)
	delReq.Header.Set("Content-Type", "application/json")
	wDel := httptest.NewRecorder()
	AdminAuthMiddleware(AdminDeleteUpstreamKeyHandler).ServeHTTP(wDel, delReq)

	if wDel.Code != http.StatusOK {
		t.Fatalf("DeleteKey expected 200, got %d: %s", wDel.Code, wDel.Body.String())
	}
}
