package approval

import (
	"context"
	"testing"
	"time"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// ─── スタブ実装 ──────────────────────────────────────────────────────────────

type stubRepository struct {
	expireApprovalsFunc         func(ctx context.Context) ([]string, error)
	updateMessageStatusFunc     func(ctx context.Context, id string, status domain.MessageStatus) error
	listPendingUnnotifiedFunc   func(ctx context.Context) ([]domain.ApprovalRequest, error)
	listResultUnnotifiedFunc    func(ctx context.Context) ([]domain.ApprovalRequest, error)
	markNotificationSentFunc    func(ctx context.Context, id string) error
	markResultNotifiedFunc      func(ctx context.Context, id string) error
	getMessageFunc              func(ctx context.Context, id string) (*domain.MessageDetail, error)
	getUserFunc                 func(ctx context.Context, id string) (*repository.User, error)
	findUserByEmailInternalFunc func(ctx context.Context, email string) (*repository.User, error)
}

func (s *stubRepository) ExpireApprovals(ctx context.Context) ([]string, error) {
	if s.expireApprovalsFunc != nil {
		return s.expireApprovalsFunc(ctx)
	}
	return nil, nil
}
func (s *stubRepository) UpdateMessageStatus(ctx context.Context, id string, status domain.MessageStatus) error {
	if s.updateMessageStatusFunc != nil {
		return s.updateMessageStatusFunc(ctx, id, status)
	}
	return nil
}
func (s *stubRepository) ListPendingUnnotified(ctx context.Context) ([]domain.ApprovalRequest, error) {
	if s.listPendingUnnotifiedFunc != nil {
		return s.listPendingUnnotifiedFunc(ctx)
	}
	return nil, nil
}
func (s *stubRepository) ListResultUnnotified(ctx context.Context) ([]domain.ApprovalRequest, error) {
	if s.listResultUnnotifiedFunc != nil {
		return s.listResultUnnotifiedFunc(ctx)
	}
	return nil, nil
}
func (s *stubRepository) MarkApprovalNotificationSent(ctx context.Context, id string) error {
	if s.markNotificationSentFunc != nil {
		return s.markNotificationSentFunc(ctx, id)
	}
	return nil
}
func (s *stubRepository) MarkApprovalResultNotified(ctx context.Context, id string) error {
	if s.markResultNotifiedFunc != nil {
		return s.markResultNotifiedFunc(ctx, id)
	}
	return nil
}
func (s *stubRepository) GetMessage(ctx context.Context, id string) (*domain.MessageDetail, error) {
	if s.getMessageFunc != nil {
		return s.getMessageFunc(ctx, id)
	}
	return nil, nil
}
func (s *stubRepository) GetUser(ctx context.Context, id string) (*repository.User, error) {
	if s.getUserFunc != nil {
		return s.getUserFunc(ctx, id)
	}
	return nil, nil
}
func (s *stubRepository) FindUserByEmailInternal(ctx context.Context, email string) (*repository.User, error) {
	if s.findUserByEmailInternalFunc != nil {
		return s.findUserByEmailInternalFunc(ctx, email)
	}
	return nil, nil
}

// serviceRepository は approval.Service が必要とするメソッドのみを実装する
// 内部インターフェースに適合させるため、repository.Repository の残りのメソッドは空スタブ
type serviceRepository struct {
	stubRepository
}

// approval.Service が使う repository.Repository を満たすための残りのスタブ群
// （テスト対象のメソッド以外は呼ばれないが、インターフェース実装として必要）
func (s *serviceRepository) ListMessages(_ context.Context, _ domain.ListQuery) ([]domain.Message, int, error) {
	return nil, 0, nil
}
func (s *serviceRepository) GetMessage(ctx context.Context, id string) (*domain.MessageDetail, error) {
	return s.stubRepository.GetMessage(ctx, id)
}
func (s *serviceRepository) ListQuarantine(_ context.Context, _ domain.ListQuery) ([]domain.Message, int, error) {
	return nil, 0, nil
}
func (s *serviceRepository) GetQuarantine(_ context.Context, _ string) (*domain.MessageDetail, error) {
	return nil, nil
}
func (s *serviceRepository) UpdateMessageStatus(ctx context.Context, id string, status domain.MessageStatus) error {
	return s.stubRepository.UpdateMessageStatus(ctx, id, status)
}
func (s *serviceRepository) BulkUpdateMessageStatus(_ context.Context, _ []string, _ domain.MessageStatus) error {
	return nil
}
func (s *serviceRepository) FindUserByEmail(_ context.Context, _ string) (*repository.User, error) {
	return nil, nil
}
func (s *serviceRepository) CreateUser(_ context.Context, _ *repository.User) error { return nil }
func (s *serviceRepository) UpsertFederatedUser(_ context.Context, _, _ string, _ domain.Role, _ domain.ProvisionedBy) (*repository.User, error) {
	return nil, nil
}
func (s *serviceRepository) DeactivateMissingLDAPUsers(_ context.Context, _ []string) (int, error) {
	return 0, nil
}
func (s *serviceRepository) CountUsers(_ context.Context) (int, error)               { return 0, nil }
func (s *serviceRepository) ListUsers(_ context.Context) ([]repository.User, error)  { return nil, nil }
func (s *serviceRepository) UpdateUserPassword(_ context.Context, _, _ string) error { return nil }
func (s *serviceRepository) UpdateUserRole(_ context.Context, _ string, _ domain.Role) error {
	return nil
}
func (s *serviceRepository) DeleteUser(_ context.Context, _ string) error { return nil }
func (s *serviceRepository) CreateMailbox(_ context.Context, _ *repository.Mailbox) error {
	return nil
}
func (s *serviceRepository) ListMailboxes(_ context.Context) ([]repository.Mailbox, error) {
	return nil, nil
}
func (s *serviceRepository) GetMailbox(_ context.Context, _ string) (*repository.Mailbox, error) {
	return nil, nil
}
func (s *serviceRepository) UpdateMailbox(_ context.Context, _, _ string, _ bool) error { return nil }
func (s *serviceRepository) DeleteMailbox(_ context.Context, _ string) error            { return nil }
func (s *serviceRepository) ListAssignments(_ context.Context, _ string) ([]repository.MailboxAssignment, error) {
	return nil, nil
}
func (s *serviceRepository) AddAssignment(_ context.Context, _ *repository.MailboxAssignment) error {
	return nil
}
func (s *serviceRepository) RemoveAssignment(_ context.Context, _, _ string, _ domain.AssignmentRole) error {
	return nil
}
func (s *serviceRepository) GetMailboxAddressesForUser(_ context.Context, _ string, _ []domain.AssignmentRole) ([]string, error) {
	return nil, nil
}
func (s *serviceRepository) GetStats(_ context.Context, _ *domain.MailboxVisibilityFilter) (*domain.Stats, error) {
	return nil, nil
}
func (s *serviceRepository) ListAttachmentsByMessage(_ context.Context, _ string) ([]domain.Attachment, error) {
	return nil, nil
}
func (s *serviceRepository) ListAttachmentsByToken(_ context.Context, _ string) ([]domain.Attachment, error) {
	return nil, nil
}
func (s *serviceRepository) GetAttachmentByToken(_ context.Context, _, _ string) (*domain.Attachment, error) {
	return nil, nil
}
func (s *serviceRepository) ListAttachmentsByTokenPublic(_ context.Context, _ string) ([]domain.Attachment, error) {
	return nil, nil
}
func (s *serviceRepository) GetAttachmentByTokenPublic(_ context.Context, _, _ string) (*domain.Attachment, error) {
	return nil, nil
}
func (s *serviceRepository) GetAttachmentToAddressesByToken(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (s *serviceRepository) DisableAttachment(_ context.Context, _ string, _ bool) error { return nil }
func (s *serviceRepository) DeleteAttachment(_ context.Context, _ string) error          { return nil }
func (s *serviceRepository) CreateAuditLog(_ context.Context, _ *domain.AuditLog) error  { return nil }
func (s *serviceRepository) ListAuditLogs(_ context.Context, _ domain.AuditLogQuery) ([]domain.AuditLog, int, error) {
	return nil, 0, nil
}
func (s *serviceRepository) CreateAPIKey(_ context.Context, _ *domain.APIKey, _ string) error {
	return nil
}
func (s *serviceRepository) ListAPIKeys(_ context.Context) ([]domain.APIKey, error) { return nil, nil }
func (s *serviceRepository) FindAPIKeyByHash(_ context.Context, _ string) (*domain.APIKey, error) {
	return nil, nil
}
func (s *serviceRepository) RevokeAPIKey(_ context.Context, _ string) error         { return nil }
func (s *serviceRepository) UpdateAPIKeyLastUsed(_ context.Context, _ string) error { return nil }
func (s *serviceRepository) ListApprovalRequests(_ context.Context, _ string) ([]domain.ApprovalRequest, error) {
	return nil, nil
}
func (s *serviceRepository) ListAllApprovalRequests(_ context.Context) ([]domain.ApprovalRequest, error) {
	return nil, nil
}
func (s *serviceRepository) GetApprovalRequest(_ context.Context, _ string) (*domain.ApprovalRequest, error) {
	return nil, nil
}
func (s *serviceRepository) UpdateApprovalStatus(_ context.Context, _ string, _ domain.ApprovalStatus, _ *string) error {
	return nil
}
func (s *serviceRepository) MarkApprovalNotificationSent(ctx context.Context, id string) error {
	return s.stubRepository.MarkApprovalNotificationSent(ctx, id)
}
func (s *serviceRepository) MarkApprovalResultNotified(ctx context.Context, id string) error {
	return s.stubRepository.MarkApprovalResultNotified(ctx, id)
}
func (s *serviceRepository) ListPendingUnnotified(ctx context.Context) ([]domain.ApprovalRequest, error) {
	return s.stubRepository.ListPendingUnnotified(ctx)
}
func (s *serviceRepository) ListResultUnnotified(ctx context.Context) ([]domain.ApprovalRequest, error) {
	return s.stubRepository.ListResultUnnotified(ctx)
}
func (s *serviceRepository) ExpireApprovals(ctx context.Context) ([]string, error) {
	return s.stubRepository.ExpireApprovals(ctx)
}
func (s *serviceRepository) GetUser(ctx context.Context, id string) (*repository.User, error) {
	return s.stubRepository.GetUser(ctx, id)
}
func (s *serviceRepository) UpdateUserApprover(_ context.Context, _ string, _ *string) error {
	return nil
}
func (s *serviceRepository) FindUserByEmailInternal(ctx context.Context, email string) (*repository.User, error) {
	return s.stubRepository.FindUserByEmailInternal(ctx, email)
}

// ─── expireApprovals ─────────────────────────────────────────────────────────

func TestExpireApprovals_UpdatesMessageStatus(t *testing.T) {
	expiredMessageIDs := []string{"msg-expired-1", "msg-expired-2"}

	var updatedMessageIDs []string
	repo := &serviceRepository{
		stubRepository: stubRepository{
			expireApprovalsFunc: func(_ context.Context) ([]string, error) {
				return expiredMessageIDs, nil
			},
			updateMessageStatusFunc: func(_ context.Context, id string, status domain.MessageStatus) error {
				if status != domain.StatusExpired {
					t.Errorf("ステータス 期待: expired, 実際: %s", status)
				}
				updatedMessageIDs = append(updatedMessageIDs, id)
				return nil
			},
		},
	}

	svc := New(repo, config.ApprovalConfig{}, config.NotificationConfig{})
	svc.expireApprovals(context.Background())

	if len(updatedMessageIDs) != 2 {
		t.Errorf("更新件数 期待: 2, 実際: %d", len(updatedMessageIDs))
	}
	for i, id := range expiredMessageIDs {
		if updatedMessageIDs[i] != id {
			t.Errorf("[%d] message_id 期待: %s, 実際: %s", i, id, updatedMessageIDs[i])
		}
	}
}

func TestExpireApprovals_NoExpired_DoesNothing(t *testing.T) {
	callCount := 0
	repo := &serviceRepository{
		stubRepository: stubRepository{
			expireApprovalsFunc: func(_ context.Context) ([]string, error) {
				return nil, nil
			},
			updateMessageStatusFunc: func(_ context.Context, _ string, _ domain.MessageStatus) error {
				callCount++
				return nil
			},
		},
	}

	svc := New(repo, config.ApprovalConfig{}, config.NotificationConfig{})
	svc.expireApprovals(context.Background())

	if callCount != 0 {
		t.Errorf("UpdateMessageStatus は呼ばれてはいけない, 実際の呼び出し回数: %d", callCount)
	}
}

// ─── sendPendingNotifications ────────────────────────────────────────────────

func TestSendPendingNotifications_Disabled_SkipsAll(t *testing.T) {
	callCount := 0
	repo := &serviceRepository{
		stubRepository: stubRepository{
			listPendingUnnotifiedFunc: func(_ context.Context) ([]domain.ApprovalRequest, error) {
				callCount++
				return nil, nil
			},
		},
	}

	// RequestEnabled=false のとき何も起きない
	svc := New(repo, config.ApprovalConfig{Notification: config.ApprovalNotificationConfig{RequestEnabled: false}}, config.NotificationConfig{})
	svc.sendPendingNotifications(context.Background())

	if callCount != 0 {
		t.Errorf("RequestEnabled=false のとき ListPendingUnnotified は呼ばれてはいけない")
	}
}

// ─── sendResultNotifications ─────────────────────────────────────────────────

func TestSendResultNotifications_Disabled_SkipsAll(t *testing.T) {
	callCount := 0
	repo := &serviceRepository{
		stubRepository: stubRepository{
			listResultUnnotifiedFunc: func(_ context.Context) ([]domain.ApprovalRequest, error) {
				callCount++
				return nil, nil
			},
		},
	}

	svc := New(repo, config.ApprovalConfig{Notification: config.ApprovalNotificationConfig{ResultEnabled: false}}, config.NotificationConfig{})
	svc.sendResultNotifications(context.Background())

	if callCount != 0 {
		t.Errorf("ResultEnabled=false のとき ListResultUnnotified は呼ばれてはいけない")
	}
}

// ─── renderTemplate ──────────────────────────────────────────────────────────

func TestRenderTemplate_SubstitutesVariables(t *testing.T) {
	tmpl := "件名: {{.Subject}} / 送信元: {{.FromAddress}}"
	data := templateData{
		Subject:     "テストメール",
		FromAddress: "sender@example.com",
	}

	result, err := renderTemplate(tmpl, data)
	if err != nil {
		t.Fatalf("テンプレート描画失敗: %v", err)
	}
	expected := "件名: テストメール / 送信元: sender@example.com"
	if result != expected {
		t.Errorf("期待: %q, 実際: %q", expected, result)
	}
}

func TestRenderTemplate_EmptyTemplate_ReturnsEmpty(t *testing.T) {
	result, err := renderTemplate("", templateData{})
	if err != nil {
		t.Fatalf("空テンプレートでエラー: %v", err)
	}
	if result != "" {
		t.Errorf("期待: 空文字, 実際: %q", result)
	}
}

func TestRenderTemplate_ExpiresAtFormatted(t *testing.T) {
	expiresAt := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	tmpl := "期限: {{.ExpiresAt}}"
	data := templateData{ExpiresAt: expiresAt.Format("2006-01-02 15:04:05")}

	result, err := renderTemplate(tmpl, data)
	if err != nil {
		t.Fatalf("テンプレート描画失敗: %v", err)
	}
	if result != "期限: 2026-12-31 23:59:59" {
		t.Errorf("期待: 期限: 2026-12-31 23:59:59, 実際: %q", result)
	}
}
