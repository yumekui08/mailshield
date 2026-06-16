package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/auth"
	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/pwreset"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// AuthHandler は認証フローを処理するハンドラーである。
// standalone と oidc は有効な場合のみ非 nil になる。
type AuthHandler struct {
	standalone  *auth.StandaloneProvider
	oidc        *auth.OIDCProvider
	store       *auth.SessionStore
	sessionCfg  *config.SessionConfig
	frontendURL string
	repo        repository.Repository
	pwResetStore *pwreset.Store
	notifCfg    config.NotificationConfig
	auditLogger *audit.Logger
}

// NewAuthHandler はAuthHandlerを返す。
// standalone と oidc はどちらか一方または両方が nil でも動作する。
func NewAuthHandler(
	standalone *auth.StandaloneProvider,
	oidc *auth.OIDCProvider,
	store *auth.SessionStore,
	cfg *config.SessionConfig,
	frontendURL string,
	repo repository.Repository,
	pwResetStore *pwreset.Store,
	notifCfg config.NotificationConfig,
	auditLogger *audit.Logger,
) *AuthHandler {
	return &AuthHandler{
		standalone:   standalone,
		oidc:         oidc,
		store:        store,
		sessionCfg:   cfg,
		frontendURL:  frontendURL,
		repo:         repo,
		pwResetStore: pwResetStore,
		notifCfg:     notifCfg,
		auditLogger:  auditLogger,
	}
}

// HandleProviders はGET /api/v1/auth/providers を処理する。
// 有効な認証プロバイダー一覧と初回セットアップが必要かどうかを返す。
func (h *AuthHandler) HandleProviders(w http.ResponseWriter, r *http.Request) {
	type providerInfo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var providers []providerInfo
	if h.standalone != nil {
		providers = append(providers, providerInfo{ID: "standalone", Name: "メールアドレス・パスワード"})
	}
	if h.oidc != nil {
		providers = append(providers, providerInfo{ID: "oidc", Name: "SSO（OIDC）"})
	}
	if providers == nil {
		providers = []providerInfo{}
	}

	// スタンドアロンが有効でユーザーが0人の場合はセットアップが必要
	setupRequired := false
	if h.standalone != nil {
		count, err := h.repo.CountUsers(r.Context())
		if err == nil && count == 0 {
			setupRequired = true
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"providers":      providers,
		"setup_required": setupRequired,
	})
}

// HandleLoginStandalone はPOST /api/v1/auth/login を処理する。
// メールアドレスとパスワードでスタンドアロン認証を行いセッションを発行する。
func (h *AuthHandler) HandleLoginStandalone(w http.ResponseWriter, r *http.Request) {
	if h.standalone == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "スタンドアロン認証は無効です")
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "リクエストの形式が正しくありません")
		return
	}
	if req.Email == "" || req.Password == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "emailとpasswordは必須です")
		return
	}

	session, err := h.standalone.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		slog.Warn("スタンドアロンログイン失敗", "email", req.Email, "error", err)
		ip := audit.ClientIP(r)
		h.auditLogger.Log(domain.AuditLog{
			EventType: domain.EventAuthLoginFailure,
			IPAddress: &ip,
			Detail:    map[string]any{"email": req.Email},
		})
		writeErrorResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
		return
	}

	sessionID, err := h.store.Create(r.Context(), session)
	if err != nil {
		slog.Error("セッション保存失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "セッション作成に失敗しました")
		return
	}

	h.setSessionCookie(w, sessionID, session.ExpiresAt)

	slog.Info("スタンドアロンログイン成功", "email", session.User.Email, "role", session.Role)

	ip := audit.ClientIP(r)
	h.auditLogger.Log(domain.AuditLog{
		EventType:  domain.EventAuthLoginSuccess,
		ActorID:    &session.User.Sub,
		ActorEmail: &session.User.Email,
		IPAddress:  &ip,
	})

	writeJSON(w, http.StatusOK, map[string]string{"message": "ログインしました"})
}

// HandleLoginOIDC はGET /api/v1/auth/login/oidc を処理する。
// OIDCプロバイダーへのリダイレクトURLを返す。
func (h *AuthHandler) HandleLoginOIDC(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "OIDC認証は無効です")
		return
	}

	state := uuid.New().String()
	nonce := uuid.New().String()
	redirectTo := r.URL.Query().Get("redirect_to")
	if redirectTo == "" {
		redirectTo = "/"
	}

	if err := h.store.SaveState(r.Context(), state, nonce, redirectTo); err != nil {
		slog.Error("OIDC state保存失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ログイン処理に失敗しました")
		return
	}

	authURL := h.oidc.AuthCodeURL(state, nonce)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// HandleCallback はGET /api/v1/auth/callback を処理する。
