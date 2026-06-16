package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleHealthz(t *testing.T) {
	h := NewHealthHandler()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	h.HandleHealthz(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type 期待: application/json, 実際: %s", contentType)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("レスポンスJSONデコード失敗: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status 期待: ok, 実際: %s", body["status"])
	}
}
