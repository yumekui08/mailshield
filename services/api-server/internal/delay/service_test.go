package delay

import (
	"context"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/reinject"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// stubRepo は Service が使う repository.Repository メソッドのみ実装するスタブ。
// テスト対象外のメソッドは呼ばれない前提で埋め込みなしの最小実装にする。
type stubRepo struct {
	repository.Repository
	getDelayed   *domain.DelayedRelease
	claimResult  bool
	claimCalled  bool
	getEMLCalled bool
	rolledBack   bool
	msgStatusSet domain.MessageStatus
}

func (s *stubRepo) GetDelayedRelease(_ context.Context, _ string) (*domain.DelayedRelease, error) {
	return s.getDelayed, nil
}
func (s *stubRepo) ClaimDelayedRelease(_ context.Context, _ string, _ domain.DelayedReleaseStatus, _ *string) (bool, error) {
	s.claimCalled = true
	return s.claimResult, nil
}
func (s *stubRepo) UpdateDelayedReleaseStatus(_ context.Context, _ string, _ domain.DelayedReleaseStatus) error {
	s.rolledBack = true
	return nil
}
func (s *stubRepo) GetMessage(_ context.Context, id string) (*domain.MessageDetail, error) {
	return &domain.MessageDetail{Message: domain.Message{ID: id, EMLPath: "raw/x.eml", FromAddress: "a@b.test", ToAddresses: []string{"c@d.test"}}}, nil
}
func (s *stubRepo) UpdateMessageStatus(_ context.Context, _ string, status domain.MessageStatus) error {
	s.msgStatusSet = status
	return nil
}

// stubEML は GetEML の呼び出しを記録する。
type stubEML struct{ called *bool }

func (s stubEML) GetEML(_ context.Context, _ string) ([]byte, error) {
	*s.called = true
	return []byte("eml"), nil
}
func (s stubEML) GetPresignedURL(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}

func TestRelease_ClaimLost_DoesNotDeliver(t *testing.T) {
	rel := &domain.DelayedRelease{ID: "d1", MessageID: "m1", Status: domain.DelayedPending}
	repo := &stubRepo{getDelayed: rel, claimResult: false} // クレーム負け
	emlCalled := false
	svc := New(repo, stubEML{called: &emlCalled}, reinject.New("localhost", 1025))

	// クレームに負けた場合はエラーなし・配送せず（EML 取得もしない）
	if err := svc.Release(context.Background(), "d1", nil); err != nil {
		t.Fatalf("クレーム負けはエラーにしないべき: %v", err)
	}
	if !repo.claimCalled {
		t.Error("ClaimDelayedRelease が呼ばれていない")
	}
	if emlCalled {
		t.Error("クレームに負けたのに EML 取得・配送を試みている（二重配送のリスク）")
	}
}

func TestCancel_ClaimLost_ReturnsFalse(t *testing.T) {
	rel := &domain.DelayedRelease{ID: "d2", MessageID: "m2", Status: domain.DelayedPending}
	repo := &stubRepo{getDelayed: rel, claimResult: false}
	emlCalled := false
	svc := New(repo, stubEML{called: &emlCalled}, reinject.New("localhost", 1025))

	ok, err := svc.Cancel(context.Background(), "d2", "user-1")
	if err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if ok {
		t.Error("クレームに負けた場合は false を返すべき")
	}
}

func TestCancel_Success_SetsRejected(t *testing.T) {
	rel := &domain.DelayedRelease{ID: "d3", MessageID: "m3", Status: domain.DelayedPending}
	repo := &stubRepo{getDelayed: rel, claimResult: true}
	emlCalled := false
	svc := New(repo, stubEML{called: &emlCalled}, reinject.New("localhost", 1025))

	ok, err := svc.Cancel(context.Background(), "d3", "user-1")
	if err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if !ok {
		t.Fatal("取消は成功すべき")
	}
	if repo.msgStatusSet != domain.StatusRejected {
		t.Errorf("取消後のメールステータス = %q, want rejected", repo.msgStatusSet)
	}
}
