// Package approval は承認フローのバックグラウンドサービスを提供する。
// 未送信の承認依頼通知メール送信と期限切れ処理を担う。
package approval

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"text/template"
	"time"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// Service は承認フロー関連のバックグラウンド処理を担う。
type Service struct {
	repo     repository.Repository
	cfg      config.ApprovalConfig
	notifCfg config.NotificationConfig
}

// New は Service を生成する。
func New(
	repo repository.Repository,
	cfg config.ApprovalConfig,
	notifCfg config.NotificationConfig,
) *Service {
	return &Service{
		repo:     repo,
		cfg:      cfg,
		notifCfg: notifCfg,
	}
}

// RunNotifier は未送信の承認依頼通知を定期的に送信するループを起動する。
// ctx がキャンセルされると停止する。
func (s *Service) RunNotifier(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sendPendingNotifications(ctx)
			s.sendResultNotifications(ctx)
		}
	}
}

// RunExpiryWorker は期限切れ承認依頼を定期的に処理するループを起動する。
func (s *Service) RunExpiryWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.expireApprovals(ctx)
		}
	}
}

// sendPendingNotifications は notification_sent=false の依頼に承認者通知を送る。
func (s *Service) sendPendingNotifications(ctx context.Context) {
	if !s.cfg.Notification.RequestEnabled {
		return
	}
	list, err := s.repo.ListPendingUnnotified(ctx)
	if err != nil {
		slog.Error("未送信承認依頼取得失敗", "error", err)
		return
	}
	for _, req := range list {
		if err := s.sendRequestNotification(ctx, req); err != nil {
			slog.Warn("承認依頼通知送信失敗", "approval_id", req.ID, "error", err)
			continue
		}
		if err := s.repo.MarkApprovalNotificationSent(ctx, req.ID); err != nil {
			slog.Warn("notification_sent 更新失敗", "approval_id", req.ID, "error", err)
		}
	}
}

// sendResultNotifications は result_notified=false の依頼に承認結果を送信者へ通知する。
func (s *Service) sendResultNotifications(ctx context.Context) {
	if !s.cfg.Notification.ResultEnabled {
		return
	}
	list, err := s.repo.ListResultUnnotified(ctx)
	if err != nil {
		slog.Error("未通知承認結果取得失敗", "error", err)
		return
	}
	for _, req := range list {
		if err := s.sendResultNotification(ctx, req); err != nil {
			slog.Warn("承認結果通知送信失敗", "approval_id", req.ID, "error", err)
			continue
		}
		if err := s.repo.MarkApprovalResultNotified(ctx, req.ID); err != nil {
			slog.Warn("result_notified 更新失敗", "approval_id", req.ID, "error", err)
		}
	}
}

// expireApprovals は期限切れ依頼を expired にし、メール情報を expired ステータスに更新する。
func (s *Service) expireApprovals(ctx context.Context) {
	messageIDs, err := s.repo.ExpireApprovals(ctx)
	if err != nil {
		slog.Error("承認期限切れ処理失敗", "error", err)
		return
	}
	for _, msgID := range messageIDs {
		if err := s.repo.UpdateMessageStatus(ctx, msgID, domain.StatusExpired); err != nil {
			slog.Warn("メールステータス expired 更新失敗", "message_id", msgID, "error", err)
		}
		slog.Info("承認依頼期限切れ処理完了", "message_id", msgID)
	}
}

// templateData は通知メールのテンプレート変数を保持する。
type templateData struct {
	Subject     string
	FromAddress string
	ToAddresses []string
	ReceivedAt  string
	ExpiresAt   string
	ApprovalURL string
	Comment     string
}

