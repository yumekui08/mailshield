// Package handler はHTTPハンドラーを実装する。
package handler

import (
	"encoding/json"
	"net/http"
)

// HealthHandler はヘルスチェックエンドポイントを処理する。
type HealthHandler struct{}

// NewHealthHandler はHealthHandlerを返す。
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// HandleHealthz はGET /healthz を処理する。
func (h *HealthHandler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
