package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/reinject"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
	"github.com/koizumib/mailshield/services/api-server/internal/storage"
)

// ApprovalHandler は承認フロー関連の API ハンドラーである。
type ApprovalHandler struct {
	repo        repository.Repository
	emlStorage  storage.EMLStorage
	reinjector  *reinject.Reinjector
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
		reinjector:  reinject.New(notifCfg.ReinjectHost, notifCfg.ReinjectPort),
		auditLogger: auditLogger,
	}
}

// HandleList は GET /api/v1/approvals を処理する（サーバサイド検索・ページング）。
// admin/operator は全件、viewer は自分が承認者の依頼のみが対象。
//
//	q         メール件名 / 送信元 / メール ID の部分一致
//	status    対象ステータス（カンマ区切り可）。未指定時は却下を除外して表示する
//	page      ページ番号（1 始まり・既定 1）
//	per_page  1 ページ件数（既定 20・上限 100）
func (h *ApprovalHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	page := atoiDefault(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	perPage := atoiDefault(r.URL.Query().Get("per_page"), 20)
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	filter := repository.ApprovalSearchFilter{
		Query:    r.URL.Query().Get("q"),
		Statuses: parseApprovalStatuses(r.URL.Query().Get("status")),
		Limit:    perPage,
		Offset:   (page - 1) * perPage,
	}
	// viewer は自分が承認できる依頼のみに限定する。
	if session.Role != domain.RoleAdmin && session.Role != domain.RoleOperator {
		filter.ViewerID = session.User.Sub
	}

	items, total, err := h.repo.SearchApprovalRequests(r.Context(), filter)
	if err != nil {
		slog.Error("承認依頼一覧取得失敗", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items,
		"meta": map[string]int{
			"total": total, "page": page, "per_page": perPage, "total_pages": totalPages,
		},
	})
}

// parseApprovalStatuses は status クエリ（カンマ区切り）を検証済みステータス列に変換する。
// 空・全て不正な場合は「却下と期限切れ以外」…ではなく既定として
// pending / approved / expired（＝却下を除外）を返す。
func parseApprovalStatuses(raw string) []domain.ApprovalStatus {
	known := map[string]domain.ApprovalStatus{
		"pending":  domain.ApprovalStatusPending,
		"approved": domain.ApprovalStatusApproved,
		"rejected": domain.ApprovalStatusRejected,
		"expired":  domain.ApprovalStatusExpired,
	}
	var out []domain.ApprovalStatus
	for _, part := range strings.Split(raw, ",") {
		if s, ok := known[strings.TrimSpace(part)]; ok {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		// 既定: 却下は隠す（pending / approved / expired のみ）
		return []domain.ApprovalStatus{
			domain.ApprovalStatusPending,
			domain.ApprovalStatusApproved,
			domain.ApprovalStatusExpired,
		}
	}
	return out
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
		Attachments:     []domain.Attachment{},
	}
	if msg != nil {
		detail.Message = msg.Message

		// 本文を EML から抽出して Web UI 表示用に載せる（処理済み EML 優先）。
		emlPath := msg.Message.EMLPath
		if msg.Message.ProcessedEMLPath != nil && *msg.Message.ProcessedEMLPath != "" {
			emlPath = *msg.Message.ProcessedEMLPath
		}
		if raw, err := h.emlStorage.GetEML(r.Context(), emlPath); err != nil {
			// 本文が取れなくても依頼情報は返す（表示側で「本文取得不可」を出す）。
			slog.Warn("承認メール本文の取得に失敗（本文なしで続行）", "message_id", req.MessageID, "error", err)
		} else {
			body := extractEMLBody(raw)
			detail.TextBody = body.Text
			detail.HTMLBody = body.HTML
		}

		// 分離済み添付ファイル一覧（download_token 付き）。
		if atts, err := h.repo.ListAttachmentsByMessage(r.Context(), req.MessageID); err != nil {
			slog.Warn("承認メール添付一覧の取得に失敗（添付なしで続行）", "message_id", req.MessageID, "error", err)
		} else if atts != nil {
			detail.Attachments = atts
		}
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

// 決定処理のエラー分類（単一 API での HTTP コード決定・一括での失敗仕分けに使う）。
var (
	errApprovalNotFound  = fmt.Errorf("approval request not found")
	errApprovalNotClaim  = fmt.Errorf("approval request is not pending")
	errApprovalForbidden = fmt.Errorf("forbidden")
)

func (h *ApprovalHandler) decide(w http.ResponseWriter, r *http.Request, status domain.ApprovalStatus) {
	id := chi.URLParam(r, "id")
	session := middleware.GetSession(r.Context())
	if session == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		Comment string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	switch err := h.applyDecision(r.Context(), session, id, status, body.Comment); {
	case err == nil:
		// 成功（下でレスポンス）
	case errors.Is(err, errApprovalNotFound):
		http.Error(w, "not found", http.StatusNotFound)
		return
	case errors.Is(err, errApprovalNotClaim):
		http.Error(w, "approval request is not pending", http.StatusConflict)
		return
	case errors.Is(err, errApprovalForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	default:
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": string(status)})
}

// applyDecision は 1 件の承認依頼を承認/却下する共通ロジック（単一 API と一括で共用）。
// 失敗時は errApprovalNotFound / errApprovalNotClaim / errApprovalForbidden または
// 汎用エラーを返す。成功時のみ監査ログを記録する。
func (h *ApprovalHandler) applyDecision(ctx context.Context, session *domain.Session, id string, status domain.ApprovalStatus, comment string) error {
	req, err := h.repo.GetApprovalRequest(ctx, id)
	if err != nil {
		slog.Error("承認依頼取得失敗", "id", id, "error", err)
		return err
	}
	if req == nil {
		return errApprovalNotFound
	}
	if req.Status != domain.ApprovalStatusPending {
		return errApprovalNotClaim
	}

	// 操作権限チェック: viewer は自分が承認できる依頼のみ
	if session.Role == domain.RoleViewer {
		ok, err := h.canActOn(ctx, session.User.Sub, req)
		if err != nil {
			slog.Error("承認権限判定失敗", "id", id, "error", err)
			return err
		}
		if !ok {
			return errApprovalForbidden
		}
	}

	var commentPtr *string
	if comment != "" {
		commentPtr = &comment
	}

	// 先に依頼を原子的にクレームする（pending の場合のみ更新される）。
	// メールボックス承認では複数の承認者が同時に決定できるため、
	// クレームに勝った 1 人だけが配送を実行し、二重配送を防ぐ。
	claimed, err := h.repo.ClaimApprovalRequest(ctx, id, status, commentPtr)
	if err != nil {
		slog.Error("承認ステータス更新失敗", "id", id, "error", err)
		return err
	}
	if !claimed {
		return errApprovalNotClaim
	}

	if status == domain.ApprovalStatusApproved {
		if err := h.approveAndReinject(ctx, req); err != nil {
			// 配送に失敗したら依頼を pending に戻し、再試行できるようにする
			if rbErr := h.repo.UpdateApprovalStatus(ctx, id, domain.ApprovalStatusPending, nil); rbErr != nil {
				slog.Error("承認ステータスのロールバック失敗（手動対応が必要）", "id", id, "error", rbErr)
			}
			slog.Error("承認・再インジェクト失敗", "approval_id", id, "error", err)
			return err
		}
	}

	h.auditLogger.Log(domain.AuditLog{
		EventType:  fmt.Sprintf("approval.%s", string(status)),
		ActorID:    audit.StrPtr(session.User.Sub),
		ActorEmail: audit.StrPtr(session.User.Email),
		TargetType: audit.StrPtr("approval_request"),
		TargetID:   audit.StrPtr(id),
	})
	return nil
}

// HandleBulkApprove は POST /api/v1/approvals/bulk-approve を処理する。
func (h *ApprovalHandler) HandleBulkApprove(w http.ResponseWriter, r *http.Request) {
	h.bulkDecide(w, r, domain.ApprovalStatusApproved)
}

// HandleBulkReject は POST /api/v1/approvals/bulk-reject を処理する。
func (h *ApprovalHandler) HandleBulkReject(w http.ResponseWriter, r *http.Request) {
	h.bulkDecide(w, r, domain.ApprovalStatusRejected)
}

// bulkDecide は複数の承認依頼をまとめて承認/却下し、成功・失敗を仕分けて返す。
func (h *ApprovalHandler) bulkDecide(w http.ResponseWriter, r *http.Request, status domain.ApprovalStatus) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		IDs     []string `json:"ids"`
		Comment string   `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの解析に失敗しました")
		return
	}
	if len(body.IDs) == 0 {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "ids が空です")
		return
	}
	if len(body.IDs) > 100 {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "一度に処理できるのは 100 件までです")
		return
	}

	succeeded := []string{}
	failed := map[string]string{}
	for _, id := range body.IDs {
		switch err := h.applyDecision(r.Context(), session, id, status, body.Comment); {
		case err == nil:
			succeeded = append(succeeded, id)
		case errors.Is(err, errApprovalNotFound):
			failed[id] = "見つかりません"
		case errors.Is(err, errApprovalNotClaim):
			failed[id] = "すでに決定済みです"
		case errors.Is(err, errApprovalForbidden):
			failed[id] = "権限がありません"
		default:
			failed[id] = "処理に失敗しました"
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"succeeded": succeeded,
		"failed":    failed,
	})
}

// canActOn は userID がこの承認依頼を閲覧・決定できるかを返す。
//   - approver_id 指定の依頼: 自分が承認者本人
//   - メールボックス承認の依頼: 対象メールボックスのいずれかに admin 割り当てを持つ
func (h *ApprovalHandler) canActOn(ctx context.Context, userID string, req *domain.ApprovalRequest) (bool, error) {
	if req.ApproverID != nil && *req.ApproverID == userID {
		return true, nil
	}
	for _, mailbox := range req.MailboxEmails {
		ok, err := h.repo.IsMailboxApprover(ctx, userID, mailbox)
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

	if err := h.reinjector.Send(ctx, msg.Message.FromAddress, msg.Message.ToAddresses, eml); err != nil {
		// ロールバック
		_ = h.repo.UpdateMessageStatus(ctx, req.MessageID, domain.StatusApprovalPending)
		return fmt.Errorf("再インジェクト失敗: %w", err)
	}

	slog.Info("承認メール再インジェクト完了", "message_id", req.MessageID, "approval_id", req.ID)
	return nil
}
