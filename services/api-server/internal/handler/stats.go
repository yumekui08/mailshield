package handler

import (
	"log/slog"
	"net/http"
	"strconv"

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
	filter, ok := h.visibilityFilter(w, r)
	if !ok {
		return
	}

	stats, err := h.repo.GetStats(r.Context(), filter)
	if err != nil {
		slog.Error("統計取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "統計の取得に失敗しました")
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// HandleTimeseries は GET /api/v1/stats/timeseries を処理する。
// クエリパラメータ days（デフォルト 14・最大 90）で期間を指定する。
// 日別の処理件数を古い日付から順に返す（メールがない日も件数 0 で含む）。
func (h *StatsHandler) HandleTimeseries(w http.ResponseWriter, r *http.Request) {
	days := 14
	if raw := r.URL.Query().Get("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 90 {
			writeErrorResponse(w, http.StatusBadRequest, "INVALID_PARAMETER", "days は 1〜90 の整数で指定してください")
			return
		}
		days = n
	}

	filter, ok := h.visibilityFilter(w, r)
	if !ok {
		return
	}

	points, err := h.repo.GetStatsTimeseries(r.Context(), days, filter)
	if err != nil {
		slog.Error("日別統計取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "統計の取得に失敗しました")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": points})
}

// visibilityFilter はセッションのロールに応じた可視性フィルターを構築する。
// viewer 以外は nil（制限なし）。失敗時はエラーレスポンスを書き込み ok=false を返す。
func (h *StatsHandler) visibilityFilter(w http.ResponseWriter, r *http.Request) (*domain.MailboxVisibilityFilter, bool) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
		return nil, false
	}

	var filter *domain.MailboxVisibilityFilter
	if session.Role == domain.RoleViewer {
		inboundRoles := toAssignmentRoles(h.policy.InboundQuarantine.VisibleTo)
		outboundRoles := toAssignmentRoles(h.policy.OutboundQuarantine.VisibleTo)

		inbound, err := h.repo.GetMailboxAddressesForUser(r.Context(), session.User.Sub, inboundRoles)
		if err != nil {
			slog.Error("stats visibility filter 構築失敗", "user_id", session.User.Sub, "error", err)
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "統計の取得に失敗しました")
			return nil, false
		}
		outbound, err := h.repo.GetMailboxAddressesForUser(r.Context(), session.User.Sub, outboundRoles)
		if err != nil {
			slog.Error("stats visibility filter 構築失敗", "user_id", session.User.Sub, "error", err)
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "統計の取得に失敗しました")
			return nil, false
		}
		filter = &domain.MailboxVisibilityFilter{
			InboundMailboxes:  inbound,
			OutboundMailboxes: outbound,
		}
	}

	return filter, true
}
