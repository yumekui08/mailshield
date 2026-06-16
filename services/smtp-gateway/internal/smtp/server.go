// Package smtp は go-smtp を使った SMTP サーバーを実装する。
// 信頼済みMTA（Postfix等）から port 10024 でメールを受け取り、
// パイプライン処理を起動する。
package smtp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"strings"
	"sync"
	"time"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/google/uuid"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// Handler はメール受信後の処理を担うインターフェース。
// smtp パッケージはこのインターフェースに依存し、具体的な処理を知らない。
type Handler interface {
	HandleMail(ctx context.Context, mail *domain.Mail) error
}

// Options は SMTP サーバーの設定を保持する。
// ゼロ値は安全なデフォルト値として扱われる。
type Options struct {
	Port                  int
	Hostname              string
	TrustedSources        []string
	MaxMessageSizeMB      int
	MaxRecipients         int
	ReadTimeoutSeconds    int
	WriteTimeoutSeconds   int
	HandlerTimeoutSeconds int
}

// Server は SMTP サーバーのラッパーである。
type Server struct {
	server  *gosmtp.Server
	backend *smtpBackend
}

// New は SMTP サーバーを初期化する。
func New(opts Options, handler Handler) *Server {
	maxSize := int64(opts.MaxMessageSizeMB) * 1024 * 1024

	if opts.Hostname == "" {
		opts.Hostname = "smtp-gateway"
	}
	if opts.MaxRecipients == 0 {
		opts.MaxRecipients = 100
	}
	if opts.ReadTimeoutSeconds == 0 {
		opts.ReadTimeoutSeconds = 30
	}
	if opts.WriteTimeoutSeconds == 0 {
		opts.WriteTimeoutSeconds = 30
	}
	if opts.HandlerTimeoutSeconds == 0 {
		opts.HandlerTimeoutSeconds = 30
	}

	backend := &smtpBackend{
		trustedSources: opts.TrustedSources,
		maxMsgSize:     maxSize,
		handler:        handler,
		handlerTimeout: time.Duration(opts.HandlerTimeoutSeconds) * time.Second,
	}

	s := gosmtp.NewServer(backend)
	s.Addr = fmt.Sprintf(":%d", opts.Port)
	s.Domain = opts.Hostname
	s.MaxMessageBytes = maxSize
	s.MaxRecipients = opts.MaxRecipients
	s.AllowInsecureAuth = true // 内部ネットワーク専用・TLS不要
	s.ReadTimeout = time.Duration(opts.ReadTimeoutSeconds) * time.Second
	s.WriteTimeout = time.Duration(opts.WriteTimeoutSeconds) * time.Second

	return &Server{server: s, backend: backend}
}

// ListenAndServe は SMTP サーバーを起動する（ブロッキング）。
func (s *Server) ListenAndServe() error {
	slog.Info("SMTPサーバー起動", "addr", s.server.Addr)
	return s.server.ListenAndServe()
}

// GracefulClose は新規接続の受付を停止し、進行中のセッションが完了するまで待機する。
// ctx がタイムアウトした場合はその時点で返り、残セッションは強制終了される。
func (s *Server) GracefulClose(ctx context.Context) error {
	s.server.Close()

	done := make(chan struct{})
	go func() {
		s.backend.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ────────────────────────────────────────────────────────────
// go-smtp バックエンド実装
// ────────────────────────────────────────────────────────────

type smtpBackend struct {
	trustedSources []string
	maxMsgSize     int64
	handler        Handler
	handlerTimeout time.Duration
	wg             sync.WaitGroup
}

// NewSession は接続ごとにセッションを作成する。
// 接続元IPがホワイトリストにない場合は拒否する。
func (b *smtpBackend) NewSession(c *gosmtp.Conn) (gosmtp.Session, error) {
	remoteAddr := c.Conn().RemoteAddr().String()
	remoteIP, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// IPv6 ベアアドレス等ポート分離できない形式はそのまま使う
		remoteIP = remoteAddr
		slog.Debug("リモートアドレスのポート分離失敗（アドレスをそのまま使用）", "remote_addr", remoteAddr)
	}

	if !b.isTrusted(remoteIP) {
		slog.Warn("信頼されていない接続元からの接続を拒否",
			"remote_addr", remoteAddr)
		return nil, &gosmtp.SMTPError{
			Code:         550,
			EnhancedCode: gosmtp.EnhancedCode{5, 7, 0},
			Message:      "Access denied",
		}
	}

	// セッション開始時にカウントアップ。Logout() でカウントダウンする。
	// GracefulClose は wg.Wait() でゼロになるまで待機する。
	b.wg.Add(1)
	return &smtpSession{backend: b}, nil
}

// isTrusted は接続元IPがホワイトリストに含まれるか確認する。
// ホスト名が指定されている場合はDNS解決して比較する。
func (b *smtpBackend) isTrusted(remoteIP string) bool {
	for _, trusted := range b.trustedSources {
		if trusted == remoteIP {
			return true
		}
		addrs, err := net.LookupHost(trusted)
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if addr == remoteIP {
				return true
			}
		}
	}
	return false
}

// ────────────────────────────────────────────────────────────
// セッション実装
// ────────────────────────────────────────────────────────────

