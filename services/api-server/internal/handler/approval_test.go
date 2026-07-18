package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
)

// viewerSession は閲覧者ロールのセッションを返す。
func viewerSession(userID string) *domain.Session {
	return &domain.Session{
		ID:        "session-viewer-" + userID,
		Role:      domain.RoleViewer,
		User:      domain.UserClaims{Sub: userID, Email: userID + "@example.com"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
}

func requestWithSessionURLParamAndBody(method, target, paramKey, paramValue string, body []byte, session *domain.Session) *http.Request {
	var req *http.Request
	if len(body) > 0 {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramKey, paramValue)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = middleware.WithSession(ctx, session)
	return req.WithContext(ctx)
}

func sampleApprovalRequest(id, messageID, approverID string) domain.ApprovalRequest {
	now := time.Now().Truncate(time.Second)
	return domain.ApprovalRequest{
		ID:         id,
		MessageID:  messageID,
		ApproverID: &approverID,
		Status:     domain.ApprovalStatusPending,
		ExpiresAt:  now.Add(72 * time.Hour),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func sampleApprovalMessage(messageID string) domain.Message {
	now := time.Now().Truncate(time.Second)
	return domain.Message{
		ID:          messageID,
		EMLPath:     "/raw/" + messageID + ".eml",
		FromAddress: "sender@example.com",
		ToAddresses: []string{"to@example.com"},
		Subject:     "承認テストメール",
		SizeBytes:   512,
		Status:      domain.StatusApprovalPending,
		ReceivedAt:  now,
		UpdatedAt:   now,
	}
}

// ─── HandleList ───────────────────────────────────────────────────────────────

func TestApprovalHandleList_Admin_ReturnsAll(t *testing.T) {
	items := []domain.ApprovalRequest{
		sampleApprovalRequest("apr-1", "msg-1", "approver-a"),
		sampleApprovalRequest("apr-2", "msg-2", "approver-b"),
	}
	repo := &mockRepository{
		listAllApprovalRequestsFunc: func(_ context.Context) ([]domain.ApprovalRequest, error) {
			return items, nil
		},
	}

	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSession(http.MethodGet, "/api/v1/approvals", adminSession())
	rr := httptest.NewRecorder()
	h.HandleList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}
	var result struct {
		Items []domain.ApprovalRequest `json:"items"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("JSONデコード失敗: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("件数 期待: 2, 実際: %d", len(result.Items))
	}
}

func TestApprovalHandleList_Viewer_ReturnsOwn(t *testing.T) {
	const approverID = "viewer-user-1"
	items := []domain.ApprovalRequest{
		sampleApprovalRequest("apr-3", "msg-3", approverID),
	}
	repo := &mockRepository{
		listApprovalRequestsFunc: func(_ context.Context, id string) ([]domain.ApprovalRequest, error) {
			if id != approverID {
				t.Errorf("期待する approverID: %s, 実際: %s", approverID, id)
			}
			return items, nil
		},
	}

	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSession(http.MethodGet, "/api/v1/approvals", viewerSession(approverID))
	rr := httptest.NewRecorder()
	h.HandleList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}
	var result struct {
		Items []domain.ApprovalRequest `json:"items"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("JSONデコード失敗: %v", err)
	}
	if len(result.Items) != 1 {
		t.Errorf("件数 期待: 1, 実際: %d", len(result.Items))
	}
}

// ─── HandleGet ───────────────────────────────────────────────────────────────

func TestApprovalHandleGet_Admin_Found(t *testing.T) {
	apr := sampleApprovalRequest("apr-get-1", "msg-get-1", "approver-x")
	msg := sampleApprovalMessage("msg-get-1")
	detail := &domain.MessageDetail{Message: msg, InspectResults: []domain.InspectResult{}}
	repo := &mockRepository{
		getApprovalRequestFunc: func(_ context.Context, id string) (*domain.ApprovalRequest, error) {
			if id == "apr-get-1" {
				return &apr, nil
			}
			return nil, nil
		},
		getMessageFunc: func(_ context.Context, _ string) (*domain.MessageDetail, error) {
			return detail, nil
		},
	}

	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodGet, "/api/v1/approvals/apr-get-1", "id", "apr-get-1", adminSession())
	rr := httptest.NewRecorder()
	h.HandleGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}
	var got domain.ApprovalRequestDetail
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("JSONデコード失敗: %v", err)
	}
	if got.ID != "apr-get-1" {
		t.Errorf("ID 期待: apr-get-1, 実際: %s", got.ID)
	}
}

