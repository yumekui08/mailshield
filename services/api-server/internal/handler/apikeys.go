package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// APIKeysHandler は API キー管理 API を処理するハンドラーである。
type APIKeysHandler struct {
	repo        repository.Repository
	auditLogger *audit.Logger
}

// NewAPIKeysHandler は APIKeysHandler を返す。
func NewAPIKeysHandler(repo repository.Repository, auditLogger *audit.Logger) *APIKeysHandler {
	return &APIKeysHandler{repo: repo, auditLogger: auditLogger}
}

// HandleList は GET /api/v1/api-keys を処理する。
func (h *APIKeysHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	keys, err := h.repo.ListAPIKeys(r.Context())
	if err != nil {
		slog.Error("API キー一覧取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "API キー一覧の取得に失敗しました")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": keys,
		"meta": map[string]int{"total": len(keys)},
	})
}

// HandleCreate は POST /api/v1/api-keys を処理する。
// 平文キーはレスポンスに一度だけ含まれる。DB にはハッシュのみ保存する。
func (h *APIKeysHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string     `json:"name"`
		Role      string     `json:"role"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの形式が正しくありません")
		return
	}
	if req.Name == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "name は必須です")
		return
	}

	role := domain.Role(req.Role)
	if role != domain.RoleAdmin && role != domain.RoleOperator && role != domain.RoleViewer {
		role = domain.RoleViewer
	}

	// 32 バイトのランダム値で平文キーを生成する
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		slog.Error("API キー生成失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "API キーの生成に失敗しました")
		return
	}
	plainKey := "mailshield_sk_" + hex.EncodeToString(raw)

	hashBytes := sha256.Sum256([]byte(plainKey))
	keyHash := hex.EncodeToString(hashBytes[:])

	session := middleware.GetSession(r.Context())
	var createdBy *string
	if session != nil {
		id := session.User.Sub
		createdBy = &id
	}

	key := &domain.APIKey{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Role:      role,
		CreatedBy: createdBy,
		ExpiresAt: req.ExpiresAt,
		CreatedAt: time.Now(),
	}

	if err := h.repo.CreateAPIKey(r.Context(), key, keyHash); err != nil {
		slog.Error("API キー保存失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "API キーの保存に失敗しました")
		return
	}

	slog.Info("API キー作成完了", "id", key.ID, "name", key.Name, "role", key.Role)

	ip := audit.ClientIP(r)
	entry := domain.AuditLog{
		EventType:  domain.EventAPIKeyCreated,
		TargetType: audit.StrPtr("api_key"),
		TargetID:   &key.ID,
		IPAddress:  &ip,
		Detail:     map[string]any{"name": key.Name, "role": string(key.Role)},
	}
	if session != nil {
		entry.ActorID = &session.User.Sub
		entry.ActorEmail = &session.User.Email
	}
	h.auditLogger.Log(entry)

	// 平文キーはこのレスポンスにのみ含まれる
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         key.ID,
		"name":       key.Name,
		"role":       string(key.Role),
		"created_by": key.CreatedBy,
		"expires_at": key.ExpiresAt,
		"created_at": key.CreatedAt,
		"key":        plainKey,
	})
}

// HandleRevoke は DELETE /api/v1/api-keys/:id を処理する。
func (h *APIKeysHandler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.repo.RevokeAPIKey(r.Context(), id); err != nil {
		slog.Error("API キー失効失敗", "id", id, "error", err)
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "API キーが見つからないか既に失効しています")
		return
	}

	slog.Info("API キー失効完了", "id", id)

	session := middleware.GetSession(r.Context())
	ip := audit.ClientIP(r)
	entry := domain.AuditLog{
		EventType:  domain.EventAPIKeyRevoked,
		TargetType: audit.StrPtr("api_key"),
		TargetID:   &id,
		IPAddress:  &ip,
	}
	if session != nil {
		entry.ActorID = &session.User.Sub
		entry.ActorEmail = &session.User.Email
	}
	h.auditLogger.Log(entry)

	w.WriteHeader(http.StatusNoContent)
}
