package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// approvalStubRepo は createApprovalRequest の承認者解決をテストするためのスタブ。
type approvalStubRepo struct {
	mailboxAdmins map[string]int    // mailboxEmail → admin 数
	approvers     map[string]string // email → approver_id（users.approver_id）
	userIDs       map[string]string // email → user_id
	saved         *domain.ApprovalRequest
}

func (r *approvalStubRepo) SaveMessage(context.Context, *domain.Mail) error { return nil }
func (r *approvalStubRepo) UpdateMessageStatus(context.Context, string, domain.MessageStatus) error {
	return nil
}
func (r *approvalStubRepo) SaveInspectResult(context.Context, *domain.InspectResult, string) error {
	return nil
}
func (r *approvalStubRepo) SaveAttachment(context.Context, *domain.MailAttachment) error { return nil }
func (r *approvalStubRepo) UpdateProcessedEMLPath(context.Context, string, string) error { return nil }

func (r *approvalStubRepo) FindApproverForSender(_ context.Context, email string) (string, error) {
	return r.approvers[email], nil
}

func (r *approvalStubRepo) FindUserIDByEmail(_ context.Context, email string) (string, error) {
	return r.userIDs[email], nil
}

func (r *approvalStubRepo) CountMailboxAdmins(_ context.Context, mailboxEmail string) (int, error) {
	return r.mailboxAdmins[mailboxEmail], nil
}

func (r *approvalStubRepo) SaveApprovalRequest(_ context.Context, req *domain.ApprovalRequest) error {
	r.saved = req
	return nil
}
func (r *approvalStubRepo) SaveDelayedRelease(_ context.Context, _ *domain.DelayedRelease) error {
	return nil
}

func newApprovalHandler(repo *approvalStubRepo, globalEmail string) *mailHandler {
	return &mailHandler{
		repo:        repo,
		approvalCfg: config.ApprovalConfig{GlobalApproverEmail: globalEmail, ExpiryHours: 72},
	}
}

func testMail(direction domain.Direction, from string, to []string) *domain.Mail {
	return &domain.Mail{
		MessageID:   "msg-1",
		Direction:   direction,
		FromAddress: from,
		ToAddresses: to,
		ReceivedAt:  time.Now(),
	}
}

func TestCreateApprovalRequest_MailboxAdminOutbound(t *testing.T) {
	repo := &approvalStubRepo{
		mailboxAdmins: map[string]int{"sales@internal.test": 2},
		// approver_id も設定されているが、mailbox admin が優先される
		approvers: map[string]string{"sales@internal.test": "user-legacy"},
	}
	h := newApprovalHandler(repo, "")

	mail := testMail(domain.DirectionOutbound, "sales@internal.test", []string{"cust@external.test"})
	if err := h.createApprovalRequest(context.Background(), mail, slog.Default()); err != nil {
		t.Fatalf("createApprovalRequest 失敗: %v", err)
	}
	if repo.saved == nil {
		t.Fatal("承認依頼が保存されていない")
	}
	if len(repo.saved.MailboxEmails) != 1 || repo.saved.MailboxEmails[0] != "sales@internal.test" {
		t.Errorf("MailboxEmails = %v, want [sales@internal.test]", repo.saved.MailboxEmails)
	}
	if repo.saved.ApproverID != "" {
		t.Errorf("ApproverID = %q, want 空（mailbox 方式が優先）", repo.saved.ApproverID)
	}
}

func TestCreateApprovalRequest_MailboxAdminInboundAllRecipients(t *testing.T) {
	// 宛先が複数の場合、admin がいるすべてのメールボックスが対象になる
	// （admin がいない宛先は対象外）
	repo := &approvalStubRepo{
		mailboxAdmins: map[string]int{
			"second@internal.test": 1,
			"third@internal.test":  2,
		},
	}
	h := newApprovalHandler(repo, "")

	mail := testMail(domain.DirectionInbound, "ext@external.test",
		[]string{"first@internal.test", "second@internal.test", "third@internal.test"})
	if err := h.createApprovalRequest(context.Background(), mail, slog.Default()); err != nil {
		t.Fatalf("createApprovalRequest 失敗: %v", err)
	}
	want := []string{"second@internal.test", "third@internal.test"}
	if len(repo.saved.MailboxEmails) != 2 ||
		repo.saved.MailboxEmails[0] != want[0] || repo.saved.MailboxEmails[1] != want[1] {
		t.Errorf("MailboxEmails = %v, want %v", repo.saved.MailboxEmails, want)
	}
}

func TestCreateApprovalRequest_FallbackToUserApprover(t *testing.T) {
	// mailbox admin がいない場合は users.approver_id（送信者）にフォールバック
	repo := &approvalStubRepo{
		approvers: map[string]string{"taro@internal.test": "user-approver-1"},
	}
	h := newApprovalHandler(repo, "")

	mail := testMail(domain.DirectionOutbound, "taro@internal.test", []string{"cust@external.test"})
	if err := h.createApprovalRequest(context.Background(), mail, slog.Default()); err != nil {
		t.Fatalf("createApprovalRequest 失敗: %v", err)
	}
	if repo.saved.ApproverID != "user-approver-1" {
		t.Errorf("ApproverID = %q, want user-approver-1", repo.saved.ApproverID)
	}
	if len(repo.saved.MailboxEmails) != 0 {
		t.Errorf("MailboxEmails = %v, want 空", repo.saved.MailboxEmails)
	}
}

func TestCreateApprovalRequest_FallbackToGlobal(t *testing.T) {
	repo := &approvalStubRepo{
		userIDs: map[string]string{"boss@internal.test": "user-boss"},
	}
	h := newApprovalHandler(repo, "boss@internal.test")

	mail := testMail(domain.DirectionOutbound, "taro@internal.test", []string{"cust@external.test"})
	if err := h.createApprovalRequest(context.Background(), mail, slog.Default()); err != nil {
		t.Fatalf("createApprovalRequest 失敗: %v", err)
	}
	if repo.saved.ApproverID != "user-boss" {
		t.Errorf("ApproverID = %q, want user-boss", repo.saved.ApproverID)
	}
}

func TestCreateApprovalRequest_NoApproverResolved(t *testing.T) {
	repo := &approvalStubRepo{}
	h := newApprovalHandler(repo, "")

	mail := testMail(domain.DirectionOutbound, "taro@internal.test", []string{"cust@external.test"})
	if err := h.createApprovalRequest(context.Background(), mail, slog.Default()); err == nil {
		t.Fatal("承認者が解決できない場合はエラーを返すべき")
	}
	if repo.saved != nil {
		t.Error("承認者未解決なのに保存されている")
	}
}
