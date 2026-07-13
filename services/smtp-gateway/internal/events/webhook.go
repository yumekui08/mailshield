package events

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// WebhookPublisher は mail.received イベントを HTTP POST で外部システムへ通知する。
//
//   - ペイロードは domain.MailEvent の JSON（EML 本文は含まない）
//   - secret が設定されている場合、ボディの HMAC-SHA256 を
//     `X-MailShield-Signature: sha256=<hex>` ヘッダーに付与する
//   - ネットワークエラー・5xx はリトライする（4xx は受信側の恒久エラーとみなし即諦める）
//   - 呼び出し元のコンテキスト（イベント発行タイムアウト）を超えるリトライは行わない
type WebhookPublisher struct {
	url        string
	secret     string
	maxRetries int
	backoff    time.Duration
	client     *http.Client
}

// NewWebhook は WebhookPublisher を返す。
// timeoutSeconds は 1 リクエストあたりの HTTP タイムアウト。
func NewWebhook(url, secret string, timeoutSeconds, maxRetries, retryBackoffSeconds int) (*WebhookPublisher, error) {
	if url == "" {
		return nil, fmt.Errorf("events.webhook.url が設定されていません")
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if retryBackoffSeconds <= 0 {
		retryBackoffSeconds = 1
	}
	return &WebhookPublisher{
		url:        url,
		secret:     secret,
		maxRetries: maxRetries,
		backoff:    time.Duration(retryBackoffSeconds) * time.Second,
		client:     &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
	}, nil
}

// PublishMailReceived はイベントを webhook 先へ POST する。
func (p *WebhookPublisher) PublishMailReceived(ctx context.Context, event *domain.MailEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("イベント JSON エンコード失敗: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= p.maxRetries; attempt++ {
		retryable, err := p.post(ctx, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable {
			return fmt.Errorf("webhook 発行失敗（リトライ不可）: %w", err)
		}
		slog.Warn("webhook 発行失敗（リトライ）",
			"message_id", event.MessageID, "attempt", attempt, "error", err)
		if attempt < p.maxRetries {
			select {
			case <-ctx.Done():
				return fmt.Errorf("webhook 発行タイムアウト: %w", ctx.Err())
			case <-time.After(p.backoff):
			}
		}
	}
	return fmt.Errorf("webhook 発行失敗（%d 回試行）: %w", p.maxRetries, lastErr)
}

// post は 1 回の HTTP POST を行う。戻り値はリトライすべきかどうかとエラー。
func (p *WebhookPublisher) post(ctx context.Context, body []byte) (retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("リクエスト作成失敗: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MailShield-Event", "mail.received")
	if p.secret != "" {
		mac := hmac.New(sha256.New, []byte(p.secret))
		mac.Write(body)
		req.Header.Set("X-MailShield-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return true, err // ネットワークエラーはリトライ対象
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return false, nil
	case resp.StatusCode >= 500:
		return true, fmt.Errorf("webhook 先が %d を返しました", resp.StatusCode)
	default:
		// 4xx は設定誤り・拒否とみなしリトライしない
		return false, fmt.Errorf("webhook 先が %d を返しました", resp.StatusCode)
	}
}

// Close は何もしない（保持する接続プールは http.Client が管理する）。
func (p *WebhookPublisher) Close() error { return nil }
