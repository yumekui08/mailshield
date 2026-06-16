package handler

import (
	"log/slog"
	"net/http"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// StatsHandler はダッシュボード統計 API のハンドラーである。
type StatsHandler struct {
	repo   repository.Repository
	policy config.MailboxPolicyConfig
}

// NewStatsHandler は StatsHandler を返す。
func NewStatsHandler(repo repository.Repository, policy config.MailboxPolicyConfig) *StatsHandler {
	return &StatsHandler{repo: repo, policy: policy}
}

// HandleGet は GET /api/v1/stats を処理する。
// admin/operator は全体の統計を返す。viewer はメールボックス可視性でフィルタした統計を返す。
func (h *StatsHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
		return
	}

	var filter *domain.MailboxVisibilityFilter
	if session.Role == domain.RoleViewer {
		inboundRoles := toAssignmentRoles(h.policy.InboundQuarantine.VisibleTo)
		outboundRoles := toAssignmentRoles(h.policy.OutboundQuarantine.VisibleTo)

		inbound, err := h.repo.GetMailboxAddressesForUser(r.Context(), session.User.Sub, inboundRoles)
		if err != nil {
			slog.Error("stats visibility filter 構築失敗", "user_id", session.User.Sub, "error", err)
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "統計の取得に失敗しました")
			return
		}
		outbound, err := h.repo.GetMailboxAddressesForUser(r.Context(), session.User.Sub, outboundRoles)
		if err != nil {
			slog.Error("stats visibility filter 構築失敗", "user_id", session.User.Sub, "error", err)
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "統計の取得に失敗しました")
			return
		}
		filter = &domain.MailboxVisibilityFilter{
			InboundMailboxes:  inbound,
			OutboundMailboxes: outbound,
		}
	}

	stats, err := h.repo.GetStats(r.Context(), filter)
	if err != nil {
		slog.Error("統計取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "統計の取得に失敗しました")
		return
	}

	writeJSON(w, http.StatusOK, stats)
}
