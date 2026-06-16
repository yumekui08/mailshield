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

// QuarantineHandler は隔離メッセージ関連のAPIハンドラーである。
type QuarantineHandler struct {
	repo        repository.Repository
	emlStorage  storage.EMLStorage
	notifCfg    config.NotificationConfig
	policy      config.MailboxPolicyConfig
	auditLogger *audit.Logger
}

// NewQuarantineHandler はQuarantineHandlerを返す。
func NewQuarantineHandler(
	repo repository.Repository,
	emlStorage storage.EMLStorage,
	notifCfg config.NotificationConfig,
	policy config.MailboxPolicyConfig,
	auditLogger *audit.Logger,
) *QuarantineHandler {
	return &QuarantineHandler{
		repo:        repo,
		emlStorage:  emlStorage,
		notifCfg:    notifCfg,
		policy:      policy,
		auditLogger: auditLogger,
	}
}

// HandleList はGET /api/v1/quarantine を処理する。
// status=quarantined固定でメッセージ一覧を返す。
// admin/operator は全件閲覧。viewer は mailbox_policy に従ってフィルタリングされる。
func (h *QuarantineHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
		return
	}

	q, err := parseListQuery(r)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	// viewer は mailbox_policy に従って可視性フィルターを適用する
	if session.Role == domain.RoleViewer {
		filter, err := h.buildVisibilityFilter(r, session.User.Sub)
		if err != nil {
			slog.Error("可視性フィルター構築失敗", "user_id", session.User.Sub, "error", err)
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "隔離メッセージの取得に失敗しました")
			return
		}
		q.VisibilityFilter = filter
	}

	messages, total, err := h.repo.ListQuarantine(r.Context(), q)
	if err != nil {
		slog.Error("隔離メッセージ一覧取得失敗",
			"error", err,
		)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "隔離メッセージの取得に失敗しました")
		return
	}

	totalPages := calcTotalPages(total, q.PerPage)
	result := domain.PagedResult[domain.Message]{
		Data: messages,
		Meta: domain.PageMeta{
			Total:      total,
			Page:       q.Page,
			PerPage:    q.PerPage,
			TotalPages: totalPages,
		},
	}

	writeJSON(w, http.StatusOK, result)
}

// HandleGet はGET /api/v1/quarantine/{id} を処理する。
func (h *QuarantineHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "idが必要です")
		return
	}

	detail, err := h.repo.GetQuarantine(r.Context(), id)
	if err != nil {
		slog.Warn("隔離メッセージ取得失敗",
			"message_id", id,
			"error", err,
		)
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "隔離メッセージが見つかりません")
		return
	}

	// viewer の場合は可視性ポリシーに従って閲覧権限を確認する
	if session.Role == domain.RoleViewer {
		visible, err := h.canView(r, session.User.Sub, &detail.Message)
		if err != nil {
			slog.Error("閲覧権限確認失敗", "user_id", session.User.Sub, "message_id", id, "error", err)
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "閲覧権限の確認に失敗しました")
			return
		}
		if !visible {
			writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "隔離メッセージが見つかりません")
			return
		}
	}

	writeJSON(w, http.StatusOK, detail)
}

