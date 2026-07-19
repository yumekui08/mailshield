package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// MailboxesHandler はメールボックス管理 API を処理するハンドラーである。
type MailboxesHandler struct {
	repo        repository.Repository
	auditLogger *audit.Logger
}

// NewMailboxesHandler は MailboxesHandler を返す。
func NewMailboxesHandler(repo repository.Repository, auditLogger *audit.Logger) *MailboxesHandler {
	return &MailboxesHandler{repo: repo, auditLogger: auditLogger}
}

type mailboxResponse struct {
	ID           string `json:"id"`
	EmailAddress string `json:"email_address"`
	DisplayName  string `json:"display_name"`
	IsActive     bool   `json:"is_active"`
	// AssignmentSummary は一覧画面向けの role 別割り当て概要（人数 + 先頭数人）。
	AssignmentSummary []roleSummaryResponse `json:"assignment_summary"`
}

type roleSummaryResponse struct {
	Role   string                `json:"role"`
	Count  int                   `json:"count"`
	Sample []summaryUserResponse `json:"sample"`
}

type summaryUserResponse struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

type assignmentResponse struct {
	ID              string `json:"id"`
	MailboxID       string `json:"mailbox_id"`
	UserID          string `json:"user_id"`
	Role            string `json:"role"`
	UserEmail       string `json:"user_email"`
	UserDisplayName string `json:"user_display_name"`
}

func toMailboxResponse(m repository.Mailbox) mailboxResponse {
	return mailboxResponse{
		ID:           m.ID,
		EmailAddress: m.EmailAddress,
		DisplayName:  m.DisplayName,
		IsActive:     m.IsActive,
	}
}

func toAssignmentResponse(a repository.MailboxAssignment) assignmentResponse {
	return assignmentResponse{
		ID:              a.ID,
		MailboxID:       a.MailboxID,
		UserID:          a.UserID,
		Role:            string(a.Role),
		UserEmail:       a.UserEmail,
		UserDisplayName: a.UserDisplayName,
	}
}

// HandleList は GET /api/v1/mailboxes を処理する。
// クエリパラメータで絞り込み・ページングできる:
//
//	q               email_address / display_name の部分一致
//	assigned_user_id 指定ユーザーが割り当てられたメールボックスに限定
//	provisioned_by   manual | ldap | scim
//	active           true | false（有効状態）
//	missing_role     member | owner | approver（そのロールの有効ユーザーが未割り当て）
//	limit / offset   ページング（limit 既定 50・上限 200）
func (h *MailboxesHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	filter := parseMailboxFilter(r)

	mailboxes, total, err := h.repo.SearchMailboxes(r.Context(), filter)
	if err != nil {
		slog.Error("メールボックス検索失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "メールボックス一覧の取得に失敗しました")
		return
	}

	// 割り当てサマリ（人数 + 先頭 3 人）。取得失敗はサマリ空で続行する。
	summaries, err := h.repo.ListAssignmentSummaries(r.Context(), 3)
	if err != nil {
		slog.Warn("割り当てサマリ取得失敗（サマリなしで続行）", "error", err)
		summaries = map[string][]repository.MailboxRoleSummary{}
	}

	resp := make([]mailboxResponse, len(mailboxes))
	for i, m := range mailboxes {
		resp[i] = toMailboxResponse(m)
		resp[i].AssignmentSummary = toRoleSummaryResponses(summaries[m.ID])
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": resp,
		"meta": map[string]any{
			"total":  total,
			"limit":  filter.Limit,
			"offset": filter.Offset,
		},
	})
}

// parseMailboxFilter はクエリパラメータを MailboxSearchFilter に変換する。
// 不正な列挙値・数値は無視して既定挙動にフォールバックする（検索は寛容に扱う）。
func parseMailboxFilter(r *http.Request) repository.MailboxSearchFilter {
	q := r.URL.Query()
	f := repository.MailboxSearchFilter{
		Query:          q.Get("q"),
		AssignedUserID: q.Get("assigned_user_id"),
		Limit:          atoiDefault(q.Get("limit"), 50),
		Offset:         atoiDefault(q.Get("offset"), 0),
	}
	switch domain.ProvisionedBy(q.Get("provisioned_by")) {
	case domain.ProvisionedByManual, domain.ProvisionedByLDAP, domain.ProvisionedBySCIM:
		f.ProvisionedBy = domain.ProvisionedBy(q.Get("provisioned_by"))
	}
	switch domain.AssignmentRole(q.Get("missing_role")) {
	case domain.AssignmentRoleMember, domain.AssignmentRoleOwner, domain.AssignmentRoleApprover:
		f.MissingRole = domain.AssignmentRole(q.Get("missing_role"))
	}
	switch q.Get("active") {
	case "true":
		v := true
		f.Active = &v
	case "false":
		v := false
		f.Active = &v
	}
	return f
}

