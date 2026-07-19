package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/koizumib/mailshield/services/api-server/internal/audit"
	"github.com/koizumib/mailshield/services/api-server/internal/auth"
	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/delay"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/middleware"
	"github.com/koizumib/mailshield/services/api-server/internal/otp"
	"github.com/koizumib/mailshield/services/api-server/internal/policyfile"
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
	delayService *delay.Service,
	cfg *config.Config,
) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(middleware.SecurityHeaders)

	// 認証系エンドポイント（ログイン・パスワードリセット・OTP 発行）のレート制限。
	// ブルートフォース攻撃と通知メール送信の濫用を防ぐ。
	sensitiveLimit := middleware.PassThrough
	if cfg.Auth.RateLimit.EffectiveEnabled() {
		limiter := middleware.NewRateLimiter(
			cfg.Auth.RateLimit.EffectiveMaxAttempts(),
			time.Duration(cfg.Auth.RateLimit.EffectiveWindowSeconds())*time.Second,
		)
		sensitiveLimit = limiter.Middleware
	}

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
	delayHandler := NewDelayHandler(repo, delayService, cfg.MailboxPolicy, auditLogger)
	statsHandler := NewStatsHandler(repo, cfg.MailboxPolicy)
	policyHandler := NewPolicyHandler(
		policyfile.RoutesDir(cfg.Settings.SmtpGatewayConfigFile),
		cfg.Gateway.URL,
		repo,
		auditLogger,
	)
	usersHandler := NewUsersHandler(repo, auditLogger)
	mailboxesHandler := NewMailboxesHandler(repo, auditLogger)
	// 設定エンティティ管理（ADR 008）。具体実装（mariadb）は ConfigRepository も満たす。
	var configHandler *ConfigHandler
	if cfgRepo, ok := repo.(repository.ConfigRepository); ok {
		configHandler = NewConfigHandler(cfgRepo, auditLogger)
	}
	auditHandler := NewAuditHandler(repo)
	attachmentsHandler := NewAttachmentsHandler(repo, attachmentStor, cfg.AttachmentDownload, otpStore, cfg.Notification)

	authMW := middleware.AuthenticateOrAPIKey(sessionStore, &cfg.Auth.Session, repo)

	r.Get("/healthz", healthHandler.HandleHealthz)

	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			// 認証不要（資格情報を受け取るエンドポイントはレート制限付き）
			r.Get("/providers", authHandler.HandleProviders)
			r.With(sensitiveLimit).Post("/login", authHandler.HandleLoginStandalone)
			r.Get("/login/oidc", authHandler.HandleLoginOIDC)
			r.Get("/callback", authHandler.HandleCallback)
			r.With(sensitiveLimit).Post("/setup", authHandler.HandleSetup)
			r.With(sensitiveLimit).Post("/forgot-password", authHandler.HandleForgotPassword)
			r.With(sensitiveLimit).Post("/reset-password", authHandler.HandleResetPassword)

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
			r.Get("/timeseries", statsHandler.HandleTimeseries)
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

		// ユーザーエンドポイント
		r.Route("/users", func(r chi.Router) {
			r.Use(authMW)

			// 検索は operator 以上（UserPicker: メールボックス割り当て等で使用）。最小フィールドのみ返す。
			r.With(middleware.RequireRole(domain.RoleOperator, domain.RoleAdmin)).
				Get("/search", usersHandler.HandleSearch)

			// 管理操作は admin のみ
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(domain.RoleAdmin))
				r.Get("/", usersHandler.HandleList)
				r.Post("/", usersHandler.HandleCreate)
				r.Patch("/{id}", usersHandler.HandleUpdate)
				r.Delete("/{id}", usersHandler.HandleDelete)
			})
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

		// 設定エンティティ管理（ADR 008・ワーカーインスタンス / 設定変数）（operator/admin）
		if configHandler != nil {
			r.Route("/config", func(r chi.Router) {
				r.Use(authMW)
				r.Use(middleware.RequireRole(domain.RoleOperator, domain.RoleAdmin))

				r.Get("/worker-instances", configHandler.HandleListWorkerInstances)
				r.Post("/worker-instances", configHandler.HandleCreateWorkerInstance)
				r.Put("/worker-instances/{id}", configHandler.HandleUpdateWorkerInstance)
				r.Delete("/worker-instances/{id}", configHandler.HandleDeleteWorkerInstance)

				r.Get("/variables", configHandler.HandleListConfigVariables)
				r.Post("/variables", configHandler.HandleCreateConfigVariable)
				r.Put("/variables/{id}", configHandler.HandleUpdateConfigVariable)
				r.Delete("/variables/{id}", configHandler.HandleDeleteConfigVariable)

				r.Get("/routings", configHandler.HandleListRoutings)
				r.Post("/routings", configHandler.HandleCreateRouting)
				r.Put("/routings/{id}", configHandler.HandleUpdateRouting)
				r.Delete("/routings/{id}", configHandler.HandleDeleteRouting)

				// マニフェスト・バンドルのインポート/エクスポート（ADR 008）
				r.Get("/export", configHandler.HandleExportBundle)
				r.Post("/import", configHandler.HandleImportBundle)
			})
		}

		// 添付ファイル公開エンドポイント（認証不要・ダウンロードトークンが認証代替）
		r.Route("/public/attachments", func(r chi.Router) {
			r.Get("/{token}", attachmentsHandler.HandlePublicList)
			r.Get("/{token}/{filename}", attachmentsHandler.HandlePublicDownload)
			r.With(sensitiveLimit).Post("/{token}/otp/request", attachmentsHandler.HandleOTPRequest)
			r.With(sensitiveLimit).Post("/{token}/otp/verify", attachmentsHandler.HandleOTPVerify)
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

		// ポリシー編集エンドポイント（閲覧: operator/admin、更新: admin のみ）
		r.Route("/policy", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleOperator, domain.RoleAdmin))
			r.Get("/routes", policyHandler.HandleListRoutes)
			r.Get("/routes/{route}", policyHandler.HandleGetRoute)
			r.Get("/routes/{route}/versions", policyHandler.HandleListVersions)
			r.Get("/stats", policyHandler.HandleStats)

			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireRole(domain.RoleAdmin))
				r.Put("/routes/{route}", policyHandler.HandleUpdateRoute)
				r.Post("/routes/{route}/rollback", policyHandler.HandleRollback)
			})
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
			r.Post("/bulk-approve", approvalHandler.HandleBulkApprove)
			r.Post("/bulk-reject", approvalHandler.HandleBulkReject)
			r.Get("/{id}", approvalHandler.HandleGet)
			r.Post("/{id}/approve", approvalHandler.HandleApprove)
			r.Post("/{id}/reject", approvalHandler.HandleReject)
		})

		// 送信ディレイ（送信待ち）エンドポイント
		r.Route("/delayed", func(r chi.Router) {
			r.Use(authMW)
			r.Use(middleware.RequireRole(domain.RoleViewer, domain.RoleOperator, domain.RoleAdmin))

			r.Get("/", delayHandler.HandleList)
			r.Post("/{id}/cancel", delayHandler.HandleCancel)
			r.Post("/{id}/send-now", delayHandler.HandleSendNow)
		})
	})

	return r
}
