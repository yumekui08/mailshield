package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/deliver"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/events"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/logging"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/metrics"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/notify"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/pipeline"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/policy"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/repository/mariadb"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/router"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/smtp"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/storage"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/arcsealer"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/attachcheck"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/clamav"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/disclaimer"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/filesep"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/header"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/macrostrip"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/qrcheck"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/sanitize"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/tika"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/urlcheck"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/urlrewrite"
)

const version = "0.1.0"

func resolveConfigDir(path string) string {
	if path == "" {
		path = os.Getenv("MAILSHIELD_CONFIG_DIR")
	}
	if path == "" {
		path = "config"
	}
	return path
}

func main() {
	var (
		configPath  string
		showVersion bool
	)
	flag.StringVar(&configPath, "c", "", "設定ディレクトリのパス (デフォルト: config, 環境変数: MAILSHIELD_CONFIG_DIR)")
	flag.StringVar(&configPath, "config", "", "設定ディレクトリのパス (デフォルト: config, 環境変数: MAILSHIELD_CONFIG_DIR)")
	flag.BoolVar(&showVersion, "v", false, "バージョンを表示して終了")
	flag.BoolVar(&showVersion, "version", false, "バージョンを表示して終了")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: smtp-gateway [options]\n\nOptions:\n")
		fmt.Fprintf(os.Stderr, "  -c, -config <dir>   設定ディレクトリのパス\n")
		fmt.Fprintf(os.Stderr, "                      <dir>/mailshield.default.yaml → <dir>/mailshield.yaml →\n")
		fmt.Fprintf(os.Stderr, "                      <dir>/mailshield.d/*.yaml → <dir>/routes.d/ の順に読み込む\n")
		fmt.Fprintf(os.Stderr, "                      (デフォルト: config, 環境変数: MAILSHIELD_CONFIG_DIR)\n")
		fmt.Fprintf(os.Stderr, "  -v, -version        バージョンを表示して終了\n")
		fmt.Fprintf(os.Stderr, "  -h, -help           このヘルプを表示して終了\n")
	}
	flag.Parse()

	if showVersion {
		fmt.Printf("mailshield smtp-gateway version %s\n", version)
		os.Exit(0)
	}

	// ログ初期化前なので設定読み込みエラーは stderr に出力
	configDir := resolveConfigDir(configPath)

	cfg, err := config.Load(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "設定読み込み失敗: %v\n", err)
		os.Exit(1)
	}

	if err := logging.Setup(&cfg.Log); err != nil {
		fmt.Fprintf(os.Stderr, "ログ初期化失敗: %v\n", err)
		os.Exit(1)
	}

	slog.Info("smtp-gateway 起動中",
		"version", version,
		"config_dir", configDir,
		"log_level", cfg.Log.Level,
		"log_output", cfg.Log.Output,
	)

	var (
		emlStorage     domain.EMLStorage
		archiveStorage domain.ArchiveStorage
		attachStorage  domain.AttachmentStorage
	)
	switch cfg.Storage.Backend {
	case "filesystem":
		slog.Debug("ローカルファイルシステムストレージ初期化", "dir", cfg.Storage.LocalDir)
		fs, err := storage.NewFilesystem(cfg.Storage.LocalDir, cfg.Storage.PublicBaseURL, cfg.Storage.PublicPathPrefix)
		if err != nil {
			slog.Error("ローカルストレージ初期化失敗", "error", err)
			os.Exit(1)
		}
		slog.Info("ローカルファイルシステムストレージ初期化完了", "dir", cfg.Storage.LocalDir)
		emlStorage, archiveStorage, attachStorage = fs, fs, fs
	default: // minio, s3
		slog.Debug("MinIO 初期化", "endpoint", cfg.Storage.Endpoint)
		ms, err := storage.New(
			cfg.Storage.Endpoint,
			cfg.Storage.AccessKey,
			cfg.Storage.SecretKey,
			cfg.Storage.BucketEML,
			cfg.Storage.BucketAttachments,
			cfg.Storage.Region,
			cfg.Storage.UseSSL,
		)
		if err != nil {
			slog.Error("MinIO 初期化失敗", "error", err)
			os.Exit(1)
		}
		slog.Info("MinIO 接続完了", "endpoint", cfg.Storage.Endpoint)
		emlStorage, archiveStorage, attachStorage = ms, ms, ms
	}

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
		PingTimeoutSeconds:     cfg.Database.PingTimeoutSeconds,
	})
	if err != nil {
		slog.Error("MariaDB 初期化失敗", "error", err)
		os.Exit(1)
	}
	defer repo.Close()
	slog.Info("MariaDB 接続完了", "host", cfg.Database.Host)

	var publisher domain.EventPublisher
	switch cfg.Events.Backend {
	case "webhook":
		pub, err := events.NewWebhook(
			cfg.Events.Webhook.URL,
			cfg.Events.Webhook.Secret,
			cfg.Events.Webhook.TimeoutSeconds,
			cfg.Events.Webhook.MaxRetries,
			cfg.Events.Webhook.RetryBackoffSeconds,
		)
		if err != nil {
			slog.Error("webhook イベントバックエンド初期化失敗", "error", err)
			os.Exit(1)
		}
		slog.Info("イベント通知: webhook モード", "url", cfg.Events.Webhook.URL)
		publisher = pub
	default: // none
		slog.Info("イベント通知: なし（mail.received イベントは発行しない）")
		publisher = events.NewNoop()
	}

	if len(cfg.Routes) == 0 {
		slog.Error("routes が設定されていません")
		os.Exit(1)
	}

	// ワーカーインスタンスはステートレスなので全ルートで共有する。
	// どのルートで有効化するかは各ルートの WorkersConfig で制御する。
	workerConfigDir := cfg.Workers.WorkerConfigDir

	avWorker, err := clamav.New(workerConfigDir)
	if err != nil {
		slog.Error("av-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	dlpWorker, err := tika.New(workerConfigDir)
	if err != nil {
		slog.Error("dlp-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	headerWorker, err := header.New(workerConfigDir)
	if err != nil {
		slog.Error("header-inspector 初期化失敗", "error", err)
		os.Exit(1)
	}
	urlCheckWorker, err := urlcheck.New(workerConfigDir)
	if err != nil {
		slog.Error("url-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	qrCheckWorker, err := qrcheck.New(workerConfigDir)
	if err != nil {
		slog.Error("qr-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	attachCheckWorker, err := attachcheck.New(workerConfigDir)
	if err != nil {
		slog.Error("attachment-inspector 初期化失敗", "error", err)
		os.Exit(1)
	}
	sanitizeWorker, err := sanitize.New(workerConfigDir)
	if err != nil {
		slog.Error("sanitize-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	macroStripWorker, err := macrostrip.New(workerConfigDir)
	if err != nil {
		slog.Error("macro-strip 初期化失敗", "error", err)
		os.Exit(1)
	}
	downloadModeFn := func(dir domain.Direction) domain.DownloadMode {
		mode, _ := cfg.AttachmentDownload.DownloadModeFor(string(dir))
		return domain.DownloadMode(mode)
	}
	filesepWorker, err := filesep.New(workerConfigDir, attachStorage, repo, cfg.Notification.SMTPHost, cfg.Notification.SMTPPort, downloadModeFn)
	if err != nil {
		slog.Error("filesep-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	urlRewriteWorker, err := urlrewrite.New(workerConfigDir)
	if err != nil {
		slog.Error("url-rewrite-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	disclaimerWorker, err := disclaimer.New(workerConfigDir)
	if err != nil {
		slog.Error("disclaimer-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	arcSealerWorker, err := arcsealer.New(workerConfigDir)
	if err != nil {
		// 設定ファイルがない場合は警告のみ（オプション機能）
		slog.Warn("arc-sealer 初期化スキップ（設定ファイルなし・ARC シールは無効）", "error", err)
		arcSealerWorker = nil
	}

	builtinInspect := []domain.InspectWorker{
		avWorker,
		dlpWorker,
		headerWorker,
		urlCheckWorker,
		qrCheckWorker,
		attachCheckWorker,
	}
	builtinTransform := []domain.TransformWorker{
		sanitizeWorker,
		macroStripWorker,
		urlRewriteWorker,
		disclaimerWorker,
		filesepWorker,
	}
	if arcSealerWorker != nil {
		builtinTransform = append(builtinTransform, arcSealerWorker)
	}

	rt, err := router.New(cfg.Routes)
	if err != nil {
		slog.Error("ルーター初期化失敗", "error", err)
		os.Exit(1)
	}

	// 配送レジストリ: 名前付き deliverer（deliverers セクション）+ reinject 後方互換。
	// 全ルートのポリシーエンジンで共有する。
	deliverReg, err := deliver.NewRegistry(cfg.Deliverers, cfg.Reinject.Host, cfg.Reinject.Port)
	if err != nil {
		slog.Error("deliverer 初期化失敗", "error", err)
		os.Exit(1)
	}

	routeHandlers := make(map[string]*routeHandler, len(cfg.Routes))
	for i := range cfg.Routes {
		routeCfg := &cfg.Routes[i]

		slog.Debug("ルート初期化中",
			"route", routeCfg.Name,
			"direction", routeCfg.Direction,
			"workers_dir", cfg.Workers.WorkersDir,
		)

		mgr, err := worker.New(cfg.Workers.WorkersDir, cfg.Workers.WorkerConfigDir, &routeCfg.Workers, builtinInspect, builtinTransform)
		if err != nil {
			slog.Error("ワーカーロード失敗", "route", routeCfg.Name, "error", err)
			os.Exit(1)
		}

		pe, err := policy.New(routeCfg.Policy.RulesFile, deliverReg)
		if err != nil {
			slog.Error("ポリシーエンジン初期化失敗", "route", routeCfg.Name, "error", err)
			os.Exit(1)
		}

		routeHandlers[routeCfg.Name] = &routeHandler{
			cfg:       routeCfg,
			inspect:   pipeline.NewInspectPipeline(mgr.InspectEntries()),
			transform: pipeline.NewTransformPipeline(mgr.TransformWorkers()),
			policy:    pe,
		}

		slog.Info("ルート初期化完了",
			"route", routeCfg.Name,
			"direction", routeCfg.Direction,
			"inspect_workers", len(mgr.InspectWorkers()),
			"transform_workers", len(mgr.TransformWorkers()),
			"policy_file", routeCfg.Policy.RulesFile,
		)
	}

	var quarantineNotifier *notify.QuarantineNotifier
	if cfg.QuarantineNotification.Enabled {
		quarantineNotifier = notify.New(
			cfg.Notification.SMTPHost,
			cfg.Notification.SMTPPort,
			cfg.Notification.FromAddress,
			cfg.QuarantineNotification.UIBaseURL,
			cfg.Notification.SMTPConnectTimeoutSeconds,
			cfg.Notification.SMTPDeadlineSeconds,
		)
		slog.Info("隔離即時通知: 有効",
			"smtp_host", cfg.Notification.SMTPHost,
			"ui_base_url", cfg.QuarantineNotification.UIBaseURL,
		)
	} else {
		slog.Info("隔離即時通知: 無効")
	}

	mtr := metrics.New(version)

	handler := &mailHandler{
		storage:        emlStorage,
		archiveStorage: archiveStorage,
		repo:           repo,
		publisher:      publisher,
		router:         rt,
		routeHandlers:  routeHandlers,
		cfg:            &cfg.Server,
		approvalCfg:    cfg.Approval,
		notifier:       quarantineNotifier,
		metrics:        mtr,
	}

	smtpServer := smtp.New(smtp.Options{
		Port:                     cfg.Server.SMTPPort,
		Hostname:                 cfg.Server.SMTPHostname,
		TrustedSources:           cfg.Server.TrustedSources,
		MaxMessageSizeMB:         cfg.Server.MaxMessageSizeMB,
		MaxRecipients:            cfg.Server.MaxRecipients,
		ReadTimeoutSeconds:       cfg.Server.ReadTimeoutSeconds,
		WriteTimeoutSeconds:      cfg.Server.WriteTimeoutSeconds,
		HandlerTimeoutSeconds:    cfg.Server.HandlerTimeoutSeconds,
		DNSResolveTimeoutSeconds: cfg.Server.DNSResolveTimeoutSeconds,
	}, handler)

	// DefaultServeMux は使わない（他パッケージが登録したデバッグハンドラー等の意図しない公開を防ぐ）
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	healthMux.HandleFunc("/simulate", handler.handleSimulate)
	healthMux.Handle("/metrics", mtr.Handler())
	// /readyz は依存サービス（MariaDB）への疎通を含むレディネスチェック。
	// /healthz はプロセス生存確認のみ（liveness）として残す。
	healthMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := repo.Ping(ctx); err != nil {
			slog.Warn("/readyz: DB 疎通失敗", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "db unreachable")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	healthAddr := fmt.Sprintf(":%d", cfg.Server.HealthPort)
	httpServer := &http.Server{Addr: healthAddr, Handler: healthMux}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("ヘルスチェックサーバーエラー", "error", err)
		}
	}()

	serverErr := make(chan error, 1)
	go func() {
		if err := smtpServer.ListenAndServe(); err != nil {
			serverErr <- err
		}
	}()

	slog.Info("smtp-gateway 起動完了",
		"smtp_port", cfg.Server.SMTPPort,
		"health_port", cfg.Server.HealthPort,
		"routes", len(cfg.Routes),
	)

	// SIGTERM: コンテナ停止・systemd stop
	// SIGINT:  Ctrl+C（開発時）
	// SIGHUP:  将来の設定リロード用（現時点では再起動と同等に扱う）
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	select {
	case sig := <-quit:
		slog.Info("シグナル受信・シャットダウン開始", "signal", sig.String())
	case err := <-serverErr:
		slog.Error("SMTPサーバー異常終了", "error", err)
	}

	shutdownTimeout := time.Duration(cfg.Server.ShutdownTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	slog.Info("SMTPサーバー停止中（処理中セッション完了を待機）",
		"timeout_seconds", cfg.Server.ShutdownTimeoutSeconds)
	if err := smtpServer.GracefulClose(ctx); err != nil {
		slog.Warn("SMTPセッションのタイムアウト（強制終了）", "error", err)
	}

	slog.Info("非同期アーカイブの完了を待機中")
	handler.archiveWg.Wait()

	slog.Info("HTTPサーバー停止中")
	httpCtx, httpCancel := context.WithTimeout(context.Background(), time.Duration(cfg.Server.HTTPShutdownTimeoutSeconds)*time.Second)
	defer httpCancel()
	if err := httpServer.Shutdown(httpCtx); err != nil {
		slog.Warn("HTTPサーバーのシャットダウンに時間がかかりました", "error", err)
	}

	slog.Info("シャットダウン完了")
}

type routeHandler struct {
	cfg       *config.RouteConfig
	inspect   *pipeline.InspectPipeline
	transform *pipeline.TransformPipeline
	policy    *policy.Engine
}

type mailHandler struct {
	storage        domain.EMLStorage
	archiveStorage domain.ArchiveStorage
	repo           domain.MailRepository
	publisher      domain.EventPublisher
	router         *router.Router
	routeHandlers  map[string]*routeHandler
	cfg            *config.ServerConfig
	approvalCfg    config.ApprovalConfig
	notifier       *notify.QuarantineNotifier // nil の場合は通知しない
	metrics        *metrics.Metrics
	archiveWg      sync.WaitGroup
}

func (h *mailHandler) HandleMail(ctx context.Context, mail *domain.Mail) error {
	start := time.Now()
	defer func() {
		h.metrics.ObserveProcessing(time.Since(start).Seconds())
	}()
	log := slog.With(
		"message_id", mail.MessageID,
		"from", mail.FromAddress,
		"to", mail.ToAddresses,
		"size_bytes", mail.SizeBytes,
	)

	// 1. ルート解決（MAIL FROM / RCPT TO の正規表現マッチ）
	route, ok := h.router.Resolve(mail.FromAddress, mail.ToAddresses)
	if !ok {
		h.metrics.IncUnrouted()
		log.Warn("マッチするルートなし（メール拒否）",
			"from", mail.FromAddress,
			"to", mail.ToAddresses,
		)
		return fmt.Errorf("マッチするルートなし: from=%s to=%v: %w", mail.FromAddress, mail.ToAddresses, domain.ErrNoRouteMatched)
	}
	rh := h.routeHandlers[route.Name]
	mail.Direction = domain.Direction(route.Direction)
	h.metrics.IncReceived(route.Name)

	log.Info("[1/7] メール受信",
		"route", route.Name,
		"direction", route.Direction,
		"subject", mail.Subject,
	)

	// 2. MinIO に原本 EML を保存（失敗したら 451 を返し Postfix にリトライさせる）
	// 独立コンテキストを使い、ストレージ遅延がパイプライン（ステップ5-7）のタイムアウト予算を消費しないようにする
	log.Debug("[2/7] MinIO へ原本 EML を保存中")
	{
		saveCtx, saveCancel := context.WithTimeout(context.Background(), time.Duration(h.cfg.StorageSaveTimeoutSeconds)*time.Second)
		path, err := h.storage.Save(saveCtx, mail.MessageID, mail.RawEML, mail.ReceivedAt)
		saveCancel()
		if err != nil {
			h.metrics.IncError("storage_save")
			log.Error("[2/7] EML 保存失敗（451 を返す）", "error", err)
			return fmt.Errorf("EML 保存失敗: %w", err)
		}
		mail.EMLPath = path
	}
	log.Info("[2/7] EML 保存完了", "eml_path", mail.EMLPath)

	// 3. MariaDB にメタデータを記録（失敗してもログだけで続行）
	log.Debug("[3/7] MariaDB へメタデータ記録中")
	{
		dbCtx, dbCancel := context.WithTimeout(context.Background(), time.Duration(h.cfg.DBSaveTimeoutSeconds)*time.Second)
		if err := h.repo.SaveMessage(dbCtx, mail); err != nil {
			log.Warn("[3/7] DB メタデータ保存失敗（続行）", "error", err)
		} else {
			log.Debug("[3/7] DB メタデータ記録完了")
		}
		dbCancel()
	}

	// 4. mail.received 統合イベントを発行（失敗してもログだけで続行）
	log.Debug("[4/7] mail.received イベント発行中")
	{
		mqCtx, mqCancel := context.WithTimeout(context.Background(), time.Duration(h.cfg.EventPublishTimeoutSeconds)*time.Second)
		event := toMailEvent(mail)
		if err := h.publisher.PublishMailReceived(mqCtx, event); err != nil {
			log.Warn("[4/7] mail.received 発行失敗（続行）", "error", err)
		} else {
			log.Debug("[4/7] mail.received 発行完了")
		}
		mqCancel()
	}

	// 5. 検査パイプライン（並列）
	log.Info("[5/7] 検査パイプライン開始", "route", route.Name)
	inspectResults, err := rh.inspect.Run(ctx, mail)
	if err != nil {
		log.Warn("[5/7] 検査パイプラインエラー（続行）", "error", err)
	}
	for _, r := range inspectResults {
		log.Info("[5/7] 検査結果",
			"worker", r.WorkerName,
			"detected", r.Detected,
			"score", r.Score,
		)
		if r.Detected {
			h.metrics.IncDetected(route.Name, r.WorkerName)
		}
		if err := h.repo.SaveInspectResult(ctx, r, mail.MessageID); err != nil {
			log.Warn("[5/7] 検査結果 DB 保存失敗（続行）",
				"worker", r.WorkerName, "error", err)
		}
	}

	// 6. 変換パイプライン（直列）
	log.Info("[6/7] 変換パイプライン開始", "route", route.Name)
	transformed, err := rh.transform.Run(ctx, mail)
	if err != nil {
		// 変換失敗時は未処理メールを配送せず隔離する。
		// sanitize / urlrewrite 等の効果が得られないまま配送するとセキュリティリスクになる。
		log.Error("[6/7] 変換パイプライン失敗: 未処理メールの配送を防ぐため隔離します",
			"route", route.Name, "error", err)
		h.metrics.IncError("transform")
		h.metrics.IncAction(route.Name, string(policy.ActionQuarantine))
		if uerr := h.repo.UpdateMessageStatus(ctx, mail.MessageID, domain.StatusQuarantined); uerr != nil {
			log.Warn("ステータス更新失敗（続行）", "error", uerr)
		}
		h.archiveWg.Add(1)
		go h.archiveAsync(mail.MessageID, mail.RawEML, mail.ReceivedAt)
		if h.notifier != nil {
			h.notifier.SendAsync(mail.MessageID, mail.Subject, mail.FromAddress, mail.ToAddresses)
		}
		return nil // Postfix には 250 OK を返す（メールは隔離済み）
	}
	if transformed.Subject != mail.Subject {
		log.Info("[6/7] 件名変換完了",
			"original_subject", mail.Subject,
			"new_subject", transformed.Subject,
		)
	} else {
		log.Debug("[6/7] 変換なし")
	}

	// 7. ポリシーエンジンでアクション決定・実行
	log.Info("[7/7] ポリシー評価開始", "route", route.Name)
	result, err := rh.policy.EvaluateAndAct(ctx, transformed, inspectResults)
	if err != nil {
		h.metrics.IncError("policy")
		log.Error("[7/7] ポリシーエンジンエラー", "error", err)
		return fmt.Errorf("ポリシー実行失敗: %w", err)
	}
	action := result.Action

	// B-3: マッチするルールがない場合はメールを消失させず 550 を返す
	if action == "" {
		h.metrics.IncError("no_rule")
		log.Error("[7/7] マッチするポリシールールがありません。policy.yaml にデフォルトルールを追加してください",
			"route", route.Name, "message_id", mail.MessageID)
		return domain.ErrNoRuleMatched
	}
	h.metrics.IncAction(route.Name, string(action))

	actionStatusMap := map[policy.ActionType]domain.MessageStatus{
		policy.ActionDeliver:    domain.StatusDelivered,
		policy.ActionReject:     domain.StatusRejected,
		policy.ActionQuarantine: domain.StatusQuarantined,
		policy.ActionApproval:   domain.StatusApprovalPending,
		policy.ActionDelay:      domain.StatusDelayed,
	}
	if status, ok := actionStatusMap[action]; ok {
		if err := h.repo.UpdateMessageStatus(ctx, mail.MessageID, status); err != nil {
			log.Warn("ステータス更新失敗（続行）", "error", err)
		}
	}

	// アーカイブを非同期実行（配送フローをブロックしない）。
	// delay は後で自動配送する際に処理済み EML を取得するため、必ずアーカイブする。
	switch action {
	case policy.ActionDeliver, policy.ActionApproval, policy.ActionQuarantine, policy.ActionDelay:
		h.archiveWg.Add(1)
		go h.archiveAsync(mail.MessageID, transformed.RawEML, mail.ReceivedAt)
	}

	// 隔離時は受信者へ即時通知（設定で無効化可能）
	if action == policy.ActionQuarantine && h.notifier != nil {
		h.notifier.SendAsync(mail.MessageID, mail.Subject, mail.FromAddress, mail.ToAddresses)
	}

	// 承認フロー保留時は approval_requests レコードを作成する
	if action == policy.ActionApproval {
		if err := h.createApprovalRequest(ctx, mail, log); err != nil {
			log.Warn("承認依頼レコード作成失敗（続行）", "message_id", mail.MessageID, "error", err)
		}
	}

	// 送信ディレイ保留時は delayed_releases レコードを作成する
	if action == policy.ActionDelay {
		rel := &domain.DelayedRelease{
			ID:        uuid.New().String(),
			MessageID: mail.MessageID,
			ReleaseAt: time.Now().Add(time.Duration(result.DelayMinutes) * time.Minute),
		}
		if err := h.repo.SaveDelayedRelease(ctx, rel); err != nil {
			log.Warn("遅延送信レコード作成失敗（続行）", "message_id", mail.MessageID, "error", err)
		} else {
			log.Info("送信ディレイ保留レコード作成",
				"message_id", mail.MessageID, "release_at", rel.ReleaseAt)
		}
	}

	// mail.processed 統合イベントを発行（失敗してもログだけで続行）
	{
		procCtx, procCancel := context.WithTimeout(context.Background(), time.Duration(h.cfg.EventPublishTimeoutSeconds)*time.Second)
		procEvent := toProcessedEvent(route.Name, route.Direction, string(action), transformed, inspectResults)
		if err := h.publisher.PublishMailProcessed(procCtx, procEvent); err != nil {
			log.Warn("mail.processed 発行失敗（続行）", "error", err)
		}
		procCancel()
	}

	log.Info("メール処理完了",
		"route", route.Name,
		"action", string(action),
		"elapsed_ms", time.Since(start).Milliseconds(),
	)

	// B-2: reject アクションは SMTP 層へエラーを伝播して 550 を返させる
	if action == policy.ActionReject {
		return domain.ErrMailRejected
	}
	return nil
}

func (h *mailHandler) archiveAsync(messageID string, eml []byte, receivedAt time.Time) {
	defer h.archiveWg.Done()
	timeout := time.Duration(h.cfg.ShutdownTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	maxRetries := h.cfg.ArchiveMaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	backoffSeconds := h.cfg.ArchiveRetryBackoffSeconds
	if backoffSeconds <= 0 {
		backoffSeconds = 2
	}
	var (
		path string
		err  error
	)
	for i := range maxRetries {
		path, err = h.archiveStorage.ArchiveProcessed(ctx, messageID, eml, receivedAt)
		if err == nil {
			break
		}
		slog.Warn("アーカイブ失敗（リトライ）",
			"message_id", messageID,
			"attempt", i+1,
			"error", err,
		)
		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(i+1) * time.Duration(backoffSeconds) * time.Second):
			}
		}
	}
	if err != nil {
		slog.Error("アーカイブ最終失敗（隔離解放不可・手動対応が必要）",
			"message_id", messageID,
			"error", err,
		)
		return
	}

	if err := h.repo.UpdateProcessedEMLPath(ctx, messageID, path); err != nil {
		slog.Warn("processed_eml_path 更新失敗（続行）",
			"message_id", messageID,
			"error", err,
		)
	}
}

// createApprovalRequest は承認依頼レコードを作成する。
//
// 承認者の解決順:
//  1. メールボックスの承認者（role=admin）: outbound は送信元、inbound は宛先の
//     メールボックスを調べ、admin 割り当てが 1 人以上いる**すべての**メールボックスを
//     依頼の対象にする（いずれかのメールボックスの admin が承認すれば配送される）
//  2. ユーザー個人の承認者（users.approver_id）: 送信者 → 受信者
//  3. グローバルフォールバック（approval.global_approver_email）
func (h *mailHandler) createApprovalRequest(ctx context.Context, mail *domain.Mail, log *slog.Logger) error {
	// 1. メールボックス承認者（role=admin）
	candidates := mail.ToAddresses
	if mail.Direction == domain.DirectionOutbound {
		candidates = []string{mail.FromAddress}
	}
	var mailboxEmails []string
	for _, addr := range candidates {
		count, err := h.repo.CountMailboxAdmins(ctx, addr)
		if err != nil {
			log.Warn("メールボックス承認者解決エラー（続行）", "mailbox", addr, "error", err)
			continue
		}
		if count > 0 {
			mailboxEmails = append(mailboxEmails, addr)
		}
	}

	// 2. ユーザー個人の承認者（送信者 → 受信者）
	var approverID string
	if len(mailboxEmails) == 0 {
		var err error
		approverID, err = h.repo.FindApproverForSender(ctx, mail.FromAddress)
		if err != nil {
			log.Warn("承認者解決エラー（送信者）", "from", mail.FromAddress, "error", err)
		}

		if approverID == "" && len(mail.ToAddresses) > 0 {
			approverID, err = h.repo.FindApproverForSender(ctx, mail.ToAddresses[0])
			if err != nil {
				log.Warn("承認者解決エラー（受信者）", "to", mail.ToAddresses[0], "error", err)
			}
		}

		// 3. グローバルフォールバック
		if approverID == "" && h.approvalCfg.GlobalApproverEmail != "" {
			approverID, err = h.repo.FindUserIDByEmail(ctx, h.approvalCfg.GlobalApproverEmail)
			if err != nil {
				log.Warn("承認者解決エラー（グローバル）", "email", h.approvalCfg.GlobalApproverEmail, "error", err)
			}
		}
	}

	if len(mailboxEmails) == 0 && approverID == "" {
		return fmt.Errorf("承認者を解決できません (message_id=%s, from=%s)", mail.MessageID, mail.FromAddress)
	}

	expiryHours := h.approvalCfg.ExpiryHours
	if expiryHours <= 0 {
		expiryHours = 72
	}

	req := &domain.ApprovalRequest{
		ID:            uuid.New().String(),
		MessageID:     mail.MessageID,
		ApproverID:    approverID,
		MailboxEmails: mailboxEmails,
		ExpiresAt:     time.Now().Add(time.Duration(expiryHours) * time.Hour),
	}
	if err := h.repo.SaveApprovalRequest(ctx, req); err != nil {
		return err
	}

	log.Info("承認依頼レコード作成",
		"message_id", mail.MessageID,
		"approver_id", approverID,
		"mailbox_emails", mailboxEmails,
		"expires_at", req.ExpiresAt,
	)
	return nil
}

// SimulateResult は /simulate エンドポイントのレスポンス型。
// TransformedEML はデバッグ・テスト専用で本番用途ではない。
type SimulateResult struct {
	RouteName          string                  `json:"route_name"`
	Direction          string                  `json:"direction"`
	InspectResults     []simulateInspectResult `json:"inspect_results"`
	OriginalSubject    string                  `json:"original_subject"`
	TransformedSubject string                  `json:"transformed_subject"`
	SubjectChanged     bool                    `json:"subject_changed"`
	TransformedEML     string                  `json:"transformed_eml"`
	TransformError     string                  `json:"transform_error,omitempty"`
	Action             string                  `json:"action"`
	MatchedRule        string                  `json:"matched_rule"`
	ProcessingMS       int64                   `json:"processing_ms"`
}

type simulateInspectResult struct {
	Worker   string         `json:"worker"`
	Detected bool           `json:"detected"`
	Score    int            `json:"score"`
	Details  map[string]any `json:"details"`
}

// handleSimulate は POST /simulate を処理する。
// リクエストボディに生の EML バイト列を渡す（最大 10MB）。
func (h *mailHandler) handleSimulate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawEML, err := io.ReadAll(io.LimitReader(r.Body, int64(h.cfg.MaxMessageSizeMB)*1024*1024))
	if err != nil || len(rawEML) == 0 {
		http.Error(w, "request body required (raw EML)", http.StatusBadRequest)
		return
	}

	start := time.Now()

	msg, err := mail.ReadMessage(bytes.NewReader(rawEML))
	if err != nil {
		http.Error(w, "invalid EML: "+err.Error(), http.StatusBadRequest)
		return
	}
	fromAddr := parseFirstAddress(msg.Header.Get("From"))
	toAddrs := parseAddressList(msg.Header.Get("To"))
	subject := msg.Header.Get("Subject")

	m := &domain.Mail{
		MessageID:   "simulate-" + uuid.New().String(),
		RawEML:      rawEML,
		ReceivedAt:  time.Now().UTC(),
		FromAddress: fromAddr,
		ToAddresses: toAddrs,
		Subject:     subject,
		SizeBytes:   int64(len(rawEML)),
		AuthResults: domain.DefaultAuthResults(),
	}

	route, ok := h.router.Resolve(m.FromAddress, m.ToAddresses)
	if !ok {
		http.Error(w, "no matching route", http.StatusUnprocessableEntity)
		return
	}
	rh := h.routeHandlers[route.Name]
	m.Direction = domain.Direction(route.Direction)

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.cfg.SimulateTimeoutSeconds)*time.Second)
	defer cancel()
	// ドライランフラグを付与: 副作用を持つワーカー（filesep 等）は保存を省略する
	ctx = context.WithValue(ctx, domain.CtxDryRun, true)

	// 検査パイプライン（ドライラン: DB 保存なし）
	inspectResults, _ := rh.inspect.Run(ctx, m)

	// 変換パイプライン（ドライラン: storage 保存なし）
	transformed, transformErr := rh.transform.Run(ctx, m)

	// 変換失敗時は実配送経路（[6/7]）と同じセマンティクスで隔離として報告する。
	// ポリシー評価をスキップして quarantine を返し、「変換なしで配送される」という
	// 実際の動作と食い違う結果を返さない。
	var (
		action          policy.ActionType
		matchedRule     string
		transformErrMsg string
	)
	if transformErr != nil {
		slog.Warn("simulate: 変換パイプライン失敗（実配送経路では隔離される）",
			"message_id", m.MessageID, "error", transformErr)
		transformed = m
		transformErrMsg = transformErr.Error()
		action = policy.ActionQuarantine
	} else {
		action, matchedRule = rh.policy.Evaluate(transformed, inspectResults)
	}
	out := SimulateResult{
		RouteName:          route.Name,
		Direction:          route.Direction,
		OriginalSubject:    m.Subject,
		TransformedSubject: transformed.Subject,
		SubjectChanged:     transformed.Subject != m.Subject,
		TransformedEML:     string(transformed.RawEML),
		TransformError:     transformErrMsg,
		Action:             string(action),
		MatchedRule:        matchedRule,
		ProcessingMS:       time.Since(start).Milliseconds(),
	}
	for _, r := range inspectResults {
		details := r.Details
		if details == nil {
			details = map[string]any{}
		}
		out.InspectResults = append(out.InspectResults, simulateInspectResult{
			Worker:   r.WorkerName,
			Detected: r.Detected,
			Score:    r.Score,
			Details:  details,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		slog.Warn("simulate: レスポンス書き込み失敗", "error", err)
	}
}

func parseFirstAddress(raw string) string {
	if raw == "" {
		return ""
	}
	addrs, err := mail.ParseAddressList(raw)
	if err != nil || len(addrs) == 0 {
		return strings.TrimSpace(raw)
	}
	return addrs[0].Address
}

func parseAddressList(raw string) []string {
	if raw == "" {
		return nil
	}
	addrs, err := mail.ParseAddressList(raw)
	if err != nil {
		return []string{strings.TrimSpace(raw)}
	}
	result := make([]string, len(addrs))
	for i, a := range addrs {
		result[i] = a.Address
	}
	return result
}

func toMailEvent(mail *domain.Mail) *domain.MailEvent {
	return &domain.MailEvent{
		MessageID:     mail.MessageID,
		EMLPath:       mail.EMLPath,
		ReceivedAt:    mail.ReceivedAt.Format(time.RFC3339),
		FromAddress:   mail.FromAddress,
		ToAddresses:   mail.ToAddresses,
		Subject:       mail.Subject,
		SizeBytes:     mail.SizeBytes,
		HasAttachment: mail.HasAttachment,
		RspamdScore:   mail.RspamdScore,
		AuthResults:   mail.AuthResults,
	}
}

func toProcessedEvent(route, direction, action string, mail *domain.Mail, results []*domain.InspectResult) *domain.MailProcessedEvent {
	scores := make([]domain.InspectScore, 0, len(results))
	total := 0
	for _, r := range results {
		scores = append(scores, domain.InspectScore{
			Worker:   r.WorkerName,
			Score:    r.Score,
			Detected: r.Detected,
		})
		total += r.Score
	}
	return &domain.MailProcessedEvent{
		MessageID:     mail.MessageID,
		Route:         route,
		Direction:     direction,
		Action:        action,
		FromAddress:   mail.FromAddress,
		ToAddresses:   mail.ToAddresses,
		Subject:       mail.Subject,
		TotalScore:    total,
		InspectScores: scores,
		ProcessedAt:   time.Now().UTC().Format(time.RFC3339),
	}
}