// atoiDefault は s を整数に変換し、失敗したら def を返す。
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func toRoleSummaryResponses(summaries []repository.MailboxRoleSummary) []roleSummaryResponse {
	out := make([]roleSummaryResponse, 0, len(summaries))
	for _, s := range summaries {
		users := make([]summaryUserResponse, 0, len(s.Sample))
		for _, u := range s.Sample {
			users = append(users, summaryUserResponse{Email: u.Email, DisplayName: u.DisplayName})
		}
		out = append(out, roleSummaryResponse{Role: string(s.Role), Count: s.Count, Sample: users})
	}
	return out
}

// HandleCreate は POST /api/v1/mailboxes を処理する。
func (h *MailboxesHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EmailAddress string `json:"email_address"`
		DisplayName  string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの形式が正しくありません")
		return
	}
	if req.EmailAddress == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "email_address は必須です")
		return
	}

	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.EmailAddress
	}

	mailbox := &repository.Mailbox{
		ID:           uuid.New().String(),
		EmailAddress: req.EmailAddress,
		DisplayName:  displayName,
		IsActive:     true,
	}
	if err := h.repo.CreateMailbox(r.Context(), mailbox); err != nil {
		slog.Error("メールボックス作成失敗", "email", req.EmailAddress, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "メールボックス作成に失敗しました")
		return
	}

	slog.Info("メールボックス作成完了", "email", mailbox.EmailAddress)

	session := middleware.GetSession(r.Context())
	ip := audit.ClientIP(r)
	entry := domain.AuditLog{
		EventType:  domain.EventMailboxCreated,
		TargetType: audit.StrPtr("mailbox"),
		TargetID:   &mailbox.ID,
		IPAddress:  &ip,
		Detail:     map[string]any{"email_address": mailbox.EmailAddress},
	}
	if session != nil {
		entry.ActorID = &session.User.Sub
		entry.ActorEmail = &session.User.Email
	}
	h.auditLogger.Log(entry)

	writeJSON(w, http.StatusCreated, toMailboxResponse(*mailbox))
}

// HandleUpdate は PATCH /api/v1/mailboxes/:id を処理する。
func (h *MailboxesHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	mailboxID := chi.URLParam(r, "id")

	existing, err := h.repo.GetMailbox(r.Context(), mailboxID)
	if err != nil || existing == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "メールボックスが見つかりません")
		return
	}

	var req struct {
		DisplayName *string `json:"display_name"`
		IsActive    *bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの形式が正しくありません")
		return
	}

	displayName := existing.DisplayName
	if req.DisplayName != nil {
		displayName = *req.DisplayName
	}
	isActive := existing.IsActive
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	if err := h.repo.UpdateMailbox(r.Context(), mailboxID, displayName, isActive); err != nil {
		slog.Error("メールボックス更新失敗", "mailbox_id", mailboxID, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "メールボックス更新に失敗しました")
		return
	}

	existing.DisplayName = displayName
	existing.IsActive = isActive

	session := middleware.GetSession(r.Context())
	ip := audit.ClientIP(r)
	upEntry := domain.AuditLog{
		EventType:  domain.EventMailboxUpdated,
		TargetType: audit.StrPtr("mailbox"),
		TargetID:   &mailboxID,
		IPAddress:  &ip,
		Detail:     map[string]any{"display_name": displayName, "is_active": isActive},
	}
	if session != nil {
		upEntry.ActorID = &session.User.Sub
		upEntry.ActorEmail = &session.User.Email
	}
	h.auditLogger.Log(upEntry)

	writeJSON(w, http.StatusOK, toMailboxResponse(*existing))
}

// HandleDelete は DELETE /api/v1/mailboxes/:id を処理する。
func (h *MailboxesHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	mailboxID := chi.URLParam(r, "id")

	existing, err := h.repo.GetMailbox(r.Context(), mailboxID)
	if err != nil || existing == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "メールボックスが見つかりません")
		return
	}

	if err := h.repo.DeleteMailbox(r.Context(), mailboxID); err != nil {
		slog.Error("メールボックス削除失敗", "mailbox_id", mailboxID, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "メールボックス削除に失敗しました")
		return
	}

	slog.Info("メールボックス削除完了", "mailbox_id", mailboxID)

	session := middleware.GetSession(r.Context())
	ip := audit.ClientIP(r)
	delEntry := domain.AuditLog{
		EventType:  domain.EventMailboxDeleted,
		TargetType: audit.StrPtr("mailbox"),
		TargetID:   &mailboxID,
		IPAddress:  &ip,
		Detail:     map[string]any{"email_address": existing.EmailAddress},
	}
	if session != nil {
		delEntry.ActorID = &session.User.Sub
		delEntry.ActorEmail = &session.User.Email
	}
	h.auditLogger.Log(delEntry)

	w.WriteHeader(http.StatusNoContent)
}

