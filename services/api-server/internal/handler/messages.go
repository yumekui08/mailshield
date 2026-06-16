package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
	"github.com/koizumib/mailshield/services/api-server/internal/storage"
)

// MessagesHandler はメッセージ関連のAPIハンドラーである。
type MessagesHandler struct {
	repo                repository.Repository
	storage             storage.EMLStorage
	presignedURLExpiryH int
}

// NewMessagesHandler はMessagesHandlerを返す。
func NewMessagesHandler(repo repository.Repository, stor storage.EMLStorage, presignedURLExpiryH int) *MessagesHandler {
	return &MessagesHandler{
		repo:                repo,
		storage:             stor,
		presignedURLExpiryH: presignedURLExpiryH,
	}
}

// HandleList はGET /api/v1/messages を処理する。
func (h *MessagesHandler) HandleList(w http.ResponseWriter, r *http.Request) {
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

	messages, total, err := h.repo.ListMessages(r.Context(), q)
	if err != nil {
		slog.Error("メッセージ一覧取得失敗",
			"error", err,
		)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "メッセージの取得に失敗しました")
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

// HandleGet はGET /api/v1/messages/{id} を処理する。
func (h *MessagesHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
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

	detail, err := h.repo.GetMessage(r.Context(), id)
	if err != nil {
		slog.Warn("メッセージ取得失敗",
			"message_id", id,
			"error", err,
		)
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "メッセージが見つかりません")
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// HandleGetEML はGET /api/v1/messages/{id}/eml を処理する。
// MinIO の署名付き URL を生成して JSON で返す。
func (h *MessagesHandler) HandleGetEML(w http.ResponseWriter, r *http.Request) {
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

	detail, err := h.repo.GetMessage(r.Context(), id)
	if err != nil {
		slog.Warn("EML取得失敗: メッセージが見つかりません",
			"message_id", id,
			"error", err,
		)
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "メッセージが見つかりません")
		return
	}

	expiryHours := h.presignedURLExpiryH
	if expiryHours <= 0 {
		expiryHours = 1
	}

	presignedURL, err := h.storage.GetPresignedURL(r.Context(), detail.EMLPath, expiryHours)
	if err != nil {
		slog.Error("EML署名付きURL生成失敗",
			"message_id", id,
			"eml_path", detail.EMLPath,
			"error", err,
		)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "EMLのダウンロードURLを生成できませんでした")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"url":        presignedURL,
		"expires_in": expiryHours * 3600,
	})
}

// HandleGetAttachments はメッセージに紐づく分離済み添付ファイル一覧を返す。
// GET /api/v1/messages/{id}/attachments
func (h *MessagesHandler) HandleGetAttachments(w http.ResponseWriter, r *http.Request) {
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

	atts, err := h.repo.ListAttachmentsByMessage(r.Context(), id)
	if err != nil {
		slog.Error("添付ファイル一覧取得失敗",
			"message_id", id,
			"error", err,
		)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "添付ファイルの取得に失敗しました")
		return
	}

	writeJSON(w, http.StatusOK, atts)
}

// parseListQuery はHTTPリクエストからListQueryを解析する。
func parseListQuery(r *http.Request) (domain.ListQuery, error) {
	q := domain.ListQuery{
		Page:    1,
		PerPage: 20,
		Sort:    "received_at",
		Order:   "desc",
	}

	if v := r.URL.Query().Get("page"); v != "" {
		page, err := strconv.Atoi(v)
		if err != nil || page < 1 {
			return q, fmt.Errorf("pageは1以上の整数を指定してください")
		}
		q.Page = page
	}

	if v := r.URL.Query().Get("per_page"); v != "" {
		perPage, err := strconv.Atoi(v)
		if err != nil || perPage < 1 || perPage > 100 {
			return q, fmt.Errorf("per_pageは1〜100の整数を指定してください")
		}
		q.PerPage = perPage
	}

	if v := r.URL.Query().Get("status"); v != "" {
		q.Status = v
	}

	if v := r.URL.Query().Get("from"); v != "" {
		q.From = v
	}

	if v := r.URL.Query().Get("to"); v != "" {
		q.To = v
	}

	if v := r.URL.Query().Get("subject"); v != "" {
		q.Subject = v
	}

	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return q, fmt.Errorf("sinceはISO 8601形式で指定してください（例: 2006-01-02T15:04:05Z）")
		}
		q.Since = &t
	}

	if v := r.URL.Query().Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return q, fmt.Errorf("untilはISO 8601形式で指定してください（例: 2006-01-02T15:04:05Z）")
		}
		q.Until = &t
	}

	if v := r.URL.Query().Get("has_attachment"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return q, fmt.Errorf("has_attachmentはtrueまたはfalseを指定してください")
		}
		q.HasAttachment = &b
	}

	if v := r.URL.Query().Get("sort"); v != "" {
		allowed := map[string]bool{
			"received_at":  true,
			"subject":      true,
			"from_address": true,
			"size_bytes":   true,
		}
		if !allowed[v] {
			return q, fmt.Errorf("sortはreceived_at, subject, from_address, size_bytesのいずれかを指定してください")
		}
		q.Sort = v
	}

	if v := r.URL.Query().Get("order"); v != "" {
		if v != "asc" && v != "desc" {
			return q, fmt.Errorf("orderはascまたはdescを指定してください")
		}
		q.Order = v
	}

	return q, nil
}

// calcTotalPages は総ページ数を計算する。
func calcTotalPages(total, perPage int) int {
	if perPage == 0 {
		return 0
	}
	pages := total / perPage
	if total%perPage != 0 {
		pages++
	}
	return pages
}

// writeJSON はJSON形式のレスポンスを書き込む。
func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(v)
}
