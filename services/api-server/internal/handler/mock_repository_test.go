package handler

import (
	"context"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

type mockRepository struct {
	getStatsFunc                      func(ctx context.Context, filter *domain.MailboxVisibilityFilter) (*domain.Stats, error)
	getStatsTimeseriesFunc            func(ctx context.Context, days int, filter *domain.MailboxVisibilityFilter) ([]domain.StatsTimeseriesPoint, error)
	listMessagesFunc                  func(ctx context.Context, q domain.ListQuery) ([]domain.Message, int, error)
	getMessageFunc                    func(ctx context.Context, id string) (*domain.MessageDetail, error)
	listQuarantineFunc                func(ctx context.Context, q domain.ListQuery) ([]domain.Message, int, error)
	getQuarantineFunc                 func(ctx context.Context, id string) (*domain.MessageDetail, error)
	updateMessageStatusFunc           func(ctx context.Context, id string, status domain.MessageStatus) error
	findUserByEmailFunc               func(ctx context.Context, email string) (*repository.User, error)
	createUserFunc                    func(ctx context.Context, user *repository.User) error
	upsertFederatedUserFunc           func(ctx context.Context, email, displayName string, role domain.Role, source domain.ProvisionedBy) (*repository.User, error)
	deactivateMissingLDAPUsersFunc    func(ctx context.Context, presentEmails []string) (int, error)
	countUsersFunc                    func(ctx context.Context) (int, error)
	listUsersFunc                     func(ctx context.Context) ([]repository.User, error)
	updateUserPasswordFunc            func(ctx context.Context, userID, passwordHash string) error
	updateUserRoleFunc                func(ctx context.Context, userID string, role domain.Role) error
	deleteUserFunc                    func(ctx context.Context, userID string) error
	createMailboxFunc                 func(ctx context.Context, mailbox *repository.Mailbox) error
	listMailboxesFunc                 func(ctx context.Context) ([]repository.Mailbox, error)
	searchMailboxesFunc               func(ctx context.Context, filter repository.MailboxSearchFilter) ([]repository.Mailbox, int, error)
	getMailboxFunc                    func(ctx context.Context, id string) (*repository.Mailbox, error)
	updateMailboxFunc                 func(ctx context.Context, id, displayName string, isActive bool) error
	deleteMailboxFunc                 func(ctx context.Context, id string) error
	listAssignmentsFunc               func(ctx context.Context, mailboxID string) ([]repository.MailboxAssignment, error)
	addAssignmentFunc                 func(ctx context.Context, assignment *repository.MailboxAssignment) error
	removeAssignmentFunc              func(ctx context.Context, mailboxID, userID string, role domain.AssignmentRole) error
	syncMailboxAssignmentsForUserFunc func(ctx context.Context, userID string, source domain.ProvisionedBy, desired []repository.MailboxAssignmentRequest) error
	getMailboxAddressesForUserFunc    func(ctx context.Context, userID string, roles []domain.AssignmentRole) ([]string, error)

	// 承認フロー
	listApprovalRequestsFunc      func(ctx context.Context, approverID string) ([]domain.ApprovalRequest, error)
	isMailboxApproverFunc         func(ctx context.Context, userID, mailboxEmail string) (bool, error)
	listMailboxApproverEmailsFunc func(ctx context.Context, mailboxEmail string) ([]string, error)
	claimApprovalRequestFunc      func(ctx context.Context, id string, status domain.ApprovalStatus, comment *string) (bool, error)
	listAllApprovalRequestsFunc   func(ctx context.Context) ([]domain.ApprovalRequest, error)
	searchApprovalRequestsFunc    func(ctx context.Context, filter repository.ApprovalSearchFilter) ([]domain.ApprovalRequestListItem, int, error)
	getApprovalRequestFunc        func(ctx context.Context, id string) (*domain.ApprovalRequest, error)
	updateApprovalStatusFunc      func(ctx context.Context, id string, status domain.ApprovalStatus, comment *string) error
	getUserFunc                   func(ctx context.Context, id string) (*repository.User, error)

	// 送信ディレイ
	listDelayedReleasesFunc func(ctx context.Context, filter *domain.MailboxVisibilityFilter) ([]domain.DelayedRelease, error)
	getDelayedReleaseFunc   func(ctx context.Context, id string) (*domain.DelayedRelease, error)
	claimDelayedReleaseFunc func(ctx context.Context, id string, status domain.DelayedReleaseStatus, decidedBy *string) (bool, error)
}

func (m *mockRepository) ListMessages(ctx context.Context, q domain.ListQuery) ([]domain.Message, int, error) {
	return m.listMessagesFunc(ctx, q)
}

func (m *mockRepository) GetMessage(ctx context.Context, id string) (*domain.MessageDetail, error) {
	return m.getMessageFunc(ctx, id)
}

func (m *mockRepository) ListQuarantine(ctx context.Context, q domain.ListQuery) ([]domain.Message, int, error) {
	return m.listQuarantineFunc(ctx, q)
}

func (m *mockRepository) GetQuarantine(ctx context.Context, id string) (*domain.MessageDetail, error) {
	return m.getQuarantineFunc(ctx, id)
}

func (m *mockRepository) UpdateMessageStatus(ctx context.Context, id string, status domain.MessageStatus) error {
	return m.updateMessageStatusFunc(ctx, id, status)
}

func (m *mockRepository) FindUserByEmail(ctx context.Context, email string) (*repository.User, error) {
	if m.findUserByEmailFunc != nil {
		return m.findUserByEmailFunc(ctx, email)
	}
	return nil, nil
}

func (m *mockRepository) CreateUser(ctx context.Context, user *repository.User) error {
	if m.createUserFunc != nil {
		return m.createUserFunc(ctx, user)
	}
	return nil
}

func (m *mockRepository) UpsertFederatedUser(ctx context.Context, email, displayName string, role domain.Role, source domain.ProvisionedBy) (*repository.User, error) {
	if m.upsertFederatedUserFunc != nil {
		return m.upsertFederatedUserFunc(ctx, email, displayName, role, source)
	}
	return &repository.User{ID: "mock-user-id", Email: email, DisplayName: displayName, Role: role, IsActive: true, ProvisionedBy: source}, nil
}

func (m *mockRepository) DeactivateMissingLDAPUsers(ctx context.Context, presentEmails []string) (int, error) {
	if m.deactivateMissingLDAPUsersFunc != nil {
		return m.deactivateMissingLDAPUsersFunc(ctx, presentEmails)
	}
	return 0, nil
}

func (m *mockRepository) CountUsers(ctx context.Context) (int, error) {
	if m.countUsersFunc != nil {
		return m.countUsersFunc(ctx)
	}
	return 0, nil
}

func (m *mockRepository) ListUsers(ctx context.Context) ([]repository.User, error) {
	if m.listUsersFunc != nil {
		return m.listUsersFunc(ctx)
	}
	return nil, nil
}
func (m *mockRepository) SearchUsers(_ context.Context, _ string, _ int) ([]repository.User, error) {
	return nil, nil
}

func (m *mockRepository) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error {
	if m.updateUserPasswordFunc != nil {
		return m.updateUserPasswordFunc(ctx, userID, passwordHash)
	}
	return nil
}

func (m *mockRepository) UpdateUserRole(ctx context.Context, userID string, role domain.Role) error {
	if m.updateUserRoleFunc != nil {
		return m.updateUserRoleFunc(ctx, userID, role)
	}
	return nil
}

func (m *mockRepository) DeleteUser(ctx context.Context, userID string) error {
	if m.deleteUserFunc != nil {
		return m.deleteUserFunc(ctx, userID)
	}
	return nil
}

func (m *mockRepository) CreateMailbox(ctx context.Context, mailbox *repository.Mailbox) error {
	if m.createMailboxFunc != nil {
		return m.createMailboxFunc(ctx, mailbox)
	}
	return nil
}

func (m *mockRepository) ListMailboxes(ctx context.Context) ([]repository.Mailbox, error) {
	if m.listMailboxesFunc != nil {
		return m.listMailboxesFunc(ctx)
	}
	return []repository.Mailbox{}, nil
}

func (m *mockRepository) SearchMailboxes(ctx context.Context, filter repository.MailboxSearchFilter) ([]repository.Mailbox, int, error) {
	if m.searchMailboxesFunc != nil {
		return m.searchMailboxesFunc(ctx, filter)
	}
	return []repository.Mailbox{}, 0, nil
}

func (m *mockRepository) GetMailbox(ctx context.Context, id string) (*repository.Mailbox, error) {
	if m.getMailboxFunc != nil {
		return m.getMailboxFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockRepository) UpdateMailbox(ctx context.Context, id, displayName string, isActive bool) error {
	if m.updateMailboxFunc != nil {
		return m.updateMailboxFunc(ctx, id, displayName, isActive)
	}
	return nil
}

func (m *mockRepository) DeleteMailbox(ctx context.Context, id string) error {
	if m.deleteMailboxFunc != nil {
		return m.deleteMailboxFunc(ctx, id)
	}
	return nil
}

func (m *mockRepository) ListAssignments(ctx context.Context, mailboxID string) ([]repository.MailboxAssignment, error) {
	if m.listAssignmentsFunc != nil {
		return m.listAssignmentsFunc(ctx, mailboxID)
	}
	return []repository.MailboxAssignment{}, nil
}
func (m *mockRepository) ListAssignmentSummaries(_ context.Context, _ int) (map[string][]repository.MailboxRoleSummary, error) {
	return map[string][]repository.MailboxRoleSummary{}, nil
}

func (m *mockRepository) AddAssignment(ctx context.Context, assignment *repository.MailboxAssignment) error {
	if m.addAssignmentFunc != nil {
		return m.addAssignmentFunc(ctx, assignment)
	}
	return nil
}

func (m *mockRepository) RemoveAssignment(ctx context.Context, mailboxID, userID string, role domain.AssignmentRole) error {
	if m.removeAssignmentFunc != nil {
		return m.removeAssignmentFunc(ctx, mailboxID, userID, role)
	}
	return nil
}

func (m *mockRepository) SyncMailboxAssignmentsForUser(ctx context.Context, userID string, source domain.ProvisionedBy, desired []repository.MailboxAssignmentRequest) error {
	if m.syncMailboxAssignmentsForUserFunc != nil {
		return m.syncMailboxAssignmentsForUserFunc(ctx, userID, source, desired)
	}
	return nil
}

func (m *mockRepository) GetMailboxAddressesForUser(ctx context.Context, userID string, roles []domain.AssignmentRole) ([]string, error) {
	if m.getMailboxAddressesForUserFunc != nil {
		return m.getMailboxAddressesForUserFunc(ctx, userID, roles)
	}
	return []string{}, nil
}

func (m *mockRepository) GetStats(ctx context.Context, filter *domain.MailboxVisibilityFilter) (*domain.Stats, error) {
	if m.getStatsFunc != nil {
		return m.getStatsFunc(ctx, filter)
	}
	return &domain.Stats{}, nil
}

func (m *mockRepository) GetStatsTimeseries(ctx context.Context, days int, filter *domain.MailboxVisibilityFilter) ([]domain.StatsTimeseriesPoint, error) {
	if m.getStatsTimeseriesFunc != nil {
		return m.getStatsTimeseriesFunc(ctx, days, filter)
	}
	return []domain.StatsTimeseriesPoint{}, nil
}

func (m *mockRepository) ListAttachmentsByMessage(_ context.Context, _ string) ([]domain.Attachment, error) {
	return []domain.Attachment{}, nil
}

func (m *mockRepository) ListAttachmentsByToken(_ context.Context, _ string) ([]domain.Attachment, error) {
	return []domain.Attachment{}, nil
}

func (m *mockRepository) GetAttachmentByToken(_ context.Context, _, _ string) (*domain.Attachment, error) {
	return nil, nil
}

func (m *mockRepository) ListAttachmentsByTokenPublic(_ context.Context, _ string) ([]domain.Attachment, error) {
	return []domain.Attachment{}, nil
}

func (m *mockRepository) GetAttachmentByTokenPublic(_ context.Context, _, _ string) (*domain.Attachment, error) {
	return nil, nil
}

func (m *mockRepository) DisableAttachment(_ context.Context, _ string, _ bool) error {
	return nil
}

func (m *mockRepository) DeleteAttachment(_ context.Context, _ string) error {
	return nil
}

func (m *mockRepository) GetAttachmentToAddressesByToken(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (m *mockRepository) BulkUpdateMessageStatus(_ context.Context, _ []string, _ domain.MessageStatus) error {
	return nil
}

func (m *mockRepository) CreateAuditLog(_ context.Context, _ *domain.AuditLog) error {
	return nil
}

func (m *mockRepository) ListAuditLogs(_ context.Context, _ domain.AuditLogQuery) ([]domain.AuditLog, int, error) {
	return nil, 0, nil
}

func (m *mockRepository) SavePolicyVersion(_ context.Context, _ *domain.PolicyVersion) error {
	return nil
}

func (m *mockRepository) ListPolicyVersions(_ context.Context, _ string, _ int) ([]domain.PolicyVersion, error) {
	return nil, nil
}

func (m *mockRepository) GetPolicyVersion(_ context.Context, _ string) (*domain.PolicyVersion, error) {
	return nil, nil
}

func (m *mockRepository) CreateAPIKey(_ context.Context, _ *domain.APIKey, _ string) error {
	return nil
}

func (m *mockRepository) ListAPIKeys(_ context.Context) ([]domain.APIKey, error) {
	return nil, nil
}

func (m *mockRepository) FindAPIKeyByHash(_ context.Context, _ string) (*domain.APIKey, error) {
	return nil, nil
}

func (m *mockRepository) RevokeAPIKey(_ context.Context, _ string) error {
	return nil
}

func (m *mockRepository) UpdateAPIKeyLastUsed(_ context.Context, _ string) error {
	return nil
}

// ─── 承認フロー ──────────────────────────────────────────────────────────────

func (m *mockRepository) ListApprovalRequests(ctx context.Context, approverID string) ([]domain.ApprovalRequest, error) {
	if m.listApprovalRequestsFunc != nil {
		return m.listApprovalRequestsFunc(ctx, approverID)
	}
	return nil, nil
}
func (m *mockRepository) IsMailboxApprover(ctx context.Context, userID, mailboxEmail string) (bool, error) {
	if m.isMailboxApproverFunc != nil {
		return m.isMailboxApproverFunc(ctx, userID, mailboxEmail)
	}
	return false, nil
}
func (m *mockRepository) ListMailboxApproverEmails(ctx context.Context, mailboxEmail string) ([]string, error) {
	if m.listMailboxApproverEmailsFunc != nil {
		return m.listMailboxApproverEmailsFunc(ctx, mailboxEmail)
	}
	return nil, nil
}
func (m *mockRepository) EnsureApprovalNotifications(_ context.Context, _ string, _ []string) error {
	return nil
}
func (m *mockRepository) ListPendingNotificationRecipients(_ context.Context, _ string, _ int) ([]string, error) {
	return nil, nil
}
func (m *mockRepository) MarkApprovalNotificationResult(_ context.Context, _, _ string, _ bool, _ string) error {
	return nil
}
func (m *mockRepository) CountRemainingNotifications(_ context.Context, _ string, _ int) (int, error) {
	return 0, nil
}
func (m *mockRepository) ListAllApprovalRequests(ctx context.Context) ([]domain.ApprovalRequest, error) {
	if m.listAllApprovalRequestsFunc != nil {
		return m.listAllApprovalRequestsFunc(ctx)
	}
	return nil, nil
}
func (m *mockRepository) SearchApprovalRequests(ctx context.Context, filter repository.ApprovalSearchFilter) ([]domain.ApprovalRequestListItem, int, error) {
	if m.searchApprovalRequestsFunc != nil {
		return m.searchApprovalRequestsFunc(ctx, filter)
	}
	return nil, 0, nil
}
func (m *mockRepository) GetApprovalRequest(ctx context.Context, id string) (*domain.ApprovalRequest, error) {
	if m.getApprovalRequestFunc != nil {
		return m.getApprovalRequestFunc(ctx, id)
	}
	return nil, nil
}
func (m *mockRepository) UpdateApprovalStatus(ctx context.Context, id string, status domain.ApprovalStatus, comment *string) error {
	if m.updateApprovalStatusFunc != nil {
		return m.updateApprovalStatusFunc(ctx, id, status, comment)
	}
	return nil
}

// ClaimApprovalRequest はデフォルトで updateApprovalStatusFunc に委譲し true を返す
// （既存テストが「決定＝ステータス更新」の検証に updateApprovalStatusFunc を使うため）。
func (m *mockRepository) ClaimApprovalRequest(ctx context.Context, id string, status domain.ApprovalStatus, comment *string) (bool, error) {
	if m.claimApprovalRequestFunc != nil {
		return m.claimApprovalRequestFunc(ctx, id, status, comment)
	}
	if m.updateApprovalStatusFunc != nil {
		if err := m.updateApprovalStatusFunc(ctx, id, status, comment); err != nil {
			return false, err
		}
	}
	return true, nil
}
func (m *mockRepository) MarkApprovalNotificationSent(_ context.Context, _ string) error {
	return nil
}
func (m *mockRepository) MarkApprovalResultNotified(_ context.Context, _ string) error {
	return nil
}
func (m *mockRepository) ListPendingUnnotified(_ context.Context) ([]domain.ApprovalRequest, error) {
	return nil, nil
}
func (m *mockRepository) ListResultUnnotified(_ context.Context) ([]domain.ApprovalRequest, error) {
	return nil, nil
}
func (m *mockRepository) ExpireApprovals(_ context.Context) ([]string, error) {
	return nil, nil
}
func (m *mockRepository) GetUser(ctx context.Context, id string) (*repository.User, error) {
	if m.getUserFunc != nil {
		return m.getUserFunc(ctx, id)
	}
	return nil, nil
}
func (m *mockRepository) FindUserByEmailInternal(_ context.Context, _ string) (*repository.User, error) {
	return nil, nil
}
func (m *mockRepository) ListDelayedReleases(ctx context.Context, filter *domain.MailboxVisibilityFilter) ([]domain.DelayedRelease, error) {
	if m.listDelayedReleasesFunc != nil {
		return m.listDelayedReleasesFunc(ctx, filter)
	}
	return nil, nil
}
func (m *mockRepository) GetDelayedRelease(ctx context.Context, id string) (*domain.DelayedRelease, error) {
	if m.getDelayedReleaseFunc != nil {
		return m.getDelayedReleaseFunc(ctx, id)
	}
	return nil, nil
}
func (m *mockRepository) ListDueDelayedReleases(_ context.Context) ([]domain.DelayedRelease, error) {
	return nil, nil
}
func (m *mockRepository) ClaimDelayedRelease(ctx context.Context, id string, status domain.DelayedReleaseStatus, decidedBy *string) (bool, error) {
	if m.claimDelayedReleaseFunc != nil {
		return m.claimDelayedReleaseFunc(ctx, id, status, decidedBy)
	}
	return true, nil
}
func (m *mockRepository) UpdateDelayedReleaseStatus(_ context.Context, _ string, _ domain.DelayedReleaseStatus) error {
	return nil
}