// HandleRelease はPOST /api/v1/quarantine/{id}/release を処理する。
// DBのstatusをdeliveredに更新する（実際の再配送は将来実装）。
// admin/operator は無条件で解放可。viewer は mailbox_policy の release_by に従う。
func (h *QuarantineHandler) HandleRelease(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "idが必要です")
		return
	}

	// 隔離メッセージの存在確認
	msg, err := h.repo.GetQuarantine(r.Context(), id)
	if err != nil {
		slog.Warn("解放対象の隔離メッセージが見つかりません",
			"message_id", id,
			"error", err,
		)
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "隔離メッセージが見つかりません")
		return
	}

	// viewer の場合は release_by ポリシーに従って解放権限を確認する
	if session.Role == domain.RoleViewer {
		allowed, err := h.canRelease(r, session.User.Sub, &msg.Message)
		if err != nil {
			slog.Error("解放権限確認失敗", "user_id", session.User.Sub, "message_id", id, "error", err)
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "解放権限の確認に失敗しました")
			return
		}
		if !allowed {
			writeErrorResponse(w, http.StatusForbidden, "FORBIDDEN", "このメッセージを解放する権限がありません")
			return
		}
	}

	// 処理済み EML パスの確認
	if msg.Message.ProcessedEMLPath == nil {
		slog.Warn("処理済み EML がまだアーカイブ中",
			"message_id", id,
		)
		writeErrorResponse(w, http.StatusConflict, "NOT_READY", "変換後 EML がまだアーカイブ処理中です。しばらく待ってから再試行してください")
		return
	}

	// 二重配送防止: reinject の前に DB を delivered に更新する。
	// GetQuarantine は status=quarantined のみ返すため、次のリクエストは 404 になる。
	// reinject や EML 取得が失敗した場合は quarantined に rollback する。
	if err := h.repo.UpdateMessageStatus(r.Context(), id, domain.StatusDelivered); err != nil {
		slog.Error("隔離解放前のステータス更新失敗",
			"message_id", id,
			"error", err,
		)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "status 更新に失敗しました")
		return
	}

	// MinIO から処理済み EML を取得
	eml, err := h.emlStorage.GetEML(r.Context(), *msg.Message.ProcessedEMLPath)
	if err != nil {
		slog.Error("処理済み EML 取得失敗",
			"message_id", id,
			"path", *msg.Message.ProcessedEMLPath,
			"error", err,
		)
		h.rollbackToQuarantined(r.Context(), id)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "EML の取得に失敗しました")
		return
	}

	// Postfix の再インジェクトポートへ SMTP 送信
	if err := h.reinject(r.Context(), msg.Message.FromAddress, msg.Message.ToAddresses, eml); err != nil {
		slog.Error("再インジェクト失敗",
			"message_id", id,
			"error", err,
		)
		h.rollbackToQuarantined(r.Context(), id)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "メールの再配送に失敗しました")
		return
	}

	slog.Info("隔離メッセージ解放完了", "message_id", id)

	ip := audit.ClientIP(r)
	h.auditLogger.Log(domain.AuditLog{
		EventType:  domain.EventQuarantineReleased,
		ActorID:    &session.User.Sub,
		ActorEmail: &session.User.Email,
		TargetType: audit.StrPtr("message"),
		TargetID:   &id,
		IPAddress:  &ip,
		Detail:     map[string]any{"subject": msg.Message.Subject},
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "隔離解放しました",
		"id":      id,
		"status":  string(domain.StatusDelivered),
	})
}

// bulkResult は一括操作の結果を保持する。
type bulkResult struct {
	Succeeded []string         `json:"succeeded"`
	Failed    []bulkFailedItem `json:"failed"`
}

type bulkFailedItem struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// HandleBulkRelease は POST /api/v1/quarantine/bulk-release を処理する。
// 指定した複数 ID を隔離から解放する。operator/admin のみ利用可。
// 各 ID を独立して処理し、失敗があっても続行する（部分成功を返す）。
func (h *QuarantineHandler) HandleBulkRelease(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
		return
	}

	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストボディが不正です")
		return
	}
	if len(body.IDs) == 0 {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "ids が空です")
		return
	}
	if len(body.IDs) > 100 {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "一度に処理できるのは最大 100 件です")
		return
	}

	result := bulkResult{
		Succeeded: make([]string, 0),
		Failed:    make([]bulkFailedItem, 0),
	}

	for _, id := range body.IDs {
		msg, err := h.repo.GetQuarantine(r.Context(), id)
		if err != nil {
			result.Failed = append(result.Failed, bulkFailedItem{ID: id, Reason: "隔離メッセージが見つかりません"})
			continue
		}
		if msg.Message.ProcessedEMLPath == nil {
			result.Failed = append(result.Failed, bulkFailedItem{ID: id, Reason: "変換後 EML がまだアーカイブ処理中です"})
			continue
		}
		if err := h.repo.UpdateMessageStatus(r.Context(), id, domain.StatusDelivered); err != nil {
			result.Failed = append(result.Failed, bulkFailedItem{ID: id, Reason: "status 更新失敗"})
			continue
		}
		eml, err := h.emlStorage.GetEML(r.Context(), *msg.Message.ProcessedEMLPath)
		if err != nil {
			h.rollbackToQuarantined(r.Context(), id)
			result.Failed = append(result.Failed, bulkFailedItem{ID: id, Reason: "EML 取得失敗"})
			continue
		}
		if err := h.reinject(r.Context(), msg.Message.FromAddress, msg.Message.ToAddresses, eml); err != nil {
			h.rollbackToQuarantined(r.Context(), id)
			result.Failed = append(result.Failed, bulkFailedItem{ID: id, Reason: "再配送失敗"})
			slog.Error("一括解放: 再インジェクト失敗", "message_id", id, "error", err)
			continue
		}
		result.Succeeded = append(result.Succeeded, id)
	}

	slog.Info("一括隔離解放完了", "succeeded", len(result.Succeeded), "failed", len(result.Failed))

	ip := audit.ClientIP(r)
	h.auditLogger.Log(domain.AuditLog{
		EventType:  domain.EventQuarantineBulkReleased,
		ActorID:    &session.User.Sub,
		ActorEmail: &session.User.Email,
		IPAddress:  &ip,
		Detail: map[string]any{
			"succeeded": result.Succeeded,
			"failed":    len(result.Failed),
		},
	})

	writeJSON(w, http.StatusOK, result)
}

