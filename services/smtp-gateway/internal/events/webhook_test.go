package events

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

func testEvent() *domain.MailEvent {
	return &domain.MailEvent{
		MessageID:   "msg-1",
		EMLPath:     "raw/2026/07/13/msg-1.eml",
		FromAddress: "sender@external.test",
		ToAddresses: []string{"user@internal.test"},
		Subject:     "テスト",
	}
}

func TestWebhookPublish_Success(t *testing.T) {
	var gotBody []byte
	var gotEventHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotEventHeader = r.Header.Get("X-MailShield-Event")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p, err := NewWebhook(srv.URL, "", 5, 3, 1)
	if err != nil {
		t.Fatalf("NewWebhook 失敗: %v", err)
	}
	if err := p.PublishMailReceived(context.Background(), testEvent()); err != nil {
		t.Fatalf("発行失敗: %v", err)
	}
	if gotEventHeader != "mail.received" {
		t.Errorf("X-MailShield-Event = %q, want mail.received", gotEventHeader)
	}
	if len(gotBody) == 0 || gotBody[0] != '{' {
		t.Errorf("JSON ボディが送信されていない: %q", gotBody)
	}
}

func TestWebhookPublish_HMACSignature(t *testing.T) {
	const secret = "test-secret"
	var gotSig string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-MailShield-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p, _ := NewWebhook(srv.URL, secret, 5, 1, 1)
	if err := p.PublishMailReceived(context.Background(), testEvent()); err != nil {
		t.Fatalf("発行失敗: %v", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(gotBody)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != want {
		t.Errorf("署名 = %q, want %q", gotSig, want)
	}
}

func TestWebhookPublish_RetryOn5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p, _ := NewWebhook(srv.URL, "", 5, 3, 1)
	p.backoff = time.Millisecond // テスト高速化
	if err := p.PublishMailReceived(context.Background(), testEvent()); err != nil {
		t.Fatalf("3回目で成功するはず: %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("試行回数 = %d, want 3", calls.Load())
	}
}

func TestWebhookPublish_NoRetryOn4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	p, _ := NewWebhook(srv.URL, "", 5, 3, 1)
	if err := p.PublishMailReceived(context.Background(), testEvent()); err == nil {
		t.Fatal("4xx はエラーを返すべき")
	}
	if calls.Load() != 1 {
		t.Errorf("4xx はリトライしないはず: 試行回数 = %d", calls.Load())
	}
}

func TestWebhookPublish_ContextCancelStopsRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p, _ := NewWebhook(srv.URL, "", 5, 10, 5)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	if err := p.PublishMailReceived(ctx, testEvent()); err == nil {
		t.Fatal("キャンセルでエラーを返すべき")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("コンテキスト期限後もリトライし続けている (elapsed=%v)", elapsed)
	}
}

func TestNewWebhook_RequiresURL(t *testing.T) {
	if _, err := NewWebhook("", "", 5, 3, 1); err == nil {
		t.Error("URL 未設定はエラーを返すべき")
	}
}

func TestWebhookPublish_MailProcessed(t *testing.T) {
	var gotEventHeader string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEventHeader = r.Header.Get("X-MailShield-Event")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p, _ := NewWebhook(srv.URL, "", 5, 3, 1)
	event := &domain.MailProcessedEvent{
		MessageID:  "msg-1",
		Route:      "10-inbound",
		Action:     "quarantine",
		TotalScore: 130,
		InspectScores: []domain.InspectScore{
			{Worker: "av-worker", Score: 100, Detected: true},
			{Worker: "url-worker", Score: 30, Detected: false},
		},
	}
	if err := p.PublishMailProcessed(context.Background(), event); err != nil {
		t.Fatalf("発行失敗: %v", err)
	}
	if gotEventHeader != "mail.processed" {
		t.Errorf("X-MailShield-Event = %q, want mail.processed", gotEventHeader)
	}
	body := string(gotBody)
	for _, want := range []string{`"action":"quarantine"`, `"total_score":130`, `"av-worker"`} {
		if !strings.Contains(body, want) {
			t.Errorf("ボディに %q が含まれない: %s", want, body)
		}
	}
}