type smtpSession struct {
	backend     *smtpBackend
	fromAddress string
	toAddresses []string
}

func (s *smtpSession) Mail(from string, opts *gosmtp.MailOptions) error {
	s.fromAddress = from
	return nil
}

func (s *smtpSession) Rcpt(to string, opts *gosmtp.RcptOptions) error {
	s.toAddresses = append(s.toAddresses, to)
	return nil
}

func (s *smtpSession) Data(r io.Reader) error {
	// maxMsgSize+1 バイトまで読む。
	// 読んだ結果が maxMsgSize を超えていれば元のメールはそれ以上の大きさ → 拒否。
	// maxMsgSize ちょうどのメールは許可される（設定値が上限として機能する）。
	rawEML, err := io.ReadAll(io.LimitReader(r, s.backend.maxMsgSize+1))
	if err != nil {
		return fmt.Errorf("EML 読み取り失敗: %w", err)
	}
	if int64(len(rawEML)) > s.backend.maxMsgSize {
		return &gosmtp.SMTPError{
			Code:         552,
			EnhancedCode: gosmtp.EnhancedCode{5, 3, 4},
			Message:      "Message size exceeds limit",
		}
	}

	mail := &domain.Mail{
		MessageID:   uuid.New().String(),
		RawEML:      rawEML,
		ReceivedAt:  time.Now().UTC(),
		FromAddress: s.fromAddress,
		ToAddresses: s.toAddresses,
		Subject:     extractSubject(rawEML),
		SizeBytes:   int64(len(rawEML)),
		AuthResults: extractAuthResults(rawEML),
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.backend.handlerTimeout)
	defer cancel()

	if err := s.backend.handler.HandleMail(ctx, mail); err != nil {
		slog.Error("メール処理失敗",
			"message_id", mail.MessageID,
			"from", mail.FromAddress,
			"error", err)
		// MinIO 保存失敗などの場合は 451 を返してPostfixにリトライさせる
		return &gosmtp.SMTPError{
			Code:         451,
			EnhancedCode: gosmtp.EnhancedCode{4, 3, 0},
			Message:      "Try again later",
		}
	}
	return nil
}

func (s *smtpSession) Reset() {
	s.fromAddress = ""
	s.toAddresses = nil
}

func (s *smtpSession) Logout() error {
	s.backend.wg.Done()
	return nil
}

// extractSubject は生のEMLバイト列から Subject ヘッダーを取り出す（簡易実装）。
// RFC 2822 の折り畳みヘッダー（先頭が空白の継続行）を結合する。
// 正式なパースは handler 内で go-message を使って行う。
func extractSubject(eml []byte) string {
	lines := strings.Split(string(eml), "\n")
	var parts []string
	inSubject := false

	for _, line := range lines {
		stripped := strings.TrimRight(line, "\r")
		if strings.TrimSpace(stripped) == "" {
			break
		}
		if inSubject {
			if len(stripped) > 0 && (stripped[0] == ' ' || stripped[0] == '\t') {
				// 折り畳みヘッダーの継続行
				parts = append(parts, strings.TrimSpace(stripped))
				continue
			}
			// 別のヘッダーが始まった
			break
		}
		if strings.HasPrefix(strings.ToLower(stripped), "subject:") {
			parts = append(parts, strings.TrimSpace(stripped[len("subject:"):]))
			inSubject = true
		}
	}
	return strings.Join(parts, " ")
}

// extractAuthResults は生のEMLから Authentication-Results ヘッダーを読み取り、
// SPF/DKIM/DMARC の検証結果を返す。ヘッダーがない場合はすべて "none"。
func extractAuthResults(rawEML []byte) domain.AuthResults {
	result := domain.DefaultAuthResults()

	msg, err := mail.ReadMessage(bytes.NewReader(rawEML))
	if err != nil {
		return result
	}

	for _, v := range msg.Header["Authentication-Results"] {
		parseAuthResultsValue(v, &result)
	}

	return result
}

// parseAuthResultsValue は1つの Authentication-Results ヘッダー値を解析する。
// 形式: "<authserv-id>; method=result [key=value ...]; ..."
func parseAuthResultsValue(value string, result *domain.AuthResults) {
	parts := strings.Split(value, ";")
	for i, part := range parts {
		if i == 0 {
			continue // 最初のフィールドは authserv-id のためスキップ
		}
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		switch {
		case strings.HasPrefix(lower, "spf="):
			result.SPF = parseMethodResult(part[4:])
		case strings.HasPrefix(lower, "dkim="):
			result.DKIM = parseMethodResult(part[5:])
		case strings.HasPrefix(lower, "dmarc="):
			result.DMARC = parseMethodResult(part[6:])
		}
	}
}

// parseMethodResult は "pass (reason)" や "fail key=value" から結果値を抽出する。
// pass → AuthPass、fail/hardfail/softfail/policy → AuthFail、その他 → AuthNone
func parseMethodResult(s string) domain.AuthResult {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return domain.AuthNone
	}
	switch strings.ToLower(fields[0]) {
	case "pass":
		return domain.AuthPass
	case "fail", "hardfail", "softfail", "policy":
		return domain.AuthFail
	default:
		return domain.AuthNone
	}
}
