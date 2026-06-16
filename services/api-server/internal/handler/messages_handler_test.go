package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
)

// mockEMLStorage は storage.EMLStorage のテスト用モックである。
type mockEMLStorage struct {
	getPresignedURLFunc func(ctx context.Context, path string, expiryHours int) (string, error)
	getEMLFunc         func(ctx context.Context, path string) ([]byte, error)
}

func (m *mockEMLStorage) GetPresignedURL(ctx context.Context, path string, expiryHours int) (string, error) {
	if m.getPresignedURLFunc != nil {
		return m.getPresignedURLFunc(ctx, path, expiryHours)
	}
	return "http://minio:9000/presigned/" + path, nil
}

func (m *mockEMLStorage) GetEML(ctx context.Context, path string) ([]byte, error) {
	if m.getEMLFunc != nil {
		return m.getEMLFunc(ctx, path)
	}
	return nil, nil
}

func adminSession() *domain.Session {
	return &domain.Session{
		ID:        "session-admin",
		Role:      domain.RoleAdmin,
		User:      domain.UserClaims{Sub: "tenant-abc", Email: "admin@example.com"},
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
}

func requestWithSession(method, target string, session *domain.Session) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	ctx := middleware.WithSession(req.Context(), session)
	return req.WithContext(ctx)
}

func requestWithSessionAndURLParam(method, target, paramKey, paramValue string, session *domain.Session) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(paramKey, paramValue)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = middleware.WithSession(ctx, session)
	return req.WithContext(ctx)
}

func sampleMessage(id string) domain.Message {
	now := time.Now().Truncate(time.Second)
	return domain.Message{
		ID:          id,
		EMLPath:     "/raw/" + id + ".eml",
		FromAddress: "sender@example.com",
		ToAddresses: []string{"to@example.com"},
		Subject:     "Test Subject",
		SizeBytes:   1024,
		Status:      domain.StatusDelivered,
		ReceivedAt:  now,
		UpdatedAt:   now,
	}
}

func TestHandleList_Success(t *testing.T) {
	messages := []domain.Message{sampleMessage("msg-1"), sampleMessage("msg-2")}
	repo := &mockRepository{
		listMessagesFunc: func(ctx context.Context, q domain.ListQuery) ([]domain.Message, int, error) {
			return messages, 2, nil
		},
	}

	h := NewMessagesHandler(repo, &mockEMLStorage{}, 1)
	req := requestWithSession(http.MethodGet, "/api/v1/messages", adminSession())
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
	if result.Data[0].ID != "msg-1" {
		t.Errorf("Data[0].ID 期待: msg-1, 実際: %s", result.Data[0].ID)
	}
}

func TestHandleList_RepoError(t *testing.T) {
	repo := &mockRepository{
		listMessagesFunc: func(ctx context.Context, q domain.ListQuery) ([]domain.Message, int, error) {
			return nil, 0, errors.New("DB接続失敗")
		},
	}

	h := NewMessagesHandler(repo, &mockEMLStorage{}, 1)
	req := requestWithSession(http.MethodGet, "/api/v1/messages", adminSession())
	rr := httptest.NewRecorder()

	h.HandleList(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("ステータスコード 期待: 500, 実際: %d", rr.Code)
	}
}

func TestHandleGet_Success(t *testing.T) {
	msg := sampleMessage("msg-detail-1")
	detail := &domain.MessageDetail{
		Message:        msg,
		InspectResults: []domain.InspectResult{},
	}
	repo := &mockRepository{
		getMessageFunc: func(ctx context.Context, id string) (*domain.MessageDetail, error) {
			return detail, nil
		},
	}

	h := NewMessagesHandler(repo, &mockEMLStorage{}, 1)
	req := requestWithSessionAndURLParam(http.MethodGet, "/api/v1/messages/msg-detail-1", "id", "msg-detail-1", adminSession())
	rr := httptest.NewRecorder()

	h.HandleGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}

	var got domain.MessageDetail
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("レスポンスJSONデコード失敗: %v", err)
	}
	if got.ID != "msg-detail-1" {
		t.Errorf("ID 期待: msg-detail-1, 実際: %s", got.ID)
	}
}

func TestHandleGet_NotFound(t *testing.T) {
	repo := &mockRepository{
		getMessageFunc: func(ctx context.Context, id string) (*domain.MessageDetail, error) {
			return nil, errors.New("メッセージが見つかりません")
		},
	}

	h := NewMessagesHandler(repo, &mockEMLStorage{}, 1)
	req := requestWithSessionAndURLParam(http.MethodGet, "/api/v1/messages/nonexistent", "id", "nonexistent", adminSession())
	rr := httptest.NewRecorder()

	h.HandleGet(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("ステータスコード 期待: 404, 実際: %d", rr.Code)
	}
}

func TestHandleGetEML_Success(t *testing.T) {
	msg := sampleMessage("eml-msg-1")
	detail := &domain.MessageDetail{
		Message:        msg,
		InspectResults: []domain.InspectResult{},
	}
	repo := &mockRepository{
		getMessageFunc: func(ctx context.Context, id string) (*domain.MessageDetail, error) {
			return detail, nil
		},
	}
	stor := &mockEMLStorage{
		getPresignedURLFunc: func(ctx context.Context, path string, expiryHours int) (string, error) {
			return "http://minio:9000/presigned/" + path, nil
		},
	}

	h := NewMessagesHandler(repo, stor, 1)
	req := requestWithSessionAndURLParam(http.MethodGet, "/api/v1/messages/eml-msg-1/eml", "id", "eml-msg-1", adminSession())
	rr := httptest.NewRecorder()

	h.HandleGetEML(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("ステータスコード 期待: 200, 実際: %d", rr.Code)
	}

	var got map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("レスポンスJSONデコード失敗: %v", err)
	}
	url, ok := got["url"].(string)
	if !ok || url == "" {
		t.Errorf("url フィールドが空または存在しません: %v", got)
	}
}