// HandleBulkDelete は DELETE /api/v1/quarantine/bulk を処理する。
// 指定した複数 ID を一括削除（status=rejected）する。operator/admin のみ利用可。
func (h *QuarantineHandler) HandleBulkDelete(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
		return
	}

	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストボディが不正です")
		return
	}
	if len(body.IDs) == 0 {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "ids が空です")
		return
	}
	if len(body.IDs) > 100 {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "一度に処理できるのは最大 100 件です")
		return
	}

	if err := h.repo.BulkUpdateMessageStatus(r.Context(), body.IDs, domain.StatusRejected); err != nil {
		slog.Error("一括削除失敗", "count", len(body.IDs), "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "削除に失敗しました")
		return
	}

	slog.Info("一括隔離削除完了", "count", len(body.IDs))

	ip := audit.ClientIP(r)
	h.auditLogger.Log(domain.AuditLog{
		EventType:  domain.EventQuarantineBulkDeleted,
		ActorID:    &session.User.Sub,
		ActorEmail: &session.User.Email,
		IPAddress:  &ip,
		Detail:     map[string]any{"count": len(body.IDs)},
	})

	writeJSON(w, http.StatusOK, bulkResult{
		Succeeded: body.IDs,
		Failed:    []bulkFailedItem{},
	})
}

// HandleDelete はDELETE /api/v1/quarantine/{id} を処理する。
// DBのstatusをrejectedに更新する。
func (h *QuarantineHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "idが必要です")
		return
	}

	// 隔離メッセージの存在確認
	_, err := h.repo.GetQuarantine(r.Context(), id)
	if err != nil {
		slog.Warn("削除対象の隔離メッセージが見つかりません",
			"message_id", id,
			"error", err,
		)
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "隔離メッセージが見つかりません")
		return
	}

	if err := h.repo.UpdateMessageStatus(r.Context(), id, domain.StatusRejected); err != nil {
		slog.Error("隔離メッセージ削除失敗（status更新失敗）",
			"message_id", id,
			"error", err,
		)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "削除に失敗しました")
		return
	}

	slog.Info("隔離メッセージ削除（拒否）", "message_id", id)

	ip := audit.ClientIP(r)
	h.auditLogger.Log(domain.AuditLog{
		EventType:  domain.EventQuarantineDeleted,
		ActorID:    &session.User.Sub,
		ActorEmail: &session.User.Email,
		TargetType: audit.StrPtr("message"),
		TargetID:   &id,
		IPAddress:  &ip,
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "削除しました",
		"id":      id,
		"status":  string(domain.StatusRejected),
	})
}

// reinject は処理済み EML を Postfix の再インジェクトポートへ SMTP 送信する。
// content_filter なしのポート（デフォルト postfix:10025）に直接送信することで
// 検査・変換パイプラインをスキップして最終配送先へ届ける。
// ctx の deadline/cancel が TCP 接続・全 SMTP I/O に伝播する。
func (h *QuarantineHandler) reinject(ctx context.Context, from string, to []string, eml []byte) error {
	addr := fmt.Sprintf("%s:%d", h.notifCfg.ReinjectHost, h.notifCfg.ReinjectPort)
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("SMTP 接続失敗 (%s): %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline) //nolint:errcheck
	}
	c, err := smtp.NewClient(conn, h.notifCfg.ReinjectHost)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP クライアント初期化失敗: %w", err)
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
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA コマンド失敗: %w", err)
	}
	if _, err := w.Write(eml); err != nil {
		return fmt.Errorf("EML 書き込み失敗: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("DATA 終了失敗: %w", err)
	}
	return c.Quit()
}