func (s *Service) sendRequestNotification(ctx context.Context, req domain.ApprovalRequest) error {
	approver, err := s.repo.GetUser(ctx, req.ApproverID)
	if err != nil || approver == nil {
		return fmt.Errorf("承認者情報取得失敗 (approver_id=%s): %w", req.ApproverID, err)
	}
	msg, err := s.repo.GetMessage(ctx, req.MessageID)
	if err != nil || msg == nil {
		return fmt.Errorf("メール情報取得失敗 (message_id=%s): %w", req.MessageID, err)
	}

	approvalURL := fmt.Sprintf("%s/approvals/%s", s.cfg.BaseURL, req.ID)
	data := templateData{
		Subject:     msg.Message.Subject,
		FromAddress: msg.Message.FromAddress,
		ToAddresses: msg.Message.ToAddresses,
		ReceivedAt:  msg.Message.ReceivedAt.Format("2006-01-02 15:04:05"),
		ExpiresAt:   req.ExpiresAt.Format("2006-01-02 15:04:05"),
		ApprovalURL: approvalURL,
	}

	subject, err := renderTemplate(s.cfg.Notification.RequestSubjectTemplate, data)
	if err != nil {
		return fmt.Errorf("件名テンプレート描画失敗: %w", err)
	}
	body, err := renderTemplate(s.cfg.Notification.RequestBodyTemplate, data)
	if err != nil {
		return fmt.Errorf("本文テンプレート描画失敗: %w", err)
	}

	fromName := s.cfg.Notification.FromName
	if fromName == "" {
		fromName = "MailShield"
	}
	return s.sendMail(ctx, approver.Email, subject, body, fromName)
}

func (s *Service) sendResultNotification(ctx context.Context, req domain.ApprovalRequest) error {
	msg, err := s.repo.GetMessage(ctx, req.MessageID)
	if err != nil || msg == nil {
		return fmt.Errorf("メール情報取得失敗 (message_id=%s): %w", req.MessageID, err)
	}

	// 内部ユーザーのみ通知（外部送信者へは漏洩しない）
	sender, err := s.repo.FindUserByEmailInternal(ctx, msg.Message.FromAddress)
	if err != nil {
		return fmt.Errorf("送信者検索失敗: %w", err)
	}
	if sender == nil {
		return nil // 外部ユーザーは通知不要
	}

	comment := ""
	if req.Comment != nil {
		comment = *req.Comment
	}
	data := templateData{
		Subject:     msg.Message.Subject,
		FromAddress: msg.Message.FromAddress,
		ToAddresses: msg.Message.ToAddresses,
		Comment:     comment,
	}

	var subjectTpl, bodyTpl string
	switch req.Status {
	case domain.ApprovalStatusApproved:
		subjectTpl = s.cfg.Notification.ApprovedSubjectTemplate
		bodyTpl = s.cfg.Notification.ApprovedBodyTemplate
	case domain.ApprovalStatusRejected:
		subjectTpl = s.cfg.Notification.RejectedSubjectTemplate
		bodyTpl = s.cfg.Notification.RejectedBodyTemplate
	default:
		return nil
	}

	subject, err := renderTemplate(subjectTpl, data)
	if err != nil {
		return fmt.Errorf("件名テンプレート描画失敗: %w", err)
	}
	body, err := renderTemplate(bodyTpl, data)
	if err != nil {
		return fmt.Errorf("本文テンプレート描画失敗: %w", err)
	}

	fromName := s.cfg.Notification.FromName
	if fromName == "" {
		fromName = "MailShield"
	}
	return s.sendMail(ctx, sender.Email, subject, body, fromName)
}

func (s *Service) sendMail(ctx context.Context, to, subject, body, fromName string) error {
	fromAddr := s.cfg.Notification.FromAddress
	if fromAddr == "" {
		fromAddr = s.notifCfg.FromAddress
	}

	addr := fmt.Sprintf("%s:%d", s.notifCfg.SMTPHost, s.notifCfg.SMTPPort)
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("SMTP 接続失敗: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	c, err := smtp.NewClient(conn, s.notifCfg.SMTPHost)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("SMTP クライアント作成失敗: %w", err)
	}
	defer c.Close()

	rawFrom := fromAddr
	if fromName != "" {
		rawFrom = fmt.Sprintf("%s <%s>", fromName, fromAddr)
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		rawFrom, to, subject, body)

	if err := c.Mail(fromAddr); err != nil {
		return fmt.Errorf("MAIL FROM 失敗: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("RCPT TO 失敗: %w", err)
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA 失敗: %w", err)
	}
	if _, err := fmt.Fprint(wc, msg); err != nil {
		return fmt.Errorf("メール本文送信失敗: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("DATA 完了失敗: %w", err)
	}
	return c.Quit()
}

func renderTemplate(tmpl string, data templateData) (string, error) {
	if tmpl == "" {
		return "", nil
	}
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
