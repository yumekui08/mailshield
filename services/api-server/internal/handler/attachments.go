package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/otp"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
	"github.com/koizumib/mailshield/services/api-server/internal/storage"
)

// AttachmentsHandler は添付ファイルのダウンロード・管理エンドポイントを実装する。
type AttachmentsHandler struct {
	repo           repository.Repository
	attachmentStor storage.AttachmentStorage
	cfg            config.AttachmentDownloadConfig
	otpStore       *otp.Store
	notifCfg       config.NotificationConfig
}

// NewAttachmentsHandler は AttachmentsHandler を生成する。
func NewAttachmentsHandler(
	repo repository.Repository,
	attachmentStor storage.AttachmentStorage,
	cfg config.AttachmentDownloadConfig,
	otpStore *otp.Store,
	notifCfg config.NotificationConfig,
) *AttachmentsHandler {
	return &AttachmentsHandler{
		repo:           repo,
		attachmentStor: attachmentStor,
		cfg:            cfg,
		otpStore:       otpStore,
		notifCfg:       notifCfg,
	}
}

// HandleList はダウンロードトークンに紐づく添付ファイル一覧を返す（mode=auth のときメールボックスロールを確認）。
// GET /api/v1/attachments/{token}
func (h *AttachmentsHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	session := middleware.GetSession(r.Context())

	atts, err := h.repo.ListAttachmentsByToken(r.Context(), token)
	if err != nil {
		slog.Error("添付ファイル一覧取得失敗", "token", token, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "添付ファイル一覧取得失敗")
		return
	}

	// mode=auth: ユーザーのメールボックスロールを確認する
	if len(atts) > 0 && atts[0].DownloadMode == domain.DownloadModeAuth {
		if !h.checkMailboxRole(w, r, session.User.Sub, token) {
			return
		}
	}

	writeJSON(w, http.StatusOK, atts)
}

// HandleDownload は添付ファイルを MinIO からプロキシストリームする（mode=auth のときメールボックスロールを確認）。
// GET /api/v1/attachments/{token}/{filename}
func (h *AttachmentsHandler) HandleDownload(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	filename := chi.URLParam(r, "filename")
	session := middleware.GetSession(r.Context())

	att, err := h.repo.GetAttachmentByToken(r.Context(), token, filename)
	if err != nil {
		slog.Error("添付ファイル取得失敗", "token", token, "filename", filename, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "添付ファイル取得失敗")
		return
	}
	if att == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "添付ファイルが見つかりません")
		return
	}
	if att.IsDisabled {
		writeErrorResponse(w, http.StatusForbidden, "FORBIDDEN", "このファイルのダウンロードは無効化されています")
		return
	}

	// mode=auth: ユーザーのメールボックスロールを確認する
	if att.DownloadMode == domain.DownloadModeAuth {
		if !h.checkMailboxRole(w, r, session.User.Sub, token) {
			return
		}
	}

	h.streamAttachment(w, r, att.StoragePath, filename)
}

// HandlePublicList はトークンのみで添付ファイル一覧を返す（認証不要）。
// mode に応じてレスポンス内容が異なる:
//   - simple: {mode:"simple", attachments:[...]}
//   - auth:   {mode:"auth",   attachments:null} → フロントエンドはログイン画面へリダイレクト
//   - otp:    {mode:"otp",    attachments:[...]} OTP セッション有り / {mode:"otp", attachments:null} セッション無し
//
// GET /api/v1/public/attachments/{token}[?otp_session={session_id}]
func (h *AttachmentsHandler) HandlePublicList(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	atts, err := h.repo.ListAttachmentsByTokenPublic(r.Context(), token)
	if err != nil {
		slog.Error("添付ファイル一覧取得失敗（public）", "token", token, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "添付ファイル一覧取得失敗")
		return
	}
	if len(atts) == 0 {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "添付ファイルが見つかりません")
		return
	}

	mode := atts[0].DownloadMode
	type publicListResponse struct {
		Mode        domain.DownloadMode  `json:"mode"`
		Attachments *[]domain.Attachment `json:"attachments"`
	}

	resp := publicListResponse{Mode: mode}
	switch mode {
	case domain.DownloadModeSimple:
		resp.Attachments = &atts
	case domain.DownloadModeOTP:
		// OTP セッションが有効なら一覧を返す
		if sessID := r.URL.Query().Get("otp_session"); sessID != "" {
			if _, err := h.otpStore.ValidateSession(r.Context(), sessID, token); err == nil {
				resp.Attachments = &atts
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandlePublicDownload はトークンで添付ファイルをダウンロードする。
//   - mode=simple: 認証不要
//   - mode=otp:    ?otp_session={session_id} による OTP セッション検証が必要
//   - mode=auth:   認証済みユーザーのみ（別エンドポイント /attachments/{token}/{filename}）
//
// GET /api/v1/public/attachments/{token}/{filename}[?otp_session={session_id}]
func (h *AttachmentsHandler) HandlePublicDownload(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	filename := chi.URLParam(r, "filename")

	att, err := h.repo.GetAttachmentByTokenPublic(r.Context(), token, filename)
	if err != nil {
		slog.Error("添付ファイル取得失敗（public）", "token", token, "filename", filename, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "添付ファイル取得失敗")
		return
	}
	if att == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "添付ファイルが見つかりません")
		return
	}
	if att.IsDisabled {
		writeErrorResponse(w, http.StatusForbidden, "FORBIDDEN", "このファイルのダウンロードは無効化されています")
		return
	}

	switch att.DownloadMode {
	case domain.DownloadModeSimple:
		// 認証不要
	case domain.DownloadModeAuth:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"code": "AUTH_REQUIRED",
			"mode": string(domain.DownloadModeAuth),
		})
		return
	case domain.DownloadModeOTP:
		sessID := r.URL.Query().Get("otp_session")
		if sessID == "" {
			writeErrorResponse(w, http.StatusUnauthorized, "OTP_REQUIRED", "OTP 認証が必要です")
			return
		}
		if _, err := h.otpStore.ValidateSession(r.Context(), sessID, token); err != nil {
			writeErrorResponse(w, http.StatusUnauthorized, "OTP_REQUIRED", "OTP セッションが無効です")
			return
		}
	default:
		writeErrorResponse(w, http.StatusForbidden, "FORBIDDEN", "このファイルへのアクセス方式が不明です")
		return
	}

	h.streamAttachment(w, r, att.StoragePath, filename)
}

