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
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
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
		ApproverID: approverID,
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

// ─── HandleGetUserApprover ────────────────────────────────────────────────────

func TestApprovalHandleGetUserApprover_Found(t *testing.T) {
	approverID := "approver-xyz"
	repo := &mockRepository{
		getUserFunc: func(_ context.Context, id string) (*repository.User, error) {
			return &repository.User{ID: id, Email: "u@example.com", ApproverID: &approverID}, nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodGet, "/api/v1/users/user-1/approver", "id", "user-1", adminSession())
	rr := httptest.NewRecorder()
	h.HandleGetUserApprover(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}
	var got struct {
		ApproverID *string `json:"approver_id"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("JSONデコード失敗: %v", err)
	}
	if got.ApproverID == nil || *got.ApproverID != approverID {
		t.Errorf("approver_id 期待: %s, 実際: %v", approverID, got.ApproverID)
	}
}

func TestApprovalHandleGetUserApprover_NotFound(t *testing.T) {
	repo := &mockRepository{
		getUserFunc: func(_ context.Context, _ string) (*repository.User, error) {
			return nil, nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)
	req := requestWithSessionAndURLParam(http.MethodGet, "/api/v1/users/nonexistent/approver", "id", "nonexistent", adminSession())
	rr := httptest.NewRecorder()
	h.HandleGetUserApprover(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("ステータスコード 期待: 404, 実際: %d", rr.Code)
	}
}

// ─── HandleSetUserApprover ───────────────────────────────────────────────────

func TestApprovalHandleSetUserApprover_Success(t *testing.T) {
	var capturedApproverID *string
	repo := &mockRepository{
		updateUserApproverFunc: func(_ context.Context, _ string, id *string) error {
			capturedApproverID = id
			return nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)

	bodyJSON := []byte(`{"approver_id":"new-approver-id"}`)
	req := requestWithSessionURLParamAndBody(http.MethodPut, "/api/v1/users/user-1/approver", "id", "user-1", bodyJSON, adminSession())
	rr := httptest.NewRecorder()
	h.HandleSetUserApprover(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("ステータスコード 期待: 204, 実際: %d", rr.Code)
	}
	if capturedApproverID == nil || *capturedApproverID != "new-approver-id" {
		t.Errorf("approver_id 期待: new-approver-id, 実際: %v", capturedApproverID)
	}
}

func TestApprovalHandleSetUserApprover_Clear(t *testing.T) {
	var capturedApproverID *string = func() *string { s := "dummy"; return &s }()
	repo := &mockRepository{
		updateUserApproverFunc: func(_ context.Context, _ string, id *string) error {
			capturedApproverID = id
			return nil
		},
	}
	h := NewApprovalHandler(repo, &mockEMLStorage{}, config.NotificationConfig{}, testAuditLogger)

	// approver_id: null で承認者を解除
	bodyJSON := []byte(`{"approver_id":null}`)
	req := requestWithSessionURLParamAndBody(http.MethodPut, "/api/v1/users/user-1/approver", "id", "user-1", bodyJSON, adminSession())
	rr := httptest.NewRecorder()
	h.HandleSetUserApprover(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("ステータスコード 期待: 204, 実際: %d", rr.Code)
	}
	if capturedApproverID != nil {
		t.Errorf("approver_id は nil であるべき, 実際: %v", *capturedApproverID)
	}
}

// ─── 未使用 import 防止 ──────────────────────────────────────────────────────

var _ = strings.NewReader
