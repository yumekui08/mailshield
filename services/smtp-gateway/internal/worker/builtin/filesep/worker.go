// Package filesep は添付ファイル分離変換ワーカーを実装する。
// 分離した添付ファイルは MinIO に保存し、ダウンロードトークン付きリンクをメール本文に挿入する。
package filesep

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jhillyerd/enmime"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/eml"
)

// DownloadModeFn はメールの方向からダウンロードモードを解決する関数型。
// main.go から設定に基づいて注入される。
type DownloadModeFn func(direction domain.Direction) domain.DownloadMode

// Worker は添付ファイル分離変換ワーカーである。
type Worker struct {
	cfg            *Config
	storage        domain.AttachmentStorage
	repo           domain.MailRepository
	notifSMTPHost  string
	notifSMTPPort  int
	downloadModeFn DownloadModeFn
}

// New は filesep-worker を初期化する。
// workerConfigDir から filesep-worker.yaml を読み込む。
// notifSMTPHost / notifSMTPPort は separate モードで通知メールを送信する SMTP サーバーの接続先。
// notification.smtp_host / notification.smtp_port（mailshield.yaml）と同じ値を渡すこと。
// downloadModeFn が nil の場合は常に simple モードを使用する。
func New(workerConfigDir string, storage domain.AttachmentStorage, repo domain.MailRepository, notifSMTPHost string, notifSMTPPort int, downloadModeFn DownloadModeFn) (*Worker, error) {
	cfg, err := loadConfig(workerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("filesep-worker 設定ロード失敗: %w", err)
	}
	if downloadModeFn == nil {
		downloadModeFn = func(_ domain.Direction) domain.DownloadMode { return domain.DownloadModeSimple }
	}
	return &Worker{
		cfg:            cfg,
		storage:        storage,
		repo:           repo,
		notifSMTPHost:  notifSMTPHost,
		notifSMTPPort:  notifSMTPPort,
		downloadModeFn: downloadModeFn,
	}, nil
}

func (w *Worker) Name() string { return workerName }

// Transform は EML を解析し、条件に一致する添付ファイルを分離する。
// 分離した添付ファイルは MinIO に保存し、署名付き URL をメール本文に挿入する。
// ctx に domain.CtxDryRun が付与されている場合は保存・DB書き込みを省略する。
func (w *Worker) Transform(ctx context.Context, mail *domain.Mail) (*domain.Mail, error) {
	if ctx.Value(domain.CtxDryRun) != nil {
		return mail, nil
	}

	env, err := enmime.ReadEnvelope(bytes.NewReader(mail.RawEML))
	if err != nil {
		return nil, fmt.Errorf("EML パース失敗: %w", err)
	}

	// 分離対象の添付ファイルを選別
	var toSeparate []*enmime.Part
	for _, att := range env.Attachments {
		if w.cfg.shouldSeparate(att.FileName, int64(len(att.Content))) {
			toSeparate = append(toSeparate, att)
		}
	}
	if len(toSeparate) == 0 {
		return mail, nil
	}

	// メッセージ単位のダウンロードトークン・URL・モードを先に決定
	downloadToken := uuid.New().String()
	frontendURL := strings.TrimRight(w.cfg.FrontendURL, "/")
	downloadURL := fmt.Sprintf("%s/files/%s", frontendURL, downloadToken)
	downloadMode := w.downloadModeFn(mail.Direction)

	// 各添付ファイルを MinIO へ保存（トークン・モードはメッセージ単位で共有）
	attachInfos, err := w.saveAndRecord(ctx, mail, toSeparate, downloadToken, downloadMode)
	if err != nil {
		return nil, err
	}

	// テンプレートをレンダリング
	tmplData := buildTemplateData(mail.Subject, mail.ReceivedAt, w.cfg.LinkExpiryHours, downloadURL, attachInfos)
	inlineText, err := renderTemplate(w.cfg.InlineTemplate, defaultInlineTemplate, tmplData)
	if err != nil {
		return nil, err
	}

	// 添付を除いた EML を再構築
	separatedSet := make(map[*enmime.Part]bool, len(toSeparate))
	for _, p := range toSeparate {
		separatedSet[p] = true
	}
	newEML, err := w.rebuildEML(env, separatedSet, inlineText, mail)
	if err != nil {
		return nil, err
	}

	slog.Info("添付ファイル分離完了",
		"worker", workerName,
		"message_id", mail.MessageID,
		"separated_count", len(toSeparate),
		"mode", w.cfg.Mode,
	)

	// separate モードは通知メールを別送
	if w.cfg.Mode == modeSeparate {
		separateTmplData := buildTemplateData(mail.Subject, mail.ReceivedAt, w.cfg.LinkExpiryHours, downloadURL, attachInfos)
		separateBody, err := renderTemplate(w.cfg.SeparateTemplate, defaultSeparateTemplate, separateTmplData)
		if err != nil {
			slog.Warn("通知メールテンプレートレンダリング失敗（続行）",
				"worker", workerName, "message_id", mail.MessageID, "error", err)
		} else if err := w.sendNotification(ctx, mail, separateBody); err != nil {
			slog.Warn("添付ファイル通知メール送信失敗（続行）",
				"worker", workerName, "message_id", mail.MessageID, "error", err)
		}
	}

	modified := *mail
	modified.RawEML = newEML
	modified.HasAttachment = len(env.Attachments)-len(toSeparate) > 0
	return &modified, nil
}