// OIDCプロバイダーからの認可コードを受け取りセッションを生成する。
func (h *AuthHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "OIDC認証は無効です")
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "code または state が見つかりません")
		return
	}

	nonce, redirectTo, err := h.store.ConsumeState(r.Context(), state)
	if err != nil {
		slog.Warn("OIDC state検証失敗", "state", state, "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "不正なstateです")
		return
	}

	session, err := h.oidc.Exchange(r.Context(), code, nonce)
	if err != nil {
		slog.Error("OIDCトークン交換失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "認証処理に失敗しました")
		return
	}

	sessionID, err := h.store.Create(r.Context(), session)
	if err != nil {
		slog.Error("セッション保存失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "セッション作成に失敗しました")
		return
	}

	h.setSessionCookie(w, sessionID, session.ExpiresAt)

	slog.Info("OIDCログイン成功", "user", session.User.Email, "role", session.Role)

	dest := redirectTo
	if h.frontendURL != "" && len(redirectTo) > 0 && redirectTo[0] == '/' {
		dest = h.frontendURL + redirectTo
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// HandleSetup はPOST /api/v1/auth/setup を処理する。
// ユーザーが0人のときだけ最初の管理者を登録できる。
func (h *AuthHandler) HandleSetup(w http.ResponseWriter, r *http.Request) {
	if h.standalone == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "スタンドアロン認証が無効のためセットアップは不要です")
		return
	}

	count, err := h.repo.CountUsers(r.Context())
	if err != nil {
		slog.Error("ユーザー数取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "セットアップ確認に失敗しました")
		return
	}
	if count > 0 {
		writeErrorResponse(w, http.StatusConflict, "ALREADY_SETUP", "すでにセットアップ済みです")
		return
	}

	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
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

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		slog.Error("パスワードハッシュ生成失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "セットアップに失敗しました")
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
		Role:         domain.RoleAdmin,
		IsActive:     true,
	}
	if err := h.repo.CreateUser(r.Context(), user); err != nil {
		slog.Error("管理者ユーザー作成失敗", "email", req.Email, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザー作成に失敗しました")
		return
	}

	slog.Info("初期管理者作成完了", "email", req.Email)
	writeJSON(w, http.StatusCreated, map[string]string{
		"message": "管理者ユーザーを作成しました。ログインしてください。",
		"email":   req.Email,
	})
}

// HandleLogout はPOST /api/v1/auth/logout を処理する。
func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	if session != nil {
		if err := h.store.Delete(r.Context(), session.ID); err != nil {
			slog.Warn("セッション削除失敗", "session_id", session.ID, "error", err)
		}
		ip := audit.ClientIP(r)
		h.auditLogger.Log(domain.AuditLog{
			EventType:  domain.EventAuthLogout,
			ActorID:    &session.User.Sub,
			ActorEmail: &session.User.Email,
			IPAddress:  &ip,
		})
	}

	http.SetCookie(w, &http.Cookie{
		Name:     h.sessionCfg.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.sessionCfg.CookieSecure,
		MaxAge:   -1,
	})

	writeJSON(w, http.StatusOK, map[string]string{"message": "ログアウトしました"})
}

// HandleMe はGET /api/v1/auth/me を処理する。
func (h *AuthHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		writeErrorResponse(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
		return
	}

	type meResponse struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
		Name  string `json:"name"`
		Role  string `json:"role"`
	}

	writeJSON(w, http.StatusOK, meResponse{
		Sub:   session.User.Sub,
		Email: session.User.Email,
		Name:  session.User.Name,
		Role:  string(session.Role),
	})
}

// HandleForgotPassword はPOST /api/v1/auth/forgot-password を処理する。
// メールアドレスが存在する場合のみリセットリンクを送信する。
// メールアドレスの存在有無はレスポンスに反映しない（ユーザー列挙防止）。
func (h *AuthHandler) HandleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if h.standalone == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "スタンドアロン認証が無効です")
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "email が必要です")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))

	// メール存在確認（失敗しても同じレスポンスを返す）
	user, err := h.repo.FindUserByEmail(r.Context(), email)
	if err != nil || user == nil || !user.IsActive {
		slog.Info("パスワードリセット要求（ユーザー不明または無効）", "email", email)
		writeJSON(w, http.StatusOK, map[string]string{"message": "リセットリンクを送信しました（メールアドレスが登録済みの場合）"})
		return
	}

	token, err := h.pwResetStore.GenerateToken(r.Context(), user.ID)
	if err != nil {
		slog.Error("パスワードリセットトークン生成失敗", "email", email, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "処理に失敗しました")
		return
	}

	resetURL := fmt.Sprintf("%s/reset-password?token=%s", h.frontendURL, token)
	if err := h.sendPasswordResetEmail(email, resetURL); err != nil {
		slog.Error("パスワードリセットメール送信失敗", "email", email, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "MAIL_SEND_FAILED", "メールの送信に失敗しました")
		return
	}

	slog.Info("パスワードリセットメール送信完了", "email", email)

	ip := audit.ClientIP(r)
	h.auditLogger.Log(domain.AuditLog{
		EventType: domain.EventAuthPasswordResetRequest,
		IPAddress: &ip,
		Detail:    map[string]any{"email": email},
	})

	writeJSON(w, http.StatusOK, map[string]string{"message": "リセットリンクを送信しました（メールアドレスが登録済みの場合）"})
}

