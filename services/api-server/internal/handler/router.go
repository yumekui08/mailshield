package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/auth"
	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/otp"
	"github.com/koizumib/mailshield/services/api-server/internal/pwreset"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
	"github.com/koizumib/mailshield/services/api-server/internal/storage"
)

// NewRouter はchiルーターを組み立てて返す。
func NewRouter(
	standaloneProvider *auth.StandaloneProvider,
	ldapAuthProvider *auth.LDAPBindProvider,
	oidcProvider *auth.OIDCProvider,
	sessionStore auth.SessionStore,
	repo repository.Repository,
	stor storage.EMLStorage,
	attachmentStor storage.AttachmentStorage,
	otpStore otp.Store,
	pwResetStore pwreset.Store,
	cfg *config.Config,
) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)

	auditLogger := audit.New(repo)
	apiKeysHandler := NewAPIKeysHandler(repo, auditLogger)
	simulateHandler := NewSimulateHandler(cfg.Gateway.URL)

	authHandler := NewAuthHandler(
		standaloneProvider,
		ldapAuthProvider,
		oidcProvider,
		sessionStore,
		&cfg.Auth.Session,
		cfg.Server.FrontendURL,
		repo,
		pwResetStore,
		cfg.Notification,
		auditLogger,
	)
	healthHandler := NewHealthHandler()
	messagesHandler := NewMessagesHandler(repo, stor, cfg.Storage.PresignedURLExpiryH)
	quarantineHandler := NewQuarantineHandler(repo, stor, cfg.Notification, cfg.MailboxPolicy, auditLogger)
	approvalHandler := NewApprovalHandler(repo, stor, cfg.Notification, auditLogger)
	statsHandler := NewStatsHandler(repo, cfg.MailboxPolicy)
	usersHandler := NewUsersHandler(repo, auditLogger)
	mailboxesHandler := NewMailboxesHandler(repo, auditLogger)
	auditHandler := NewAuditHandler(repo)
	attachmentsHandler := NewAttachmentsHandler(repo, attachmentStor, cfg.AttachmentDownload, otpStore, cfg.Notification)

	authMW := middleware.AuthenticateOrAPIKey(sessionStore, &cfg.Auth.Session, repo)

	r.Get("/healthz", healthHandler.HandleHealthz)

	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			// 認証不要
			r.Get("/providers", authHandler.HandleProviders)
			r.Post("/login", authHandler.HandleLoginStandalone)
			r.Get("/login/oidc", authHandler.HandleLoginOIDC)
			r.Get("/callback", authHandler.HandleCallback)
			r.Post("/setup", authHandler.HandleSetup)
			r.Post("/forgot-password", authHandler.HandleForgotPassword)
			r.Post("/reset-password", authHandler.HandleResetPassword)

			// 認証必要
			r.Group(func(r chi.Router) {
				r.Use(authMW)
				r.Post("/logout", authHandler.HandleLogout)
				r.Get("/me", authHandler.HandleMe)
			})
		})

		// 統計エンドポイント（viewer以上）
		r.Route("/stats", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleViewer, domain.RoleOperator, domain.RoleAdmin))
			r.Get("/", statsHandler.HandleGet)
		})

		// メッセージエンドポイント（viewer以上）
		r.Route("/messages", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleViewer, domain.RoleOperator, domain.RoleAdmin))

			r.Get("/", messagesHandler.HandleList)
			r.Get("/{id}", messagesHandler.HandleGet)

			r.Get("/{id}/attachments", messagesHandler.HandleGetAttachments)

			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(domain.RoleOperator, domain.RoleAdmin))
				r.Get("/{id}/eml", messagesHandler.HandleGetEML)
			})
		})

		// ユーザー管理エンドポイント（admin のみ）
		r.Route("/users", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleAdmin))

			r.Get("/", usersHandler.HandleList)
			r.Post("/", usersHandler.HandleCreate)
			r.Patch("/{id}", usersHandler.HandleUpdate)
			r.Delete("/{id}", usersHandler.HandleDelete)
		})

		// メールボックス管理エンドポイント（operator/admin）
		r.Route("/mailboxes", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleOperator, domain.RoleAdmin))

			r.Get("/", mailboxesHandler.HandleList)
			r.Post("/", mailboxesHandler.HandleCreate)
			r.Patch("/{id}", mailboxesHandler.HandleUpdate)
			r.Delete("/{id}", mailboxesHandler.HandleDelete)
			r.Get("/{id}/assignments", mailboxesHandler.HandleListAssignments)
			r.Post("/{id}/assignments", mailboxesHandler.HandleAddAssignment)
			r.Delete("/{id}/assignments", mailboxesHandler.HandleRemoveAssignment)
		})

		// 添付ファイル公開エンドポイント（認証不要・ダウンロードトークンが認証代替）
		r.Route("/public/attachments", func(r chi.Router) {
			r.Get("/{token}", attachmentsHandler.HandlePublicList)
			r.Get("/{token}/{filename}", attachmentsHandler.HandlePublicDownload)
			r.Post("/{token}/otp/request", attachmentsHandler.HandleOTPRequest)
			r.Post("/{token}/otp/verify", attachmentsHandler.HandleOTPVerify)
		})

		// 添付ファイルエンドポイント
		r.Route("/attachments", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleViewer, domain.RoleOperator, domain.RoleAdmin))

			// ダウンロードトークン単位の操作（閲覧・ダウンロード）
			r.Get("/{token}", attachmentsHandler.HandleList)
			r.Get("/{token}/{filename}", attachmentsHandler.HandleDownload)

			// ID 単位の管理操作（operator/admin のみ）
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(domain.RoleOperator, domain.RoleAdmin))
				r.Patch("/{id}/disable", attachmentsHandler.HandleDisable)
				r.Delete("/{id}", attachmentsHandler.HandleDelete)
			})
		})

		// 監査ログエンドポイント（admin のみ）
		r.Route("/audit-logs", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleAdmin))
			r.Get("/", auditHandler.HandleList)
		})

		// API キー管理エンドポイント（admin のみ）
		r.Route("/api-keys", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleAdmin))
			r.Get("/", apiKeysHandler.HandleList)
			r.Post("/", apiKeysHandler.HandleCreate)
			r.Delete("/{id}", apiKeysHandler.HandleRevoke)
		})

		// ポリシーシミュレーションエンドポイント（operator/admin）
		r.Route("/simulate", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleOperator, domain.RoleAdmin))
			r.Post("/", simulateHandler.HandleSimulate)
		})

		// 隔離エンドポイント
		// 閲覧・解放は viewer 以上（フィルター・権限チェックはハンドラー内で実施）
		// 削除のみ operator/admin に制限
		r.Route("/quarantine", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleViewer, domain.RoleOperator, domain.RoleAdmin))

			r.Get("/", quarantineHandler.HandleList)
			r.Get("/{id}", quarantineHandler.HandleGet)
			r.Post("/{id}/release", quarantineHandler.HandleRelease)

			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(domain.RoleOperator, domain.RoleAdmin))
				r.Delete("/{id}", quarantineHandler.HandleDelete)
				r.Post("/bulk-release", quarantineHandler.HandleBulkRelease)
				r.Delete("/bulk", quarantineHandler.HandleBulkDelete)
			})
		})

		// 承認フローエンドポイント
		r.Route("/approvals", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleViewer, domain.RoleOperator, domain.RoleAdmin))

			r.Get("/", approvalHandler.HandleList)
			r.Get("/{id}", approvalHandler.HandleGet)
			r.Post("/{id}/approve", approvalHandler.HandleApprove)
			r.Post("/{id}/reject", approvalHandler.HandleReject)
		})

		// ユーザー承認者設定（admin のみ）
		r.Route("/users/{id}/approver", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleAdmin))

			r.Get("/", approvalHandler.HandleGetUserApprover)
			r.Put("/", approvalHandler.HandleSetUserApprover)
		})
	})

	return r
}