// saveAndRecord は各添付ファイルを MinIO に保存して DB に記録し、AttachmentInfo のリストを返す。
// downloadToken・downloadMode はメッセージ単位で呼び出し元が決定した値を受け取る。
// MinIO 保存後に DB 記録が失敗した場合、それまでに保存した全オブジェクトを削除して孤立を防ぐ。
func (w *Worker) saveAndRecord(ctx context.Context, mail *domain.Mail, parts []*enmime.Part, downloadToken string, downloadMode domain.DownloadMode) ([]AttachmentInfo, error) {
	infos := make([]AttachmentInfo, 0, len(parts))
	var savedPaths []string // 保存済みオブジェクトのパス（失敗時のロールバック用）

	for _, part := range parts {
		filename := sanitizeFilename(part.FileName)
		if filename == "" {
			filename = fmt.Sprintf("attachment-%d", time.Now().UnixNano())
		}
		ct := part.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}

		path, err := w.storage.SaveAttachment(ctx, mail.MessageID, filename, part.Content)
		if err != nil {
			w.cleanupAttachments(ctx, savedPaths)
			return nil, fmt.Errorf("添付ファイル保存失敗 (%s): %w", filename, err)
		}
		savedPaths = append(savedPaths, path)

		att := &domain.MailAttachment{
			ID:             uuid.New().String(),
			MessageID:      mail.MessageID,
			DownloadToken:  downloadToken,
			Filename:       filename,
			ContentType:    ct,
			SizeBytes:      int64(len(part.Content)),
			StorageBackend: domain.StorageBackendS3,
			StoragePath:    path,
			DownloadMode:   downloadMode,
		}
		if err := w.repo.SaveAttachment(ctx, att); err != nil {
			w.cleanupAttachments(ctx, savedPaths)
			return nil, fmt.Errorf("添付ファイルDB記録失敗 (%s): %w", filename, err)
		}

		infos = append(infos, AttachmentInfo{
			Name:   filename,
			SizeKB: float64(len(part.Content)) / 1024,
		})
	}
	return infos, nil
}

// sanitizeFilename はメール由来の添付ファイル名からパス区切り・制御文字を除去する。
// ストレージパスは "{message_id}/{filename}" 形式で構築されるため、
// ".." やパス区切りを含むファイル名によるパストラバーサル・キー汚染を防ぐ。
func sanitizeFilename(name string) string {
	// バックスラッシュは Linux の filepath.Base では区切りとして扱われないため先に置換する
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "." || name == ".." {
		return ""
	}
	return name
}

