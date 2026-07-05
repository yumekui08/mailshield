// Package main は api-server サービスのエントリーポイントである。
// DIのみを行い、ビジネスロジックは書かない。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/koizumib/mailshield/services/api-server/internal/approval"
	"github.com/koizumib/mailshield/services/api-server/internal/auth"
	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	ldapsync "github.com/koizumib/mailshield/services/api-server/internal/directory/ldap"
	"github.com/koizumib/mailshield/services/api-server/internal/handler"
	"github.com/koizumib/mailshield/services/api-server/internal/otp"
	"github.com/koizumib/mailshield/services/api-server/internal/pwreset"
	"github.com/koizumib/mailshield/services/api-server/internal/repository/mariadb"
	"github.com/koizumib/mailshield/services/api-server/internal/storage"
)

func main() {
	// ─── 設定読み込み ─────────────────────────────────────────
	configFile := os.Getenv("CONFIG_FILE")
	if configFile == "" {
		configFile = "config/api-server.yaml"
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "設定読み込み失敗: %v\n", err)
		os.Exit(1)
	}

	// ─── ログ初期化 ───────────────────────────────────────────
	setupLogger(cfg.Log.Level, cfg.Log.Format)

	slog.Info("api-server 起動中",
		"version", "0.1.0",
		"config", configFile,
		"port", cfg.Server.Port,
	)

	// ─── MariaDB ─────────────────────────────────────────────
	slog.Debug("MariaDB 初期化", "host", cfg.Database.Host, "port", cfg.Database.Port)
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4&loc=UTC",
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.Name,
	)
	repo, err := mariadb.New(dsn, mariadb.Config{
		MaxOpenConns:           cfg.Database.MaxOpenConns,
		MaxIdleConns:           cfg.Database.MaxIdleConns,
		ConnMaxLifetimeMinutes: cfg.Database.ConnMaxLifetimeMinutes,
	})
	if err != nil {
		slog.Error("MariaDB 初期化失敗", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := repo.Close(); err != nil {
			slog.Warn("MariaDB クローズ失敗", "error", err)
		}
	}()
	slog.Info("MariaDB 接続完了", "host", cfg.Database.Host)

	// ─── セッション/OTP ストア（Redis または MariaDB）────────────
	var (
		sessionStore auth.SessionStore
		otpStore     otp.Store
		pwResetStore pwreset.Store
	)
	if cfg.Redis.Backend == "mariadb" {
		slog.Info("キャッシュバックエンド: MariaDB（Redis 不要）")
		sessionStore = auth.NewMariaDBSessionStore(repo.DB(), &cfg.Auth.Session)
		otpStore = otp.NewMariaDBStore(repo.DB())
		pwResetStore = pwreset.NewMariaDBStore(repo.DB())
	} else {
		slog.Debug("Redis 初期化", "host", cfg.Redis.Host, "port", cfg.Redis.Port)
		redisClient := redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := redisClient.Ping(ctx).Err(); err != nil {
			cancel()
			slog.Error("Redis 接続確認失敗", "error", err)
			os.Exit(1)
		}
		cancel()

		defer func() {
			if err := redisClient.Close(); err != nil {
				slog.Warn("Redis クローズ失敗", "error", err)
			}
		}()
		slog.Info("Redis 接続完了", "host", cfg.Redis.Host)

		sessionStore = auth.NewSessionStore(redisClient, &cfg.Auth.Session)
		otpStore = otp.NewStore(redisClient)
		pwResetStore = pwreset.NewStore(redisClient)
	}

	// ─── バックグラウンドサービス共通 context ─────────────────
	// 承認フロー・LDAP 定期同期など、シグナル受信で一括停止するジョブはすべてこの ctx を使う。
	bgCtx, bgCancel := context.WithCancel(context.Background())

	// ─── ローカルログイン（directory.source で決まる） / OIDC（sso_mode で決まる） ──
	// directory.source: ユーザー情報の真実の源とローカルログイン手段（none→standalone / ldap→LDAP bind / scim→無し）
	// auth.sso_mode   : OIDC の扱い（disabled/optional/required）。config.Load() 内で
	//                   scim+disabled・required 時の OIDC 未設定は起動時エラーとして弾いている。
	provisioner := directory.NewProvisioner(repo)

	var standaloneProvider *auth.StandaloneProvider
	var ldapAuthProvider *auth.LDAPBindProvider
	localLoginAllowed := cfg.Auth.LocalLoginAllowed()

	switch cfg.Directory.EffectiveSource() {
	case config.DirectorySourceLDAP:
		if localLoginAllowed {
			ldapAuthProvider, err = auth.NewLDAPBindProvider(&cfg.Directory.LDAP, provisioner, repo, &cfg.Auth.Session)
			if err != nil {
				slog.Error("LDAP bind 認証プロバイダー初期化失敗", "error", err)
				os.Exit(1)
			}
			slog.Info("LDAP bind 認証: 有効")
		}

		ldapConnCfg, ldapSyncCfg, err := auth.BuildLDAPConnConfig(&cfg.Directory.LDAP)
		if err != nil {
			slog.Error("LDAP 設定不正", "error", err)
			os.Exit(1)
		}
		syncer := ldapsync.NewSyncer(provisioner, repo, repo, ldapSyncCfg)
		syncIntervalMinutes := cfg.Directory.LDAP.SyncIntervalMinutes
		if syncIntervalMinutes <= 0 {
			syncIntervalMinutes = 60
		}
		ldapSyncInterval := time.Duration(syncIntervalMinutes) * time.Minute
		go syncer.RunPeriodic(bgCtx, ldapConnCfg, ldapSyncInterval)
		slog.Info("LDAP ディレクトリ同期起動",
			"host", cfg.Directory.LDAP.Host,
			"base_dn", cfg.Directory.LDAP.BaseDN,
			"interval", ldapSyncInterval,
			"deactivate_missing_users", cfg.Directory.LDAP.DeactivateMissingUsers,
		)
	case config.DirectorySourceSCIM:
		slog.Info("ユーザー情報源: SCIM（プロビジョニングは未実装。auth.sso_mode 経由の SSO ログインのみ有効）")
	default:
		if localLoginAllowed {
			standaloneProvider = auth.NewStandaloneProvider(repo, &cfg.Auth)
			slog.Info("スタンドアロン認証: 有効")
		}
	}

	// ─── OIDCプロバイダー（sso_mode: optional/required で有効） ───────────
	var oidcProvider *auth.OIDCProvider
	if cfg.Auth.SSOAllowed() {
		slog.Debug("OIDCプロバイダー初期化", "issuer", cfg.Auth.Providers.OIDC.Issuer)
		oidcProvider, err = initOIDCWithRetry(cfg)
		if err != nil {
			slog.Error("OIDCプロバイダー初期化失敗（タイムアウト）", "error", err)
			os.Exit(1)
		}
		slog.Info("OIDCプロバイダー初期化完了", "issuer", cfg.Auth.Providers.OIDC.Issuer)
	}

	if standaloneProvider == nil && ldapAuthProvider == nil && oidcProvider == nil {
		slog.Error("認証プロバイダーが1つも有効になっていません。directory.source と auth.sso_mode の組み合わせを確認してください")
		os.Exit(1)
	}

	// ─── MinIO ────────────────────────────────────────────────
	slog.Debug("MinIO 初期化", "endpoint", cfg.Storage.Endpoint)
	stor, err := storage.New(
		cfg.Storage.Endpoint,
		cfg.Storage.PublicEndpoint,
		cfg.Storage.AccessKey,
		cfg.Storage.SecretKey,
		cfg.Storage.BucketEML,
		cfg.Storage.BucketAttachments,
		cfg.Storage.UseSSL,
		cfg.Storage.PublicUseSSL,
	)
	if err != nil {
		slog.Error("MinIO 初期化失敗", "error", err)
		os.Exit(1)
	}
	slog.Info("MinIO 初期化完了", "endpoint", cfg.Storage.Endpoint, "public_endpoint", cfg.Storage.PublicEndpoint)

	// ─── 承認フロー バックグラウンドサービス ───────────────────
	approvalSvc := approval.New(repo, cfg.Approval, cfg.Notification)
	go approvalSvc.RunNotifier(bgCtx)
	go approvalSvc.RunExpiryWorker(bgCtx)
	slog.Info("承認フロー バックグラウンドサービス起動")

	// ─── HTTPサーバー ─────────────────────────────────────────
	router := handler.NewRouter(standaloneProvider, ldapAuthProvider, oidcProvider, sessionStore, repo, stor, stor, otpStore, pwResetStore, cfg)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("api-server 起動完了", "addr", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// ─── シグナル待機 ─────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	select {
	case sig := <-quit:
		slog.Info("シグナル受信・シャットダウン開始", "signal", sig.String())
	case err := <-serverErr:
		slog.Error("HTTPサーバー異常終了", "error", err)
	}

	// ─── グレースフルシャットダウン ───────────────────────────
	bgCancel() // バックグラウンドサービスを停止
	shutdownTimeout := time.Duration(cfg.Server.ShutdownTimeoutSeconds) * time.Second
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	slog.Info("HTTPサーバー停止中", "timeout_seconds", cfg.Server.ShutdownTimeoutSeconds)
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("HTTPサーバーのシャットダウンに時間がかかりました", "error", err)
	}

	slog.Info("シャットダウン完了")
}

// initOIDCWithRetry はOIDCプロバイダーの初期化をリトライする。
// IdP の起動が遅い場合（Authentik 等）に対応するため最大3分間試みる。
func initOIDCWithRetry(cfg *config.Config) (*auth.OIDCProvider, error) {
	const maxWait = 3 * time.Minute
	const interval = 10 * time.Second

	deadline := time.Now().Add(maxWait)
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		provider, err := auth.NewOIDCProvider(ctx, &cfg.Auth)
		cancel()
		if err == nil {
			return provider, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		slog.Warn("OIDCプロバイダー接続待機中",
			"attempt", attempt,
			"issuer", cfg.Auth.Providers.OIDC.Issuer,
			"retry_in", interval,
		)
		time.Sleep(interval)
	}
}

// setupLogger はログレベルとフォーマットに従ってslogを初期化する。
func setupLogger(level, format string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}