// HandleListAssignments は GET /api/v1/mailboxes/:id/assignments を処理する。
func (h *MailboxesHandler) HandleListAssignments(w http.ResponseWriter, r *http.Request) {
	mailboxID := chi.URLParam(r, "id")

	if existing, _ := h.repo.GetMailbox(r.Context(), mailboxID); existing == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "メールボックスが見つかりません")
		return
	}

	assignments, err := h.repo.ListAssignments(r.Context(), mailboxID)
	if err != nil {
		slog.Error("割り当て一覧取得失敗", "mailbox_id", mailboxID, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "割り当て一覧の取得に失敗しました")
		return
	}

	resp := make([]assignmentResponse, len(assignments))
	for i, a := range assignments {
		resp[i] = toAssignmentResponse(a)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": resp,
		"meta": map[string]int{"total": len(resp)},
	})
}

// HandleAddAssignment は POST /api/v1/mailboxes/:id/assignments を処理する。
func (h *MailboxesHandler) HandleAddAssignment(w http.ResponseWriter, r *http.Request) {
	mailboxID := chi.URLParam(r, "id")

	if existing, _ := h.repo.GetMailbox(r.Context(), mailboxID); existing == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "メールボックスが見つかりません")
		return
	}

	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの形式が正しくありません")
		return
	}
	if req.UserID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "user_id は必須です")
		return
	}

	role := domain.AssignmentRole(req.Role)
	if role != domain.AssignmentRoleMember && role != domain.AssignmentRoleOwner && role != domain.AssignmentRoleApprover {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "role は member/owner/approver のいずれかを指定してください")
		return
	}

	// 対象ユーザーが存在するか確認
	users, err := h.repo.ListUsers(r.Context())
	if err != nil {
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザー確認に失敗しました")
		return
	}
	found := false
	for _, u := range users {
		if u.ID == req.UserID {
			found = true
			break
		}
	}
	if !found {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "ユーザーが見つかりません")
		return
	}

	assignment := &repository.MailboxAssignment{
		ID:        uuid.New().String(),
		MailboxID: mailboxID,
		UserID:    req.UserID,
		Role:      role,
	}
	if err := h.repo.AddAssignment(r.Context(), assignment); err != nil {
		slog.Error("割り当て追加失敗", "mailbox_id", mailboxID, "user_id", req.UserID, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "割り当て追加に失敗しました")
		return
	}

	slog.Info("割り当て追加完了", "mailbox_id", mailboxID, "user_id", req.UserID, "role", role)

	session := middleware.GetSession(r.Context())
	ip := audit.ClientIP(r)
	addEntry := domain.AuditLog{
		EventType:  domain.EventMailboxAssignmentAdded,
		TargetType: audit.StrPtr("mailbox"),
		TargetID:   &mailboxID,
		IPAddress:  &ip,
		Detail:     map[string]any{"user_id": req.UserID, "role": string(role)},
	}
	if session != nil {
		addEntry.ActorID = &session.User.Sub
		addEntry.ActorEmail = &session.User.Email
	}
	h.auditLogger.Log(addEntry)

	writeJSON(w, http.StatusCreated, toAssignmentResponse(*assignment))
}

// HandleRemoveAssignment は DELETE /api/v1/mailboxes/:id/assignments を処理する。
// body に user_id と role を受け取る。
func (h *MailboxesHandler) HandleRemoveAssignment(w http.ResponseWriter, r *http.Request) {
	mailboxID := chi.URLParam(r, "id")

	if existing, _ := h.repo.GetMailbox(r.Context(), mailboxID); existing == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "メールボックスが見つかりません")
		return
	}

	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの形式が正しくありません")
		return
	}
	if req.UserID == "" || req.Role == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "user_id と role は必須です")
		return
	}

	role := domain.AssignmentRole(req.Role)
	if err := h.repo.RemoveAssignment(r.Context(), mailboxID, req.UserID, role); err != nil {
		slog.Error("割り当て削除失敗", "mailbox_id", mailboxID, "user_id", req.UserID, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "割り当て削除に失敗しました")
		return
	}

	slog.Info("割り当て削除完了", "mailbox_id", mailboxID, "user_id", req.UserID, "role", role)

	session := middleware.GetSession(r.Context())
	ip := audit.ClientIP(r)
	rmEntry := domain.AuditLog{
		EventType:  domain.EventMailboxAssignmentRemoved,
		TargetType: audit.StrPtr("mailbox"),
		TargetID:   &mailboxID,
		IPAddress:  &ip,
		Detail:     map[string]any{"user_id": req.UserID, "role": string(role)},
	}
	if session != nil {
		rmEntry.ActorID = &session.User.Sub
		rmEntry.ActorEmail = &session.User.Email
	}
	h.auditLogger.Log(rmEntry)

	w.WriteHeader(http.StatusNoContent)
}
