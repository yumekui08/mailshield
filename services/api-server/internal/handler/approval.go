package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"

	"github.com/go-chi/chi/v5"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
	"github.com/koizumib/mailshield/services/api-server/internal/storage"
)

// ApprovalHandler は承認フロー関連の API ハンドラーである。
type ApprovalHandler struct {
	repo        repository.Repository
	emlStorage  storage.EMLStorage
	notifCfg    config.NotificationConfig
	auditLogger *audit.Logger
}

// NewApprovalHandler は ApprovalHandler を返す。
func NewApprovalHandler(
	repo repository.Repository,
	emlStorage storage.EMLStorage,
	notifCfg config.NotificationConfig,
	auditLogger *audit.Logger,
) *ApprovalHandler {
	return &ApprovalHandler{
		repo:        repo,
		emlStorage:  emlStorage,
		notifCfg:    notifCfg,
		auditLogger: auditLogger,
	}
}

// HandleList は GET /api/v1/approvals を処理する。
// admin/operator は全件、viewer は自分が承認者の pending 依頼のみ返す。
func (h *ApprovalHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var list []domain.ApprovalRequest
	var err error
	if session.Role == domain.RoleAdmin || session.Role == domain.RoleOperator {
		list, err = h.repo.ListAllApprovalRequests(r.Context())
	} else {
		list, err = h.repo.ListApprovalRequests(r.Context(), session.User.Sub)
	}
	if err != nil {
		slog.Error("承認依頼一覧取得失敗", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": list})
}

// HandleGet は GET /api/v1/approvals/{id} を処理する。
func (h *ApprovalHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	req, err := h.repo.GetApprovalRequest(r.Context(), id)
	if err != nil {
		slog.Error("承認依頼取得失敗", "id", id, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if req == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// 閲覧権限チェック: viewer は自分が承認できる依頼のみ
	session := middleware.GetSession(r.Context())
	if session != nil && session.Role == domain.RoleViewer {
		ok, err := h.canActOn(r.Context(), session.User.Sub, req)
		if err != nil {
			slog.Error("承認権限判定失敗", "id", id, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	msg, err := h.repo.GetMessage(r.Context(), req.MessageID)
	if err != nil {
		slog.Error("メール情報取得失敗", "message_id", req.MessageID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	detail := domain.ApprovalRequestDetail{
		ApprovalRequest: *req,
	}
	if msg != nil {
		detail.Message = msg.Message
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(detail)
}

// HandleApprove は POST /api/v1/approvals/{id}/approve を処理する。
func (h *ApprovalHandler) HandleApprove(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, domain.ApprovalStatusApproved)
}

// HandleReject は POST /api/v1/approvals/{id}/reject を処理する。
func (h *ApprovalHandler) HandleReject(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, domain.ApprovalStatusRejected)
}

func (h *ApprovalHandler) decide(w http.ResponseWriter, r *http.Request, status domain.ApprovalStatus) {
	id := chi.URLParam(r, "id")
	session := middleware.GetSession(r.Context())
	if session == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	req, err := h.repo.GetApprovalRequest(r.Context(), id)
	if err != nil {
		slog.Error("承認依頼取得失敗", "id", id, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if req == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if req.Status != domain.ApprovalStatusPending {
		http.Error(w, "approval request is not pending", http.StatusConflict)
		return
	}

	// 操作権限チェック: viewer は自分が承認できる依頼のみ
	if session.Role == domain.RoleViewer {
		ok, err := h.canActOn(r.Context(), session.User.Sub, req)
		if err != nil {
			slog.Error("承認権限判定失敗", "id", id, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	var body struct {
		Comment string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	var comment *string
	if body.Comment != "" {
		comment = &body.Comment
	}

	// 先に依頼を原子的にクレームする（pending の場合のみ更新される）。
	// メールボックス承認では複数の承認者が同時に決定できるため、
	// クレームに勝った 1 人だけが配送を実行し、二重配送を防ぐ。
	claimed, err := h.repo.ClaimApprovalRequest(r.Context(), id, status, comment)
	if err != nil {
		slog.Error("承認ステータス更新失敗", "id", id, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !claimed {
		http.Error(w, "approval request is not pending", http.StatusConflict)
		return
	}

	if status == domain.ApprovalStatusApproved {
		if err := h.approveAndReinject(r.Context(), req); err != nil {
			// 配送に失敗したら依頼を pending に戻し、再試行できるようにする
			if rbErr := h.repo.UpdateApprovalStatus(r.Context(), id, domain.ApprovalStatusPending, nil); rbErr != nil {
				slog.Error("承認ステータスのロールバック失敗（手動対応が必要）", "id", id, "error", rbErr)
			}
			slog.Error("承認・再インジェクト失敗", "approval_id", id, "error", err)
			http.Error(w, "failed to reinject mail", http.StatusInternalServerError)
			return
		}
	}

	h.auditLogger.Log(domain.AuditLog{
		EventType:  fmt.Sprintf("approval.%s", string(status)),
		ActorID:    audit.StrPtr(session.User.Sub),
		ActorEmail: audit.StrPtr(session.User.Email),
		TargetType: audit.StrPtr("approval_request"),
		TargetID:   audit.StrPtr(id),
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": string(status)})
}

// canActOn は userID がこの承認依頼を閲覧・決定できるかを返す。
//   - approver_id 指定の依頼: 自分が承認者本人
//   - メールボックス承認の依頼: 対象メールボックスのいずれかに admin 割り当てを持つ
func (h *ApprovalHandler) canActOn(ctx context.Context, userID string, req *domain.ApprovalRequest) (bool, error) {
	if req.ApproverID != nil && *req.ApproverID == userID {
		return true, nil
	}
	for _, mailbox := range req.MailboxEmails {
		ok, err := h.repo.IsMailboxAdmin(ctx, userID, mailbox)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// approveAndReinject は承認時に EML を MinIO から取得して Postfix へ再インジェクトする。
func (h *ApprovalHandler) approveAndReinject(ctx context.Context, req *domain.ApprovalRequest) error {
	msg, err := h.repo.GetMessage(ctx, req.MessageID)
	if err != nil || msg == nil {
		return fmt.Errorf("メール取得失敗 (message_id=%s): %w", req.MessageID, err)
	}

	// 変換後 EML を優先、なければ原本 EML を使用
	emlPath := msg.Message.EMLPath
	if msg.Message.ProcessedEMLPath != nil && *msg.Message.ProcessedEMLPath != "" {
		emlPath = *msg.Message.ProcessedEMLPath
	}

	eml, err := h.emlStorage.GetEML(ctx, emlPath)
	if err != nil {
		return fmt.Errorf("EML 取得失敗 (path=%s): %w", emlPath, err)
	}

	// 二重配送防止: reinject 前に delivered へ更新
	if err := h.repo.UpdateMessageStatus(ctx, req.MessageID, domain.StatusDelivered); err != nil {
		return fmt.Errorf("ステータス更新失敗: %w", err)
	}

	if err := h.reinject(ctx, msg.Message.FromAddress, msg.Message.ToAddresses, eml); err != nil {
		// ロールバック
		_ = h.repo.UpdateMessageStatus(ctx, req.MessageID, domain.StatusApprovalPending)
		return fmt.Errorf("再インジェクト失敗: %w", err)
	}

	slog.Info("承認メール再インジェクト完了", "message_id", req.MessageID, "approval_id", req.ID)
	return nil
}

func (h *ApprovalHandler) reinject(ctx context.Context, from string, to []string, eml []byte) error {
	addr := fmt.Sprintf("%s:%d", h.notifCfg.ReinjectHost, h.notifCfg.ReinjectPort)
	conn, err := (&net.Dialer{Timeout: 30_000_000_000}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("SMTP 接続失敗 (addr=%s): %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	c, err := smtp.NewClient(conn, h.notifCfg.ReinjectHost)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("SMTP クライアント作成失敗: %w", err)
	}
	defer c.Close()

	if err := c.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM 失敗: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO 失敗 (%s): %w", rcpt, err)
		}
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA 失敗: %w", err)
	}
	if _, err := wc.Write(eml); err != nil {
		return fmt.Errorf("メール送信失敗: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("DATA 完了失敗: %w", err)
	}
	return c.Quit()
}

// HandleGetUserApprover は GET /api/v1/users/{id}/approver を処理する（admin のみ）。
func (h *ApprovalHandler) HandleGetUserApprover(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	user, err := h.repo.GetUser(r.Context(), userID)
	if err != nil {
		slog.Error("ユーザー取得失敗", "user_id", userID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	type response struct {
		ApproverID *string `json:"approver_id"`
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response{ApproverID: user.ApproverID})
}

// HandleSetUserApprover は PUT /api/v1/users/{id}/approver を処理する（admin のみ）。
func (h *ApprovalHandler) HandleSetUserApprover(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")

	var body struct {
		ApproverID *string `json:"approver_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.repo.UpdateUserApprover(r.Context(), userID, body.ApproverID); err != nil {
		slog.Error("承認者設定失敗", "user_id", userID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	session := middleware.GetSession(r.Context())
	if session != nil {
		h.auditLogger.Log(domain.AuditLog{
			EventType:  "user.approver_updated",
			ActorID:    audit.StrPtr(session.User.Sub),
			ActorEmail: audit.StrPtr(session.User.Email),
			TargetType: audit.StrPtr("user"),
			TargetID:   audit.StrPtr(userID),
		})
	}

	w.WriteHeader(http.StatusNoContent)
}