// rollbackToQuarantined は delivered に更新済みのステータスを quarantined に戻す。
// reinject や EML 取得が失敗した際のロールバックに使う。
func (h *QuarantineHandler) rollbackToQuarantined(ctx context.Context, id string) {
	if err := h.repo.UpdateMessageStatus(ctx, id, domain.StatusQuarantined); err != nil {
		slog.Error("ロールバック失敗（手動で status=quarantined に戻す必要があります）",
			"message_id", id,
			"error", err,
		)
	}
}

// buildVisibilityFilter は viewer ロールのユーザー向けに可視性フィルターを構築する。
func (h *QuarantineHandler) buildVisibilityFilter(r *http.Request, userID string) (*domain.MailboxVisibilityFilter, error) {
	inboundRoles := toAssignmentRoles(h.policy.InboundQuarantine.VisibleTo)
	outboundRoles := toAssignmentRoles(h.policy.OutboundQuarantine.VisibleTo)

	inbound, err := h.repo.GetMailboxAddressesForUser(r.Context(), userID, inboundRoles)
	if err != nil {
		return nil, err
	}

	outbound, err := h.repo.GetMailboxAddressesForUser(r.Context(), userID, outboundRoles)
	if err != nil {
		return nil, err
	}

	return &domain.MailboxVisibilityFilter{
		InboundMailboxes:  inbound,
		OutboundMailboxes: outbound,
	}, nil
}

// canView は viewer が指定メッセージを閲覧できるか判定する。
func (h *QuarantineHandler) canView(r *http.Request, userID string, msg *domain.Message) (bool, error) {
	inboundRoles := toAssignmentRoles(h.policy.InboundQuarantine.VisibleTo)
	outboundRoles := toAssignmentRoles(h.policy.OutboundQuarantine.VisibleTo)

	inbound, err := h.repo.GetMailboxAddressesForUser(r.Context(), userID, inboundRoles)
	if err != nil {
		return false, err
	}
	inboundSet := sliceToSet(inbound)
	for _, to := range msg.ToAddresses {
		if inboundSet[to] {
			return true, nil
		}
	}

	outbound, err := h.repo.GetMailboxAddressesForUser(r.Context(), userID, outboundRoles)
	if err != nil {
		return false, err
	}
	outboundSet := sliceToSet(outbound)
	return outboundSet[msg.FromAddress], nil
}

// canRelease は viewer が指定メッセージを解放できるか判定する。
func (h *QuarantineHandler) canRelease(r *http.Request, userID string, msg *domain.Message) (bool, error) {
	inboundRoles := toAssignmentRoles(h.policy.InboundQuarantine.ReleaseBy)
	outboundRoles := toAssignmentRoles(h.policy.OutboundQuarantine.ReleaseBy)

	inbound, err := h.repo.GetMailboxAddressesForUser(r.Context(), userID, inboundRoles)
	if err != nil {
		return false, err
	}
	inboundSet := sliceToSet(inbound)
	for _, to := range msg.ToAddresses {
		if inboundSet[to] {
			return true, nil
		}
	}

	outbound, err := h.repo.GetMailboxAddressesForUser(r.Context(), userID, outboundRoles)
	if err != nil {
		return false, err
	}
	outboundSet := sliceToSet(outbound)
	return outboundSet[msg.FromAddress], nil
}

// toAssignmentRoles は設定の文字列スライスを AssignmentRole スライスに変換する。
func toAssignmentRoles(roles []string) []domain.AssignmentRole {
	result := make([]domain.AssignmentRole, 0, len(roles))
	for _, r := range roles {
		result = append(result, domain.AssignmentRole(r))
	}
	return result
}

// sliceToSet は文字列スライスを存在確認用マップに変換する。
func sliceToSet(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}
