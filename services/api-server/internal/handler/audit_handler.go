package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// AuditHandler は監査ログ閲覧 API のハンドラーである。
type AuditHandler struct {
	repo repository.Repository
}

// NewAuditHandler は AuditHandler を返す。
func NewAuditHandler(repo repository.Repository) *AuditHandler {
	return &AuditHandler{repo: repo}
}

// HandleList は GET /api/v1/audit-logs を処理する。admin のみ利用可。
func (h *AuditHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	q := domain.AuditLogQuery{
		Page:      1,
		PerPage:   50,
		EventType: r.URL.Query().Get("event_type"),
		ActorID:   r.URL.Query().Get("actor_id"),
		FromDate:  r.URL.Query().Get("from_date"),
		ToDate:    r.URL.Query().Get("to_date"),
	}
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			q.Page = n
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			q.PerPage = n
		}
	}

	logs, total, err := h.repo.ListAuditLogs(r.Context(), q)
	if err != nil {
		slog.Error("監査ログ一覧取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "監査ログの取得に失敗しました")
		return
	}

	totalPages := calcTotalPages(total, q.PerPage)
	writeJSON(w, http.StatusOK, domain.PagedResult[domain.AuditLog]{
		Data: logs,
		Meta: domain.PageMeta{
			Total:      total,
			Page:       q.Page,
			PerPage:    q.PerPage,
			TotalPages: totalPages,
		},
	})
}
