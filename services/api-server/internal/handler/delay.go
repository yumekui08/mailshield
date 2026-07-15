package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/delay"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// DelayHandler は送信ディレイ（遅延送信）の API ハンドラーである。
type DelayHandler struct {
	repo        repository.Repository
	service     *delay.Service
	policy      config.MailboxPolicyConfig
	auditLogger *audit.Logger
}

// NewDelayHandler は DelayHandler を返す。
func NewDelayHandler(repo repository.Repository, service *delay.Service, policy config.MailboxPolicyConfig, auditLogger *audit.Logger) *DelayHandler {
	return &DelayHandler{repo: repo, service: service, policy: policy, auditLogger: auditLogger}
}

// HandleList は GET /api/v1/delayed を処理する。
// admin/operator は全件、viewer は自分が送信者（owner）のメールボックス分のみ。
func (h *DelayHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	filter, ok := h.ownerFilter(w, r)
	if !ok {
		return
	}
	list, err := h.repo.ListDelayedReleases(r.Context(), filter)
	if err != nil {
		slog.Error("遅延送信一覧取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "送信待ち一覧の取得に失敗しました")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// HandleCancel は POST /api/v1/delayed/{id}/cancel を処理する。
func (h *DelayHandler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, false)
}

// HandleSendNow は POST /api/v1/delayed/{id}/send-now を処理する。
func (h *DelayHandler) HandleSendNow(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, true)
}

func (h *DelayHandler) decide(w http.ResponseWriter, r *http.Request, sendNow bool) {
	id := chi.URLParam(r, "id")
	session := middleware.GetSession(r.Context())
	if session == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
		return
	}

	rel, err := h.repo.GetDelayedRelease(r.Context(), id)
	if err != nil {
		slog.Error("遅延送信取得失敗", "id", id, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
		return
	}
	if rel == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "送信待ちメールが見つかりません")
		return
	}
	if rel.Status != domain.DelayedPending {
		writeErrorResponse(w, http.StatusConflict, "CONFLICT", "この送信待ちメールは既に処理済みです")
		return
	}

	// 権限チェック: viewer は自分が送信者（owner）のメールボックスのメールのみ
	if session.Role == domain.RoleViewer {
		allowed, err := h.canActOn(r.Context(), session.User.Sub, rel.FromAddress)
		if err != nil {
			slog.Error("遅延送信権限判定失敗", "id", id, "error", err)
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "権限判定に失敗しました")
			return
		}
		if !allowed {
			writeErrorResponse(w, http.StatusForbidden, "FORBIDDEN", "権限がありません")
			return
		}
	}

	if sendNow {
		if err := h.service.Release(r.Context(), id, &session.User.Sub); err != nil {
			slog.Error("遅延送信の即時配送失敗", "id", id, "error", err)
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "配送に失敗しました")
			return
		}
	} else {
		ok, err := h.service.Cancel(r.Context(), id, session.User.Sub)
		if err != nil {
			slog.Error("遅延送信の取消失敗", "id", id, "error", err)
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取消に失敗しました")
			return
		}
		if !ok {
			writeErrorResponse(w, http.StatusConflict, "CONFLICT", "この送信待ちメールは既に処理済みです")
			return
		}
	}

	event := "delay.cancelled"
	if sendNow {
		event = "delay.sent"
	}
	h.auditLogger.Log(domain.AuditLog{
		EventType:  event,
		ActorID:    audit.StrPtr(session.User.Sub),
		ActorEmail: audit.StrPtr(session.User.Email),
		TargetType: audit.StrPtr("delayed_release"),
		TargetID:   audit.StrPtr(id),
	})

	action := "cancelled"
	if sendNow {
		action = "sent"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": action})
}

// canActOn は userID が fromAddress のメールボックスに対して送信権限（owner）を持つかを返す。
func (h *DelayHandler) canActOn(ctx context.Context, userID, fromAddress string) (bool, error) {
	ownerRoles := toAssignmentRoles(h.policy.OutboundQuarantine.ReleaseBy)
	if len(ownerRoles) == 0 {
		ownerRoles = []domain.AssignmentRole{domain.AssignmentRoleOwner}
	}
	addrs, err := h.repo.GetMailboxAddressesForUser(ctx, userID, ownerRoles)
	if err != nil {
		return false, err
	}
	for _, a := range addrs {
		if a == fromAddress {
			return true, nil
		}
	}
	return false, nil
}

// ownerFilter は viewer に対して送信者（owner）メールボックスの可視性フィルターを構築する。
func (h *DelayHandler) ownerFilter(w http.ResponseWriter, r *http.Request) (*domain.MailboxVisibilityFilter, bool) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
		return nil, false
	}
	if session.Role != domain.RoleViewer {
		return nil, true // admin/operator は全件
	}
	ownerRoles := toAssignmentRoles(h.policy.OutboundQuarantine.VisibleTo)
	outbound, err := h.repo.GetMailboxAddressesForUser(r.Context(), session.User.Sub, ownerRoles)
	if err != nil {
		slog.Error("遅延送信可視性フィルター構築失敗", "user_id", session.User.Sub, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "取得に失敗しました")
		return nil, false
	}
	return &domain.MailboxVisibilityFilter{OutboundMailboxes: outbound}, true
}
