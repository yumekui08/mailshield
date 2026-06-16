package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

var testAuditLogger = audit.New(nil)

// startDummySMTP はテスト用のダミー SMTP サーバーを起動し、ホスト:ポートを返す。
// 接続を1件受け付けて即座に 250 OK を返す最低限の実装。
func startDummySMTP(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ダミー SMTP サーバー起動失敗: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprintln(conn, "220 dummy SMTP ready")
		scanner := bufio.NewScanner(conn)
		inData := false
		for scanner.Scan() {
			line := scanner.Text()
			if inData {
				// DATA フェーズ: "." が来るまでサイレントに読み続ける
				if line == "." {
					inData = false
					fmt.Fprintln(conn, "250 OK")
				}
				continue
			}
			switch {
			case len(line) >= 4 && line[:4] == "QUIT":
				fmt.Fprintln(conn, "221 Bye")
				return
			case len(line) >= 4 && line[:4] == "DATA":
				inData = true
				fmt.Fprintln(conn, "354 Start input")
			default:
				fmt.Fprintln(conn, "250 OK")
			}
		}
	}()

	return ln.Addr().String()
}

func quarantinedMessage(id string) domain.Message {
	now := time.Now().Truncate(time.Second)
	return domain.Message{
		ID:          id,
		EMLPath:     "/quarantine/" + id + ".eml",
		FromAddress: "bad@example.com",
		ToAddresses: []string{"victim@example.com"},
		Subject:     "virus test",
		SizeBytes:   2048,
		Status:      domain.StatusQuarantined,
		ReceivedAt:  now,
		UpdatedAt:   now,
	}
}

func TestQuarantineHandleList_Success(t *testing.T) {
	messages := []domain.Message{quarantinedMessage("q-1"), quarantinedMessage("q-2")}
	repo := &mockRepository{
		listQuarantineFunc: func(ctx context.Context, q domain.ListQuery) ([]domain.Message, int, error) {
			return messages, 2, nil
		},
	}

	h := NewQuarantineHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, config.MailboxPolicyConfig{}, testAuditLogger)
	req := requestWithSession(http.MethodGet, "/api/v1/quarantine", adminSession())
	rr := httptest.NewRecorder()

	h.HandleList(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}

	var result domain.PagedResult[domain.Message]
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("レスポンスJSONデコード失敗: %v", err)
	}
	if result.Meta.Total != 2 {
		t.Errorf("Meta.Total 期待: 2, 実際: %d", result.Meta.Total)
	}
	if len(result.Data) != 2 {
		t.Errorf("Data 件数 期待: 2, 実際: %d", len(result.Data))
	}
}

func TestQuarantineHandleGet_Success(t *testing.T) {
	msg := quarantinedMessage("q-detail-1")
	detail := &domain.MessageDetail{
		Message:        msg,
		InspectResults: []domain.InspectResult{},
	}
	repo := &mockRepository{
		getQuarantineFunc: func(ctx context.Context, id string) (*domain.MessageDetail, error) {
			return detail, nil
		},
	}

	h := NewQuarantineHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, config.MailboxPolicyConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodGet, "/api/v1/quarantine/q-detail-1", "id", "q-detail-1", adminSession())
	rr := httptest.NewRecorder()

	h.HandleGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}

	var got domain.MessageDetail
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("レスポンスJSONデコード失敗: %v", err)
	}
	if got.ID != "q-detail-1" {
		t.Errorf("ID 期待: q-detail-1, 実際: %s", got.ID)
	}
	if got.Status != domain.StatusQuarantined {
		t.Errorf("Status 期待: %s, 実際: %s", domain.StatusQuarantined, got.Status)
	}
}

func TestQuarantineHandleGet_NotFound(t *testing.T) {
	repo := &mockRepository{
		getQuarantineFunc: func(ctx context.Context, id string) (*domain.MessageDetail, error) {
			return nil, errors.New("隔離メッセージが見つかりません")
		},
	}

	h := NewQuarantineHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, config.MailboxPolicyConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodGet, "/api/v1/quarantine/nonexistent", "id", "nonexistent", adminSession())
	rr := httptest.NewRecorder()

	h.HandleGet(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("ステータスコード 期待: 404, 実際: %d", rr.Code)
	}
}

func TestQuarantineHandleRelease_Success(t *testing.T) {
	smtpAddr := startDummySMTP(t)
	host, portStr, _ := net.SplitHostPort(smtpAddr)
	port := 0
	fmt.Sscanf(portStr, "%d", &port)

	processedPath := "tenant/processed/2026/06/14/q-release-1.eml"
	msg := quarantinedMessage("q-release-1")
	msg.ProcessedEMLPath = &processedPath
	detail := &domain.MessageDetail{
		Message:        msg,
		InspectResults: []domain.InspectResult{},
	}
	stor := &mockEMLStorage{
		getEMLFunc: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("From: bad@example.com\r\nTo: victim@example.com\r\nSubject: test\r\n\r\nbody\r\n"), nil
		},
	}
	repo := &mockRepository{
		getQuarantineFunc: func(ctx context.Context, id string) (*domain.MessageDetail, error) {
			return detail, nil
		},
		updateMessageStatusFunc: func(ctx context.Context, id string, status domain.MessageStatus) error {
			return nil
		},
	}
	notifCfg := config.NotificationConfig{ReinjectHost: host, ReinjectPort: port}

	h := NewQuarantineHandler(repo, stor, notifCfg, config.MailboxPolicyConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodPost, "/api/v1/quarantine/q-release-1/release", "id", "q-release-1", adminSession())
	rr := httptest.NewRecorder()

	h.HandleRelease(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}

	var got map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("レスポンスJSONデコード失敗: %v", err)
	}
	if got["id"] != "q-release-1" {
		t.Errorf("id 期待: q-release-1, 実際: %s", got["id"])
	}
	if got["status"] != string(domain.StatusDelivered) {
		t.Errorf("status 期待: %s, 実際: %s", domain.StatusDelivered, got["status"])
	}
}

func TestQuarantineHandleDelete_Success(t *testing.T) {
	msg := quarantinedMessage("q-delete-1")
	detail := &domain.MessageDetail{
		Message:        msg,
		InspectResults: []domain.InspectResult{},
	}
	repo := &mockRepository{
		getQuarantineFunc: func(ctx context.Context, id string) (*domain.MessageDetail, error) {
			return detail, nil
		},
		updateMessageStatusFunc: func(ctx context.Context, id string, status domain.MessageStatus) error {
			return nil
		},
	}

	h := NewQuarantineHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, config.MailboxPolicyConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodDelete, "/api/v1/quarantine/q-delete-1", "id", "q-delete-1", adminSession())
	rr := httptest.NewRecorder()

	h.HandleDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}

	var got map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("レスポンスJSONデコード失敗: %v", err)
	}
	if got["status"] != string(domain.StatusRejected) {
		t.Errorf("status 期待: %s, 実際: %s", domain.StatusRejected, got["status"])
	}
}
