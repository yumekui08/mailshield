package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/auth"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// UsersHandler はユーザー管理 API を処理するハンドラーである。
type UsersHandler struct {
	repo        repository.Repository
	auditLogger *audit.Logger
}

// NewUsersHandler は UsersHandler を返す。
func NewUsersHandler(repo repository.Repository, auditLogger *audit.Logger) *UsersHandler {
	return &UsersHandler{repo: repo, auditLogger: auditLogger}
}

type userResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	IsActive    bool   `json:"is_active"`
}

func toUserResponse(u repository.User) userResponse {
	return userResponse{
		ID:          u.ID,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Role:        string(u.Role),
		IsActive:    u.IsActive,
	}
}

// HandleList は GET /api/v1/users を処理する。
func (h *UsersHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	users, err := h.repo.ListUsers(r.Context())
	if err != nil {
		slog.Error("ユーザー一覧取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザー一覧の取得に失敗しました")
		return
	}

	resp := make([]userResponse, len(users))
	for i, u := range users {
		resp[i] = toUserResponse(u)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": resp,
		"meta": map[string]int{"total": len(resp)},
	})
}

// HandleSearch は GET /api/v1/users/search?q=&limit= を処理する（operator 以上）。
// UserPicker（メールボックス割り当て等）の検索・複数選択に使う。最小フィールドのみ返す。
func (h *UsersHandler) HandleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	users, err := h.repo.SearchUsers(r.Context(), q, limit)
	if err != nil {
		slog.Error("ユーザー検索失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザー検索に失敗しました")
		return
	}
	resp := make([]userResponse, len(users))
	for i, u := range users {
		resp[i] = toUserResponse(u)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp, "meta": map[string]int{"total": len(resp)}})
}

// HandleCreate は POST /api/v1/users を処理する。
func (h *UsersHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの形式が正しくありません")
		return
	}
	if req.Email == "" || req.Password == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "emailとpasswordは必須です")
		return
	}
	if len(req.Password) < 8 {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "パスワードは8文字以上にしてください")
		return
	}

	role := domain.Role(req.Role)
	if role != domain.RoleAdmin && role != domain.RoleOperator && role != domain.RoleViewer {
		role = domain.RoleViewer
	}

	// メールアドレス重複チェック
	if existing, _ := h.repo.FindUserByEmail(r.Context(), req.Email); existing != nil {
		writeErrorResponse(w, http.StatusConflict, "CONFLICT", "そのメールアドレスはすでに使用されています")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		slog.Error("パスワードハッシュ生成失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザー作成に失敗しました")
		return
	}

	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.Email
	}

	user := &repository.User{
		ID:           uuid.New().String(),
		Email:        req.Email,
		DisplayName:  displayName,
		PasswordHash: hash,
		Role:         role,
		IsActive:     true,
	}
	if err := h.repo.CreateUser(r.Context(), user); err != nil {
		slog.Error("ユーザー作成失敗", "email", req.Email, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザー作成に失敗しました")
		return
	}

	slog.Info("ユーザー作成完了", "email", user.Email, "role", user.Role)

	session := middleware.GetSession(r.Context())
	ip := audit.ClientIP(r)
	entry := domain.AuditLog{
		EventType:  domain.EventUserCreated,
		TargetType: audit.StrPtr("user"),
		TargetID:   &user.ID,
		IPAddress:  &ip,
		Detail:     map[string]any{"email": user.Email, "role": string(user.Role)},
	}
	if session != nil {
		entry.ActorID = &session.User.Sub
		entry.ActorEmail = &session.User.Email
	}
	h.auditLogger.Log(entry)

	writeJSON(w, http.StatusCreated, toUserResponse(*user))
}