// HandleResetPassword はPOST /api/v1/auth/reset-password を処理する。
// トークンを検証して新しいパスワードに更新する。
func (h *AuthHandler) HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	if h.standalone == nil {
		writeErrorResponse(w, http.StatusNotFound, "NOT_FOUND", "スタンドアロン認証が無効です")
		return
	}

	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" || req.Password == "" {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "token と password が必要です")
		return
	}
	if len(req.Password) < 8 {
		writeErrorResponse(w, http.StatusBadRequest, "BAD_REQUEST", "パスワードは8文字以上にしてください")
		return
	}

	userID, err := h.pwResetStore.ConsumeToken(r.Context(), req.Token)
	if err != nil {
		if errors.Is(err, pwreset.ErrTokenNotFound) {
			writeErrorResponse(w, http.StatusBadRequest, "INVALID_TOKEN", "リセットリンクが無効または期限切れです")
			return
		}
		slog.Error("パスワードリセットトークン取得失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "処理に失敗しました")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		slog.Error("パスワードハッシュ生成失敗", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "処理に失敗しました")
		return
	}

	if err := h.repo.UpdateUserPassword(r.Context(), userID, hash); err != nil {
		slog.Error("パスワード更新失敗", "user_id", userID, "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "パスワードの更新に失敗しました")
		return
	}

	slog.Info("パスワードリセット完了", "user_id", userID)

	ip := audit.ClientIP(r)
	h.auditLogger.Log(domain.AuditLog{
		EventType:  domain.EventAuthPasswordResetDone,
		ActorID:    &userID,
		TargetType: audit.StrPtr("user"),
		TargetID:   &userID,
		IPAddress:  &ip,
	})

	writeJSON(w, http.StatusOK, map[string]string{"message": "パスワードを更新しました"})
}

func (h *AuthHandler) sendPasswordResetEmail(to, resetURL string) error {
	subject := "[MailShield] パスワードリセット"
	bodyLines := []string{
		"パスワードリセットのリクエストを受け付けました。",
		"",
		"以下のリンクをクリックしてパスワードを再設定してください。",
		"",
		resetURL,
		"",
		fmt.Sprintf("このリンクは %d 分間有効です。", int(pwreset.TokenTTL.Minutes())),
		"身に覚えのない場合は、このメールを無視してください。",
	}
	body := strings.Join(bodyLines, "\r\n")

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

// setSessionCookie はセッション Cookie をセットする内部ヘルパー。
func (h *AuthHandler) setSessionCookie(w http.ResponseWriter, sessionID string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.sessionCfg.CookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.sessionCfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
}

// writeErrorResponse はJSON形式のエラーレスポンスを書き込む内部ヘルパー。
func writeErrorResponse(w http.ResponseWriter, statusCode int, code, message string) {
	type errDetail struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	type errResp struct {
		Error errDetail `json:"error"`
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(errResp{
		Error: errDetail{Code: code, Message: message},
	})
}