// HandleOTPRequest はダウンロードトークンに紐づくメールアドレスへ OTP コードを送信する。
// リクエストのメールアドレスが元メールの受信者に含まれない場合は 400 を返す。
//
// POST /api/v1/public/attachments/{token}/otp/request
func (h *AttachmentsHandler) HandleOTPRequest(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "email が必要です")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))

	// 元メールの受信者に含まれるか確認
	recipients, err := h.repo.GetAttachmentToAddressesByToken(r.Context(), token)
	if err != nil {
		slog.Error("添付受信者取得失敗", "token", token, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "処理に失敗しました")
		return
	}
	if len(recipients) == 0 {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "ファイルが見つかりません")
		return
	}
	if !containsEmail(recipients, email) {
		writeErrorResponse(w, http.StatusBadRequest, "EMAIL_NOT_ALLOWED", "このファイルへのアクセス権がありません")
		return
	}

	// OTP コード生成・Redis 保存
	code, err := h.otpStore.GenerateCode(r.Context(), token, email)
	if err != nil {
		slog.Error("OTP コード生成失敗", "token", token, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "OTP の生成に失敗しました")
		return
	}

	// OTP メール送信
	if err := h.sendOTPEmail(email, code); err != nil {
		slog.Warn("OTP メール送信失敗（続行）", "token", token, "email", email, "error", err)
		// メール送信失敗でもコードは Redis に保存済みなので処理を続行しない
		writeErrorResponse(w, http.StatusInternalServerError, "MAIL_SEND_FAILED", "OTP メールの送信に失敗しました")
		return
	}

	slog.Info("OTP コード送信", "token", token, "email", email)
	writeJSON(w, http.StatusOK, map[string]string{"message": "OTP を送信しました"})
}