// HandleUpdate は PATCH /api/v1/users/:id を処理する。
// role と password を個別または同時に変更できる。
func (h *UsersHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")

	var req struct {
		Role        *string `json:"role"`
		Password    *string `json:"password"`
		DisplayName *string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの形式が正しくありません")
		return
	}
	if req.Role == nil && req.Password == nil && req.DisplayName == nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "role, password, display_name のいずれかを指定してください")
		return
	}

	// 対象ユーザーが存在するか確認
	users, err := h.repo.ListUsers(r.Context())
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザー取得に失敗しました")
		return
	}
	var target *repository.User
	for i := range users {
		if users[i].ID == userID {
			target = &users[i]
			break
		}
	}
	if target == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "ユーザーが見つかりません")
		return
	}

	if req.Role != nil {
		role := domain.Role(*req.Role)
		if role != domain.RoleAdmin && role != domain.RoleOperator && role != domain.RoleViewer {
			writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "roleはadmin, operator, viewerのいずれかを指定してください")
			return
		}
		if err := h.repo.UpdateUserRole(r.Context(), userID, role); err != nil {
			slog.Error("ロール更新失敗", "user_id", userID, "error", err)
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ロール変更に失敗しました")
			return
		}
		target.Role = role
	}

	if req.Password != nil {
		if len(*req.Password) < 8 {
			writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "パスワードは8文字以上にしてください")
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "パスワード変更に失敗しました")
			return
		}
		if err := h.repo.UpdateUserPassword(r.Context(), userID, hash); err != nil {
			slog.Error("パスワード更新失敗", "user_id", userID, "error", err)
			writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "パスワード変更に失敗しました")
			return
		}
	}

	slog.Info("ユーザー更新完了", "user_id", userID)

	updateSession := middleware.GetSession(r.Context())
	updateIP := audit.ClientIP(r)
	if req.Role != nil {
		e := domain.AuditLog{
			EventType:  domain.EventUserRoleChanged,
			TargetType: audit.StrPtr("user"),
			TargetID:   &userID,
			IPAddress:  &updateIP,
			Detail:     map[string]any{"new_role": *req.Role},
		}
		if updateSession != nil {
			e.ActorID = &updateSession.User.Sub
			e.ActorEmail = &updateSession.User.Email
		}
		h.auditLogger.Log(e)
	}
	if req.Password != nil {
		e := domain.AuditLog{
			EventType:  domain.EventUserPasswordChanged,
			TargetType: audit.StrPtr("user"),
			TargetID:   &userID,
			IPAddress:  &updateIP,
		}
		if updateSession != nil {
			e.ActorID = &updateSession.User.Sub
			e.ActorEmail = &updateSession.User.Email
		}
		h.auditLogger.Log(e)
	}

	writeJSON(w, http.StatusOK, toUserResponse(*target))
}

// HandleDelete は DELETE /api/v1/users/:id を処理する。
func (h *UsersHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	userID := chi.URLParam(r, "id")

	// 自分自身は削除不可
	if session != nil && session.User.Sub == userID {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "自分自身は削除できません")
		return
	}

	// 対象ユーザーが存在するか確認
	users, err := h.repo.ListUsers(r.Context())
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザー取得に失敗しました")
		return
	}
	found := false
	for _, u := range users {
		if u.ID == userID {
			found = true
			break
		}
	}
	if !found {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "ユーザーが見つかりません")
		return
	}

	if err := h.repo.DeleteUser(r.Context(), userID); err != nil {
		slog.Error("ユーザー削除失敗", "user_id", userID, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザー削除に失敗しました")
		return
	}

	slog.Info("ユーザー削除完了", "user_id", userID)

	ip := audit.ClientIP(r)
	e := domain.AuditLog{
		EventType:  domain.EventUserDeleted,
		TargetType: audit.StrPtr("user"),
		TargetID:   &userID,
		IPAddress:  &ip,
	}
	if session != nil {
		e.ActorID = &session.User.Sub
		e.ActorEmail = &session.User.Email
	}
	h.auditLogger.Log(e)

	w.WriteHeader(http.StatusNoContent)
}
