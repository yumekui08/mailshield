package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/delay"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/reinject"
)

func sampleDelayedRelease(id, from string) domain.DelayedRelease {
	now := time.Now().Truncate(time.Second)
	return domain.DelayedRelease{
		ID:          id,
		MessageID:   "msg-" + id,
		ReleaseAt:   now.Add(5 * time.Minute),
		Status:      domain.DelayedPending,
		FromAddress: from,
		ToAddresses: []string{"ext@example.com"},
		Subject:     "件名",
		CreatedAt:   now,
	}
}

// outboundPolicy は owner を可視・解放権限とする mailbox_policy。
func outboundPolicy() config.MailboxPolicyConfig {
	return config.MailboxPolicyConfig{
		OutboundQuarantine: config.DirectionPolicyConfig{
			VisibleTo: []string{"owner"},
			ReleaseBy: []string{"owner"},
		},
	}
}

func newDelayHandler(repo *mockRepository) *DelayHandler {
	svc := delay.New(repo, &mockEMLStorage{}, reinject.New("localhost", 1025))
	return NewDelayHandler(repo, svc, outboundPolicy(), testAuditLogger)
}

func TestDelayHandleList_Admin_ReturnsAll(t *testing.T) {
	var gotFilter *domain.MailboxVisibilityFilter
	repo := &mockRepository{
		listDelayedReleasesFunc: func(_ context.Context, filter *domain.MailboxVisibilityFilter) ([]domain.DelayedRelease, error) {
			gotFilter = filter
			return []domain.DelayedRelease{sampleDelayedRelease("d1", "me@corp.test")}, nil
		},
	}
	h := newDelayHandler(repo)
	req := requestWithSession(http.MethodGet, "/api/v1/delayed", adminSession())
	rr := httptest.NewRecorder()
	h.HandleList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if gotFilter != nil {
		t.Errorf("admin にはフィルターが渡らないべき: %+v", gotFilter)
	}
}

func TestDelayHandleList_Viewer_FilteredByOwner(t *testing.T) {
	var gotFilter *domain.MailboxVisibilityFilter
	repo := &mockRepository{
		getMailboxAddressesForUserFunc: func(_ context.Context, _ string, _ []domain.AssignmentRole) ([]string, error) {
			return []string{"me@corp.test"}, nil
		},
		listDelayedReleasesFunc: func(_ context.Context, filter *domain.MailboxVisibilityFilter) ([]domain.DelayedRelease, error) {
			gotFilter = filter
			return nil, nil
		},
	}
	h := newDelayHandler(repo)
	req := requestWithSession(http.MethodGet, "/api/v1/delayed", viewerSession("u1"))
	rr := httptest.NewRecorder()
	h.HandleList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if gotFilter == nil || len(gotFilter.OutboundMailboxes) != 1 || gotFilter.OutboundMailboxes[0] != "me@corp.test" {
		t.Errorf("viewer には owner フィルターが渡るべき: %+v", gotFilter)
	}
}

func TestDelayHandleCancel_Viewer_NotOwner_Forbidden(t *testing.T) {
	rel := sampleDelayedRelease("d2", "someoneelse@corp.test")
	repo := &mockRepository{
		getDelayedReleaseFunc: func(_ context.Context, _ string) (*domain.DelayedRelease, error) {
			return &rel, nil
		},
		getMailboxAddressesForUserFunc: func(_ context.Context, _ string, _ []domain.AssignmentRole) ([]string, error) {
			return []string{"myown@corp.test"}, nil // rel の from とは一致しない
		},
	}
	h := newDelayHandler(repo)
	req := requestWithSessionAndURLParam(http.MethodPost, "/api/v1/delayed/d2/cancel", "id", "d2", viewerSession("u1"))
	rr := httptest.NewRecorder()
	h.HandleCancel(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403（owner でない）", rr.Code)
	}
}

func TestDelayHandleCancel_Viewer_Owner_Success(t *testing.T) {
	rel := sampleDelayedRelease("d3", "me@corp.test")
	var claimedStatus domain.DelayedReleaseStatus
	repo := &mockRepository{
		getDelayedReleaseFunc: func(_ context.Context, _ string) (*domain.DelayedRelease, error) {
			return &rel, nil
		},
		getMailboxAddressesForUserFunc: func(_ context.Context, _ string, _ []domain.AssignmentRole) ([]string, error) {
			return []string{"me@corp.test"}, nil
		},
		claimDelayedReleaseFunc: func(_ context.Context, _ string, status domain.DelayedReleaseStatus, _ *string) (bool, error) {
			claimedStatus = status
			return true, nil
		},
		updateMessageStatusFunc: func(_ context.Context, _ string, _ domain.MessageStatus) error { return nil },
	}
	h := newDelayHandler(repo)
	req := requestWithSessionAndURLParam(http.MethodPost, "/api/v1/delayed/d3/cancel", "id", "d3", viewerSession("u1"))
	rr := httptest.NewRecorder()
	h.HandleCancel(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	if claimedStatus != domain.DelayedCancelled {
		t.Errorf("claim status = %q, want cancelled", claimedStatus)
	}
}

func TestDelayHandleCancel_AlreadyProcessed_Conflict(t *testing.T) {
	rel := sampleDelayedRelease("d4", "me@corp.test")
	rel.Status = domain.DelayedReleased // 既に処理済み
	repo := &mockRepository{
		getDelayedReleaseFunc: func(_ context.Context, _ string) (*domain.DelayedRelease, error) {
			return &rel, nil
		},
	}
	h := newDelayHandler(repo)
	req := requestWithSessionAndURLParam(http.MethodPost, "/api/v1/delayed/d4/cancel", "id", "d4", adminSession())
	rr := httptest.NewRecorder()
	h.HandleCancel(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rr.Code)
	}
}