// cleanupAttachments は MinIO 保存済みオブジェクトを削除する。
// DB 記録失敗時のロールバックに使う。削除失敗はログに記録するのみ（ベストエフォート）。
func (w *Worker) cleanupAttachments(ctx context.Context, paths []string) {
	for _, path := range paths {
		if err := w.storage.DeleteAttachment(ctx, path); err != nil {
			slog.Warn("添付ファイルロールバック削除失敗（孤立オブジェクト）",
				"worker", workerName,
				"path", path,
				"error", err,
			)
		}
	}
}

// rebuildEML は分離対象を除いた新しい EML バイト列を構築する。
// 元ヘッダー（Received トレース・X-*・認証結果等）は eml.Rebuild が保持する。
func (w *Worker) rebuildEML(env *enmime.Envelope, separated map[*enmime.Part]bool, inlineText string, mail *domain.Mail) ([]byte, error) {
	// テキスト本文の先頭にテンプレートを挿入
	textBody := env.Text
	if w.cfg.Mode == modeInline {
		textBody = inlineText + "\n" + textBody
	}

	// HTML 本文の先頭にも挿入（<pre> タグで整形）
	htmlBody := env.HTML
	if w.cfg.Mode == modeInline && htmlBody != "" {
		escaped := strings.ReplaceAll(inlineText, "<", "&lt;")
		escaped = strings.ReplaceAll(escaped, ">", "&gt;")
		htmlBody = "<pre style=\"background:#f5f5f5;padding:10px;border-left:3px solid #ccc\">" +
			escaped + "</pre><br>" + htmlBody
	}

	return eml.Rebuild(env, eml.RebuildInput{
		From:     mail.FromAddress,
		To:       mail.ToAddresses,
		Subject:  mail.Subject,
		Date:     mail.ReceivedAt,
		Text:     textBody,
		HTML:     htmlBody,
		SkipPart: func(p *enmime.Part) bool { return separated[p] },
	})
}

// sendNotification は separate モード用の通知メールを reinject SMTP 経由で送信する。
// ctx を使って接続・送信をキャンセル可能にしている（stdlib smtp.SendMail はコンテキスト非対応）。
func (w *Worker) sendNotification(ctx context.Context, mail *domain.Mail, body string) error {
	notifSubject := "[添付ファイルのご案内] " + mail.Subject

	b := enmime.Builder().
		From("", w.cfg.SeparateFrom).
		Subject(notifSubject).
		Text([]byte(body)).
		Date(time.Now().UTC())

	for _, to := range mail.ToAddresses {
		b = b.To("", to)
	}

	root, err := b.Build()
	if err != nil {
		return fmt.Errorf("通知メール構築失敗: %w", err)
	}

	var buf bytes.Buffer
	if err := root.Encode(&buf); err != nil {
		return fmt.Errorf("通知メールエンコード失敗: %w", err)
	}

	return w.sendSeparateNotification(ctx, w.cfg.SeparateFrom, mail.ToAddresses, buf.Bytes())
}

// sendSeparateNotification は ctx-aware な SMTP 送信を行う。
// net.Dialer.DialContext でコンテキストを TCP 接続に伝播し、
// ctx にデッドラインがある場合はソケットにもセットする。
func (w *Worker) sendSeparateNotification(ctx context.Context, from string, to []string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", w.notifSMTPHost, w.notifSMTPPort)

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("通知SMTP接続失敗: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if setErr := conn.SetDeadline(deadline); setErr != nil {
			_ = conn.Close()
			return fmt.Errorf("通知SMTPデッドライン設定失敗: %w", setErr)
		}
	}

	host, _, _ := net.SplitHostPort(addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("通知SMTPクライアント作成失敗: %w", err)
	}
	defer c.Close()

	if err := c.Mail(from); err != nil {
		return fmt.Errorf("通知SMTP MAIL FROM失敗: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("通知SMTP RCPT TO失敗 (%s): %w", rcpt, err)
		}
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("通知SMTP DATA失敗: %w", err)
	}
	if _, err := wc.Write(msg); err != nil {
		_ = wc.Close()
		return fmt.Errorf("通知SMTPメッセージ書き込み失敗: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("通知SMTPデータクローズ失敗: %w", err)
	}
	return c.Quit()
}