// HandleOTPVerify は OTP コードを検証し、有効な場合に OTP セッション ID を返す。
//
// POST /api/v1/public/attachments/{token}/otp/verify
func (h *AttachmentsHandler) HandleOTPVerify(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	var body struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" || body.Code == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "email と code が必要です")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))

	sessID, err := h.otpStore.Verify(r.Context(), token, email, body.Code)
	if err != nil {
		if errors.Is(err, otp.ErrTooManyAttempts) {
			writeErrorResponse(w, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", err.Error())
			return
		}
		writeErrorResponse(w, http.StatusUnauthorized, "INVALID_CODE", err.Error())
		return
	}

	slog.Info("OTP 認証成功", "token", token, "email", email)
	writeJSON(w, http.StatusOK, map[string]string{"session_id": sessID})
}

func (h *AttachmentsHandler) sendOTPEmail(to, code string) error {
	subject := "[MailShield] 添付ファイルダウンロード認証コード"
	bodyLines := []string{
		"MailShield 添付ファイルダウンロード認証コード",
		"",
		"認証コード: " + code,
		"",
		fmt.Sprintf("このコードは %d 分間有効です。", int(otp.CodeTTL.Minutes())),
		"身に覚えのない場合は、このメールを無視してください。",
	}
	body := strings.Join(bodyLines, "\r\n")

	// Subject と本文を base64 でエンコードして SMTPUTF8 不要にする
	encodedSubject := "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(subject)) + "?="
	encodedBody := base64.StdEncoding.EncodeToString([]byte(body))

	var msg bytes.Buffer
	fmt.Fprintf(&msg, "From: %s\r\n", h.notifCfg.FromAddress)
	fmt.Fprintf(&msg, "To: %s\r\n", to)
	fmt.Fprintf(&msg, "Subject: %s\r\n", encodedSubject)
	fmt.Fprintf(&msg, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&msg, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprintf(&msg, "Content-Transfer-Encoding: base64\r\n")
	fmt.Fprintf(&msg, "\r\n")
	// RFC 2045: base64 ラインは 76 文字以下
	for i := 0; i < len(encodedBody); i += 76 {
		end := i + 76
		if end > len(encodedBody) {
			end = len(encodedBody)
		}
		fmt.Fprintf(&msg, "%s\r\n", encodedBody[i:end])
	}

	addr := fmt.Sprintf("%s:%d", h.notifCfg.SMTPHost, h.notifCfg.SMTPPort)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("SMTP 接続失敗: %w", err)
	}
	c, err := smtp.NewClient(conn, h.notifCfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("SMTP クライアント作成失敗: %w", err)
	}
	defer c.Close()

	if err := c.Mail(h.notifCfg.FromAddress); err != nil {
		return fmt.Errorf("SMTP MAIL FROM 失敗: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("SMTP RCPT TO 失敗: %w", err)
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA 開始失敗: %w", err)
	}
	if _, err := wc.Write(msg.Bytes()); err != nil {
		return fmt.Errorf("SMTP データ書き込み失敗: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("SMTP DATA 終了失敗: %w", err)
	}
	return c.Quit()
}

func containsEmail(list []string, email string) bool {
	for _, e := range list {
		if strings.EqualFold(e, email) {
			return true
		}
	}
	return false
}

// HandleDisable は添付ファイルのダウンロードを有効/無効化する。
// PATCH /api/v1/attachments/{id}/disable
func (h *AttachmentsHandler) HandleDisable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body struct {
		Disabled bool `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストボディが不正です")
		return
	}

	if err := h.repo.DisableAttachment(r.Context(), id, body.Disabled); err != nil {
		slog.Error("添付ファイル無効化失敗", "id", id, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "更新に失敗しました")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleDelete は添付ファイルをソフトデリートする。
// DELETE /api/v1/attachments/{id}
func (h *AttachmentsHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.repo.DeleteAttachment(r.Context(), id); err != nil {
		slog.Error("添付ファイル削除失敗", "id", id, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "削除に失敗しました")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// checkMailboxRole は mode=auth のとき、ユーザーが許可ロールでメッセージの to_addresses の
// いずれかのメールボックスに割り当てられているかを確認する。
// アクセス不可の場合は 403 を書き込んで false を返す。
func (h *AttachmentsHandler) checkMailboxRole(
	w http.ResponseWriter,
	r *http.Request,
	userID, downloadToken string,
) bool {
	toAddrs, err := h.repo.GetAttachmentToAddressesByToken(r.Context(), downloadToken)
	if err != nil {
		slog.Error("to_addresses 取得失敗", "token", downloadToken, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "アクセス確認に失敗しました")
		return false
	}

	allowedRoleSet := h.cfg.AuthMode.AllowedRoleSet()
	var roles []domain.AssignmentRole
	for role := range allowedRoleSet {
		roles = append(roles, domain.AssignmentRole(role))
	}

	userMailboxes, err := h.repo.GetMailboxAddressesForUser(r.Context(), userID, roles)
	if err != nil {
		slog.Error("メールボックスアドレス取得失敗", "user_id", userID, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "アクセス確認に失敗しました")
		return false
	}

	mailboxSet := make(map[string]bool, len(userMailboxes))
	for _, m := range userMailboxes {
		mailboxSet[strings.ToLower(m)] = true
	}

	for _, addr := range toAddrs {
		if mailboxSet[strings.ToLower(addr)] {
			return true
		}
	}

	writeErrorResponse(w, http.StatusForbidden, "FORBIDDEN", "このファイルへのアクセス権がありません")
	return false
}

// streamAttachment は MinIO からファイルを取得してレスポンスにストリームする。
func (h *AttachmentsHandler) streamAttachment(w http.ResponseWriter, r *http.Request, storagePath, filename string) {
	body, ct, err := h.attachmentStor.GetAttachment(r.Context(), storagePath)
	if err != nil {
		slog.Error("MinIO からの添付ファイル取得失敗", "path", storagePath, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ファイルの取得に失敗しました")
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.PathEscape(filename)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, body); err != nil {
		slog.Warn("添付ファイルストリーム中断", "path", storagePath, "error", err)
	}
}