func TestApprovalHandleGet_NotFound(t *testing.T) {
	repo := &mockRepository{
		getApprovalRequestFunc: func(_ context.Context, _ string) (*domain.ApprovalRequest, error) {
			return nil, nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodGet, "/api/v1/approvals/nonexistent", "id", "nonexistent", adminSession())
	rr := httptest.NewRecorder()
	h.HandleGet(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("ステータスコード 期待: 404, 実際: %d", rr.Code)
	}
}

func TestApprovalHandleGet_Viewer_Forbidden_OtherApprover(t *testing.T) {
	apr := sampleApprovalRequest("apr-forbidden", "msg-x", "other-approver")
	repo := &mockRepository{
		getApprovalRequestFunc: func(_ context.Context, _ string) (*domain.ApprovalRequest, error) {
			return &apr, nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	// viewer のユーザーIDは "viewer-1" だが、承認者は "other-approver"
	req := requestWithSessionAndURLParam(http.MethodGet, "/api/v1/approvals/apr-forbidden", "id", "apr-forbidden", viewerSession("viewer-1"))
	rr := httptest.NewRecorder()
	h.HandleGet(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("ステータスコード 期待: 403, 実際: %d", rr.Code)
	}
}

// ─── HandleApprove ───────────────────────────────────────────────────────────

func TestApprovalHandleApprove_Success(t *testing.T) {
	smtpAddr := startDummySMTP(t)
	host, portStr, _ := net.SplitHostPort(smtpAddr)
	port := 0
	fmt.Sscanf(portStr, "%d", &port)

	apr := sampleApprovalRequest("apr-approve-1", "msg-approve-1", "approver-a")
	msg := sampleApprovalMessage("msg-approve-1")
	detail := &domain.MessageDetail{Message: msg, InspectResults: []domain.InspectResult{}}

	var capturedStatus domain.ApprovalStatus
	repo := &mockRepository{
		getApprovalRequestFunc: func(_ context.Context, _ string) (*domain.ApprovalRequest, error) {
			return &apr, nil
		},
		getMessageFunc: func(_ context.Context, _ string) (*domain.MessageDetail, error) {
			return detail, nil
		},
		updateMessageStatusFunc: func(_ context.Context, _ string, _ domain.MessageStatus) error {
			return nil
		},
		updateApprovalStatusFunc: func(_ context.Context, _ string, s domain.ApprovalStatus, _ *string) error {
			capturedStatus = s
			return nil
		},
	}
	stor := &mockEMLStorage{
		getEMLFunc: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("From: sender@example.com\r\nTo: to@example.com\r\nSubject: test\r\n\r\nbody\r\n"), nil
		},
	}

	notifCfg := config.NotificationConfig{ReinjectHost: host, ReinjectPort: port}
	h := NewApprovalHandler(repo, stor, notifCfg, testAuditLogger)

	bodyJSON, _ := json.Marshal(map[string]string{"comment": "問題なし"})
	req := requestWithSessionURLParamAndBody(http.MethodPost, "/api/v1/approvals/apr-approve-1/approve", "id", "apr-approve-1", bodyJSON, adminSession())
	rr := httptest.NewRecorder()
	h.HandleApprove(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("ステータスコード 期待: 200, 実際: %d (body: %s)", rr.Code, rr.Body.String())
	}
	if capturedStatus != domain.ApprovalStatusApproved {
		t.Errorf("ステータス 期待: approved, 実際: %s", capturedStatus)
	}
}

func TestApprovalHandleApprove_AlreadyProcessed_Conflict(t *testing.T) {
	apr := sampleApprovalRequest("apr-conflict", "msg-conflict", "approver-a")
	apr.Status = domain.ApprovalStatusApproved // すでに処理済み

	repo := &mockRepository{
		getApprovalRequestFunc: func(_ context.Context, _ string) (*domain.ApprovalRequest, error) {
			return &apr, nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodPost, "/api/v1/approvals/apr-conflict/approve", "id", "apr-conflict", adminSession())
	rr := httptest.NewRecorder()
	h.HandleApprove(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("ステータスコード 期待: 409, 実際: %d", rr.Code)
	}
}

// ─── HandleReject ────────────────────────────────────────────────────────────

func TestApprovalHandleReject_Success(t *testing.T) {
	apr := sampleApprovalRequest("apr-reject-1", "msg-reject-1", "approver-a")

	var capturedStatus domain.ApprovalStatus
	repo := &mockRepository{
		getApprovalRequestFunc: func(_ context.Context, _ string) (*domain.ApprovalRequest, error) {
			return &apr, nil
		},
		updateApprovalStatusFunc: func(_ context.Context, _ string, s domain.ApprovalStatus, _ *string) error {
			capturedStatus = s
			return nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodPost, "/api/v1/approvals/apr-reject-1/reject", "id", "apr-reject-1", adminSession())
	rr := httptest.NewRecorder()
	h.HandleReject(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}
	if capturedStatus != domain.ApprovalStatusRejected {
		t.Errorf("ステータス 期待: rejected, 実際: %s", capturedStatus)
	}
}

func TestApprovalHandleReject_Viewer_Forbidden(t *testing.T) {
	apr := sampleApprovalRequest("apr-reject-forbidden", "msg-rf", "other-approver")
	repo := &mockRepository{
		getApprovalRequestFunc: func(_ context.Context, _ string) (*domain.ApprovalRequest, error) {
			return &apr, nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodPost, "/api/v1/approvals/apr-reject-forbidden/reject", "id", "apr-reject-forbidden", viewerSession("viewer-2"))
	rr := httptest.NewRecorder()
	h.HandleReject(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("ステータスコード 期待: 403, 実際: %d", rr.Code)
	}
}

// ─── メールボックス承認（mailbox_email 方式） ─────────────────────────────────

// sampleMailboxApprovalRequest は mailbox_email 方式の承認依頼を返す。
func sampleMailboxApprovalRequest(id, messageID, mailboxEmail string) domain.ApprovalRequest {
	now := time.Now().Truncate(time.Second)
	return domain.ApprovalRequest{
		ID:            id,
		MessageID:     messageID,
		MailboxEmails: []string{mailboxEmail},
		Status:        domain.ApprovalStatusPending,
		ExpiresAt:     now.Add(72 * time.Hour),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func TestApprovalHandleReject_Viewer_MailboxAdmin_Allowed(t *testing.T) {
	apr := sampleMailboxApprovalRequest("apr-mb-1", "msg-mb-1", "sales@internal.test")

	var capturedStatus domain.ApprovalStatus
	repo := &mockRepository{
		getApprovalRequestFunc: func(_ context.Context, _ string) (*domain.ApprovalRequest, error) {
			return &apr, nil
		},
		isMailboxApproverFunc: func(_ context.Context, userID, mailboxEmail string) (bool, error) {
			return userID == "viewer-admin" && mailboxEmail == "sales@internal.test", nil
		},
		updateApprovalStatusFunc: func(_ context.Context, _ string, s domain.ApprovalStatus, _ *string) error {
			capturedStatus = s
			return nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodPost, "/api/v1/approvals/apr-mb-1/reject", "id", "apr-mb-1", viewerSession("viewer-admin"))
	rr := httptest.NewRecorder()
	h.HandleReject(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("ステータスコード 期待: 200, 実際: %d (body=%s)", rr.Code, rr.Body.String())
	}
	if capturedStatus != domain.ApprovalStatusRejected {
		t.Errorf("ステータス 期待: rejected, 実際: %s", capturedStatus)
	}
}

func TestApprovalHandleReject_Viewer_NotMailboxAdmin_Forbidden(t *testing.T) {
	apr := sampleMailboxApprovalRequest("apr-mb-2", "msg-mb-2", "sales@internal.test")
	repo := &mockRepository{
		getApprovalRequestFunc: func(_ context.Context, _ string) (*domain.ApprovalRequest, error) {
			return &apr, nil
		},
		isMailboxApproverFunc: func(_ context.Context, _, _ string) (bool, error) {
			return false, nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodPost, "/api/v1/approvals/apr-mb-2/reject", "id", "apr-mb-2", viewerSession("viewer-other"))
	rr := httptest.NewRecorder()
	h.HandleReject(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("ステータスコード 期待: 403, 実際: %d", rr.Code)
	}
}

func TestApprovalHandleApprove_ClaimLost_Conflict(t *testing.T) {
	// 2 人の承認者が同時に決定した場合、クレームに負けた側は 409 を受け、配送は行われない
	apr := sampleMailboxApprovalRequest("apr-mb-race", "msg-mb-race", "sales@internal.test")

	reinjectAttempted := false
	repo := &mockRepository{
		getApprovalRequestFunc: func(_ context.Context, _ string) (*domain.ApprovalRequest, error) {
			return &apr, nil
		},
		claimApprovalRequestFunc: func(_ context.Context, _ string, _ domain.ApprovalStatus, _ *string) (bool, error) {
			return false, nil // 他の承認者が先に決定済み
		},
		getMessageFunc: func(_ context.Context, id string) (*domain.MessageDetail, error) {
			reinjectAttempted = true
			msg := sampleApprovalMessage(id)
			return &domain.MessageDetail{Message: msg}, nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodPost, "/api/v1/approvals/apr-mb-race/approve", "id", "apr-mb-race", adminSession())
	rr := httptest.NewRecorder()
	h.HandleApprove(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("ステータスコード 期待: 409, 実際: %d", rr.Code)
	}
	if reinjectAttempted {
		t.Error("クレームに負けた側が配送処理を開始してはいけない")
	}
}

func TestApprovalHandleGet_Viewer_MailboxAdmin_Allowed(t *testing.T) {
	apr := sampleMailboxApprovalRequest("apr-mb-3", "msg-mb-3", "sales@internal.test")
	repo := &mockRepository{
		getApprovalRequestFunc: func(_ context.Context, _ string) (*domain.ApprovalRequest, error) {
			return &apr, nil
		},
		isMailboxApproverFunc: func(_ context.Context, _, _ string) (bool, error) {
			return true, nil
		},
		getMessageFunc: func(_ context.Context, id string) (*domain.MessageDetail, error) {
			msg := sampleApprovalMessage(id)
			return &domain.MessageDetail{Message: msg}, nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodGet, "/api/v1/approvals/apr-mb-3", "id", "apr-mb-3", viewerSession("viewer-admin"))
	rr := httptest.NewRecorder()
	h.HandleGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}
}

// ─── 未使用 import 防止 ──────────────────────────────────────────────────────

var _ = strings.NewReader
