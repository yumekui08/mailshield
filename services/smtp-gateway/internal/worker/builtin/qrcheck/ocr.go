package qrcheck

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ocrClient は Tesseract REST API を使う OCR テキスト抽出実装。
// POST {endpoint}/ocr にイメージバイナリを送信し、抽出テキストを受け取る。
type ocrClient struct {
	endpoint string
	client   *http.Client
}

func newOCRClient(endpoint string, timeoutSeconds int) *ocrClient {
	t := timeoutSeconds
	if t <= 0 {
		t = 30
	}
	return &ocrClient{
		endpoint: endpoint,
		client:   &http.Client{Timeout: time.Duration(t) * time.Second},
	}
}

// ScanText は画像バイナリを Tesseract に送信して抽出テキストを返す。
func (c *ocrClient) ScanText(ctx context.Context, data []byte, mimeType string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/ocr", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("OCR リクエスト作成失敗: %w", err)
	}
	if mimeType != "" {
		req.Header.Set("Content-Type", mimeType)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("OCR API 呼び出し失敗: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OCR API 異常レスポンス: status=%d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("OCR レスポンス読み込み失敗: %w", err)
	}
	return string(body), nil
}
