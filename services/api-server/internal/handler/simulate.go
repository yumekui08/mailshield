package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// SimulateHandler はポリシーシミュレーションのプロキシハンドラーである。
// リクエストで受け取った EML を smtp-gateway の /simulate エンドポイントに転送し、
// 結果をそのまま返す。
type SimulateHandler struct {
	gatewayURL string
	client     *http.Client
}

// NewSimulateHandler は SimulateHandler を初期化する。
// gatewayURL は smtp-gateway のヘルスポートベース URL（例: http://smtp-gateway:8080）。
func NewSimulateHandler(gatewayURL string) *SimulateHandler {
	return &SimulateHandler{
		gatewayURL: gatewayURL,
		// smtp-gateway の simulate は 30s タイムアウト。
		// http.DefaultClient はタイムアウトがなく smtp-gateway 無応答時に goroutine が永久ブロックするため
		// 専用クライアントを使う。リクエストコンテキストのキャンセルは NewRequestWithContext で伝播済み。
		client: &http.Client{Timeout: 35 * time.Second},
	}
}

// HandleSimulate は EML を受け取り smtp-gateway にシミュレーションを依頼する。
// リクエスト: POST /api/v1/simulate
//   - Content-Type: message/rfc822 または application/octet-stream
//   - ボディ: 生の EML バイト列
//
// レスポンス: smtp-gateway の SimulateResult JSON をそのまま返す。
func (h *SimulateHandler) HandleSimulate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil || len(body) == 0 {
		http.Error(w, "request body required (raw EML)", http.StatusBadRequest)
		return
	}

	gwURL := h.gatewayURL + "/simulate"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, gwURL, bytes.NewReader(body))
	if err != nil {
		slog.Error("simulate: リクエスト生成失敗", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "message/rfc822")

	resp, err := h.client.Do(req)
	if err != nil {
		slog.Warn("simulate: smtp-gateway への接続失敗", "url", gwURL, "error", err)
		http.Error(w, "smtp-gateway に接続できません: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK {
		slog.Warn("simulate: smtp-gateway がエラーを返した",
			"status", resp.StatusCode,
			"body", string(respBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	// レスポンスを検証して返す（不正な JSON を素通りさせない）
	var result json.RawMessage
	if err := json.Unmarshal(respBody, &result); err != nil {
		slog.Error("simulate: smtp-gateway の応答が JSON ではない", "error", err)
		http.Error(w, "invalid response from gateway", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(respBody)
}
