// Package main は smtp-gateway サービスのエントリーポイントである。
// DIのみを行い、ビジネスロジックは書かない。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/logging"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/notify"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/pipeline"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/policy"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/queue"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/repository/mariadb"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/router"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/smtp"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/storage"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/clamav"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/filesep"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/header"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/qrcheck"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/sanitize"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/tika"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/urlcheck"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/urlrewrite"
)

func main() {
	// ─── 設定読み込み（ログ初期化前なので stderr に出力）─────
	configFile := os.Getenv("CONFIG_FILE")
	if configFile == "" {
		configFile = "config/mailshield.yaml"
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "設定読み込み失敗: %v\n", err)
		os.Exit(1)
	}

	// ─── ログ初期化 ───────────────────────────────────────────
	if err := logging.Setup(&cfg.Log); err != nil {
		fmt.Fprintf(os.Stderr, "ログ初期化失敗: %v\n", err)
		os.Exit(1)
	}

	slog.Info("smtp-gateway 起動中",
		"version", "0.1.0",
		"config", configFile,
		"log_level", cfg.Log.Level,
		"log_output", cfg.Log.Output,
	)

	// ─── MinIO ───────────────────────────────────────────────
	slog.Debug("MinIO 初期化", "endpoint", cfg.Storage.Endpoint)
	emlStorage, err := storage.New(
		cfg.Storage.Endpoint,
		cfg.Storage.AccessKey,
		cfg.Storage.SecretKey,
		cfg.Storage.BucketEML,
		cfg.Storage.BucketAttachments,
		cfg.Storage.UseSSL,
	)
	if err != nil {
		slog.Error("MinIO 初期化失敗", "error", err)
		os.Exit(1)
	}
	slog.Info("MinIO 接続完了", "endpoint", cfg.Storage.Endpoint)

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
	defer repo.Close()
	slog.Info("MariaDB 接続完了", "host", cfg.Database.Host)

	// ─── RabbitMQ ────────────────────────────────────────────
	slog.Debug("RabbitMQ 初期化", "host", cfg.Queue.Host, "port", cfg.Queue.Port)
	amqpURL := fmt.Sprintf("amqp://%s:%s@%s:%d/",
		cfg.Queue.User,
		cfg.Queue.Password,
		cfg.Queue.Host,
		cfg.Queue.Port,
	)
	publisher, err := queue.New(amqpURL)
	if err != nil {
		slog.Error("RabbitMQ 初期化失敗", "error", err)
		os.Exit(1)
	}
	defer publisher.Close()
	slog.Info("RabbitMQ 接続完了", "host", cfg.Queue.Host)

	// ─── 組み込みワーカー初期化（ルート間で共有）────────────
	// ワーカーインスタンスはステートレスなので全ルートで共有する。
	// どのルートで有効化するかは各ルートの WorkersConfig で制御する。
	configDir := cfg.Routes[0].Workers.WorkerConfigDir // worker config dir はルート間共通
	if len(cfg.Routes) == 0 {
		slog.Error("routes が設定されていません")
		os.Exit(1)
	}

	avWorker, err := clamav.New(configDir)
	if err != nil {
		slog.Error("av-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	dlpWorker, err := tika.New(configDir)
	if err != nil {
		slog.Error("dlp-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	headerWorker, err := header.New(configDir)
	if err != nil {
		slog.Error("header-inspector 初期化失敗", "error", err)
		os.Exit(1)
	}
	urlCheckWorker, err := urlcheck.New(configDir)
	if err != nil {
		slog.Error("url-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	qrCheckWorker, err := qrcheck.New(configDir)
	if err != nil {
		slog.Error("qr-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	sanitizeWorker, err := sanitize.New(configDir)
	if err != nil {
		slog.Error("sanitize-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	downloadModeFn := func(dir domain.Direction) domain.DownloadMode {
		mode, _ := cfg.AttachmentDownload.DownloadModeFor(string(dir))
		return domain.DownloadMode(mode)
	}
	filesepWorker, err := filesep.New(configDir, emlStorage, repo, cfg.Server.ReinjectHost, cfg.Server.ReinjectPort, downloadModeFn)
	if err != nil {
		slog.Error("filesep-worker 初期化失敗", "error", err)
		os.Exit(1)
	}
	urlRewriteWorker, err := urlrewrite.New(configDir)
	if err != nil {
		slog.Error("url-rewrite-worker 初期化失敗", "error", err)
		os.Exit(1)
	}

	builtinInspect := []domain.InspectWorker{
		avWorker,
		dlpWorker,
		headerWorker,
		urlCheckWorker,
		qrCheckWorker,
	}
	builtinTransform := []domain.TransformWorker{
		sanitizeWorker,
		urlRewriteWorker,
		filesepWorker,
	}

	// ─── ルーター初期化 ───────────────────────────────────────
	rt, err := router.New(cfg.Routes)
	if err != nil {
		slog.Error("ルーター初期化失敗", "error", err)
		os.Exit(1)
	}

	// ─── ルートごとのワーカーマネージャー・ポリシーエンジン ──
	routeHandlers := make(map[string]*routeHandler, len(cfg.Routes))
	for i := range cfg.Routes {
		routeCfg := &cfg.Routes[i]

		slog.Debug("ルート初期化中",
			"route", routeCfg.Name,
			"direction", routeCfg.Direction,
			"workers_dir", routeCfg.Workers.WorkersDir,
		)

		mgr, err := worker.New(&routeCfg.Workers, builtinInspect, builtinTransform)
		if err != nil {
			slog.Error("ワーカーロード失敗", "route", routeCfg.Name, "error", err)
			os.Exit(1)
		}

		pe, err := policy.New(routeCfg.Policy.RulesFile)
		if err != nil {
			slog.Error("ポリシーエンジン初期化失敗", "route", routeCfg.Name, "error", err)
			os.Exit(1)
		}

		routeHandlers[routeCfg.Name] = &routeHandler{
			cfg:       routeCfg,
			inspect:   pipeline.NewInspectPipeline(mgr.InspectWorkers()),
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

	// ─── 隔離即時通知 ─────────────────────────────────────────
	var quarantineNotifier *notify.QuarantineNotifier
	if cfg.QuarantineNotification.Enabled {
		quarantineNotifier = notify.New(
			cfg.Notification.SMTPHost,
			cfg.Notification.SMTPPort,
			cfg.Notification.FromAddress,
			cfg.QuarantineNotification.UIBaseURL,
		)
		slog.Info("隔離即時通知: 有効",
			"smtp_host", cfg.Notification.SMTPHost,
			"ui_base_url", cfg.QuarantineNotification.UIBaseURL,
		)
	} else {
		slog.Info("隔離即時通知: 無効")
	}

	// ─── メール処理ハンドラー ─────────────────────────────────
	handler := &mailHandler{
		storage:        emlStorage,
		archiveStorage: emlStorage,
		repo:           repo,
		publisher:      publisher,
		router:         rt,
		routeHandlers:  routeHandlers,
		cfg:            &cfg.Server,
		notifier:       quarantineNotifier,
	}

	// ─── SMTP サーバー ────────────────────────────────────────
	smtpServer := smtp.New(smtp.Options{
		Port:                  cfg.Server.SMTPPort,
		Hostname:              cfg.Server.SMTPHostname,
		TrustedSources:        cfg.Server.TrustedSources,
		MaxMessageSizeMB:      cfg.Server.MaxMessageSizeMB,
		MaxRecipients:         cfg.Server.MaxRecipients,
		ReadTimeoutSeconds:    cfg.Server.ReadTimeoutSeconds,
		WriteTimeoutSeconds:   cfg.Server.WriteTimeoutSeconds,
		HandlerTimeoutSeconds: cfg.Server.HandlerTimeoutSeconds,
	}, handler)

	// ─── ヘルスチェックエンドポイント ────────────────────────
	healthAddr := fmt.Sprintf(":%d", cfg.Server.HealthPort)
	httpServer := &http.Server{Addr: healthAddr}
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("ヘルスチェックサーバーエラー", "error", err)
		}
	}()

	// ─── SMTP サーバー起動 ────────────────────────────────────
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

	// ─── シグナル待機 ─────────────────────────────────────────
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

	// ─── グレースフルシャットダウン ───────────────────────────
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
	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Warn("HTTPサーバーのシャットダウンに時間がかかりました", "error", err)
	}

	slog.Info("シャットダウン完了")
}

// ────────────────────────────────────────────────────────────
// routeHandler: ルートごとのパイプライン・ポリシーエンジン
// ────────────────────────────────────────────────────────────

type routeHandler struct {
	cfg       *config.RouteConfig
	inspect   *pipeline.InspectPipeline
	transform *pipeline.TransformPipeline
	policy    *policy.Engine
}

// ────────────────────────────────────────────────────────────
// mailHandler: SMTP セッションから呼ばれるメール処理の本体
// ────────────────────────────────────────────────────────────

type mailHandler struct {
	storage        domain.EMLStorage
	archiveStorage domain.ArchiveStorage
	repo           domain.MailRepository
	publisher      domain.EventPublisher
	router         *router.Router
	routeHandlers  map[string]*routeHandler
	cfg            *config.ServerConfig
	notifier       *notify.QuarantineNotifier // nil の場合は通知しない
	archiveWg      sync.WaitGroup
}

func (h *mailHandler) HandleMail(ctx context.Context, mail *domain.Mail) error {
	start := time.Now()
	log := slog.With(
		"message_id", mail.MessageID,
		"from", mail.FromAddress,
		"to", mail.ToAddresses,
		"size_bytes", mail.SizeBytes,
	)

	// 1. ルート解決（MAIL FROM / RCPT TO の正規表現マッチ）
	route, ok := h.router.Resolve(mail.FromAddress, mail.ToAddresses)
	if !ok {
		log.Warn("マッチするルートなし（メール拒否）",
			"from", mail.FromAddress,
			"to", mail.ToAddresses,
		)
		return fmt.Errorf("マッチするルートなし: from=%s to=%v", mail.FromAddress, mail.ToAddresses)
	}
	rh := h.routeHandlers[route.Name]
	mail.Direction = domain.Direction(route.Direction)

	log.Info("[1/7] メール受信",
		"route", route.Name,
		"direction", route.Direction,
		"subject", mail.Subject,
	)

	// 2. MinIO に原本 EML を保存（失敗したら 451 を返し Postfix にリトライさせる）
	log.Debug("[2/7] MinIO へ原本 EML を保存中")
	path, err := h.storage.Save(ctx, mail.MessageID, mail.RawEML, mail.ReceivedAt)
	if err != nil {
		log.Error("[2/7] EML 保存失敗（451 を返す）", "error", err)
		return fmt.Errorf("EML 保存失敗: %w", err)
	}
	mail.EMLPath = path
	log.Info("[2/7] EML 保存完了", "eml_path", mail.EMLPath)

	// 3. MariaDB にメタデータを記録（失敗してもログだけで続行）
	log.Debug("[3/7] MariaDB へメタデータ記録中")
	if err := h.repo.SaveMessage(ctx, mail); err != nil {
		log.Warn("[3/7] DB メタデータ保存失敗（続行）", "error", err)
	} else {
		log.Debug("[3/7] DB メタデータ記録完了")
	}

	// 4. RabbitMQ に mail.received を発行（失敗してもログだけで続行）
	log.Debug("[4/7] RabbitMQ へ mail.received を発行中")
	event := toMailEvent(mail)
	if err := h.publisher.PublishMailReceived(ctx, event); err != nil {
		log.Warn("[4/7] mail.received 発行失敗（続行）", "error", err)
	} else {
		log.Debug("[4/7] mail.received 発行完了")
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
		if err := h.repo.SaveInspectResult(ctx, r, mail.MessageID); err != nil {
			log.Warn("[5/7] 検査結果 DB 保存失敗（続行）",
				"worker", r.WorkerName, "error", err)
		}
	}

	// 6. 変換パイプライン（直列）
	log.Info("[6/7] 変換パイプライン開始", "route", route.Name)
	transformed, err := rh.transform.Run(ctx, mail)
	if err != nil {
		log.Warn("[6/7] 変換パイプラインエラー（元のメールで続行）", "error", err)
		transformed = mail
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
	action, err := rh.policy.EvaluateAndAct(ctx, transformed, inspectResults)
	if err != nil {
		log.Error("[7/7] ポリシーエンジンエラー", "error", err)
		return fmt.Errorf("ポリシー実行失敗: %w", err)
	}

	// アクション種別に対応する DB ステータスへ更新
	actionStatusMap := map[policy.ActionType]domain.MessageStatus{
		policy.ActionDeliver:    domain.StatusDelivered,
		policy.ActionReject:     domain.StatusRejected,
		policy.ActionQuarantine: domain.StatusQuarantined,
		policy.ActionApproval:   domain.StatusApprovalPending,
	}
	if status, ok := actionStatusMap[action]; ok {
		if err := h.repo.UpdateMessageStatus(ctx, mail.MessageID, status); err != nil {
			log.Warn("ステータス更新失敗（続行）", "error", err)
		}
	}

	// アーカイブを非同期実行（配送フローをブロックしない）
	switch action {
	case policy.ActionDeliver, policy.ActionApproval, policy.ActionQuarantine:
		h.archiveWg.Add(1)
		go h.archiveAsync(mail.MessageID, transformed.RawEML, mail.ReceivedAt)
	}

	// 隔離時は受信者へ即時通知（設定で無効化可能）
	if action == policy.ActionQuarantine && h.notifier != nil {
		h.notifier.SendAsync(mail.MessageID, mail.Subject, mail.FromAddress, mail.ToAddresses)
	}

	log.Info("メール処理完了",
		"route", route.Name,
		"action", string(action),
		"elapsed_ms", time.Since(start).Milliseconds(),
	)
	return nil
}

// archiveAsync は変換後の EML を非同期で MinIO に保存し、保存パスを DB に記録する。
func (h *mailHandler) archiveAsync(messageID string, eml []byte, receivedAt time.Time) {
	defer h.archiveWg.Done()
	timeout := time.Duration(h.cfg.ShutdownTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	const maxRetries = 3
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
			case <-time.After(time.Duration(i+1) * 2 * time.Second):
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
