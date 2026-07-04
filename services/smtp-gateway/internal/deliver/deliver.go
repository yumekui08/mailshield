// Package deliver は処理済みメールを配送先へ SMTP 送信する配送トランスポートを提供する。
//
// mailshield.yaml の deliverers セクションで名前付きの配送先（Postfix・SendGrid・
// Amazon SES 等の SMTP エンドポイント）を定義し、policy.yaml の destination で
// 名前を指定して使い分ける。STARTTLS・SMTP AUTH（PLAIN / LOGIN）に対応する。
package deliver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// TLSMode は SMTP 接続の TLS 方式。
type TLSMode string

const (
	// TLSNone は平文 SMTP（デフォルト。ローカル MTA への再インジェクト用）。
	TLSNone TLSMode = "none"
	// TLSStartTLS は STARTTLS で暗号化する（SendGrid / SES の :587 など）。
	TLSStartTLS TLSMode = "starttls"
	// TLSImplicit は接続時から TLS（SMTPS :465 など）。
	TLSImplicit TLSMode = "tls"
)

// SMTPDeliverer は単一の SMTP 配送先を表す。
type SMTPDeliverer struct {
	// name はログ出力用の識別子（deliverer 名または host:port）。
	name string
	host string
	port int
	tls  TLSMode
	// username / password が空でなければ SMTP AUTH を行う。
	username string
	password string
	// insecureSkipVerify は TLS 証明書検証をスキップする（開発・テスト用）。
	insecureSkipVerify bool
}

// Name は識別子を返す。
func (d *SMTPDeliverer) Name() string { return d.name }

// Addr は "host:port" 形式の接続先を返す。
func (d *SMTPDeliverer) Addr() string {
	return net.JoinHostPort(d.host, strconv.Itoa(d.port))
}

// Deliver はメールを配送先へ SMTP 送信する。
// ctx のデッドラインを TCP コネクションに伝播させることで、宛先がハングしても
// 呼び出し元の goroutine がブロックし続けることを防ぐ。
func (d *SMTPDeliverer) Deliver(ctx context.Context, mail *domain.Mail) error {
	addr := d.Addr()

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("SMTP 接続失敗 (deliverer=%s, addr=%s): %w", d.name, addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			_ = conn.Close()
			return fmt.Errorf("SMTP デッドライン設定失敗: %w", err)
		}
	}

	// SMTPS（implicit TLS）: 接続直後に TLS ハンドシェイクを行う
	if d.tls == TLSImplicit {
		tlsConn := tls.Client(conn, d.tlsConfig())
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return fmt.Errorf("TLS ハンドシェイク失敗 (deliverer=%s): %w", d.name, err)
		}
		conn = tlsConn
	}

	c, err := smtp.NewClient(conn, d.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("SMTP クライアント作成失敗 (deliverer=%s): %w", d.name, err)
	}
	defer c.Close()

	if d.tls == TLSStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("サーバーが STARTTLS に対応していません (deliverer=%s)", d.name)
		}
		if err := c.StartTLS(d.tlsConfig()); err != nil {
			return fmt.Errorf("STARTTLS 失敗 (deliverer=%s): %w", d.name, err)
		}
	}

	if d.username != "" {
		auth, err := d.selectAuth(c)
		if err != nil {
			return err
		}
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("SMTP AUTH 失敗 (deliverer=%s): %w", d.name, err)
		}
	}

	if err := c.Mail(mail.FromAddress); err != nil {
		return fmt.Errorf("MAIL FROM 失敗: %w", err)
	}
	for _, to := range mail.ToAddresses {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("RCPT TO 失敗 (%s): %w", to, err)
		}
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA コマンド失敗: %w", err)
	}
	if _, err := wc.Write(mail.RawEML); err != nil {
		return fmt.Errorf("メールデータ送信失敗: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("DATA 完了失敗: %w", err)
	}
	if err := c.Quit(); err != nil {
		// 一部のMTA（Mailpit等）はDATA完了後に即接続を閉じる。
		// QUITのEOFは配送成功後の接続切断であり、再配送の原因にならない。
		if !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "connection reset") {
			return fmt.Errorf("QUIT 失敗 (deliverer=%s, message_id=%s): %w",
				d.name, mail.MessageID, err)
		}
		slog.Debug("QUIT接続切断（配送済み・無視）", "deliverer", d.name, "error", err)
	}

	slog.Info("メール配送完了",
		"message_id", mail.MessageID,
		"deliverer", d.name,
		"destination", addr)
	return nil
}

func (d *SMTPDeliverer) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName:         d.host,
		InsecureSkipVerify: d.insecureSkipVerify, //nolint:gosec // 開発・テスト用の明示的なオプトイン
	}
}

// selectAuth はサーバーが広告する AUTH メカニズムから PLAIN / LOGIN を選択する。
// PLAIN を優先し、PLAIN 非対応なら LOGIN にフォールバックする
// （Amazon SES など LOGIN のみ広告するサーバーがある）。
func (d *SMTPDeliverer) selectAuth(c *smtp.Client) (smtp.Auth, error) {
	ok, mechs := c.Extension("AUTH")
	if !ok {
		return nil, fmt.Errorf("サーバーが SMTP AUTH に対応していません (deliverer=%s)", d.name)
	}
	if strings.Contains(mechs, "PLAIN") {
		return smtp.PlainAuth("", d.username, d.password, d.host), nil
	}
	if strings.Contains(mechs, "LOGIN") {
		return &loginAuth{username: d.username, password: d.password}, nil
	}
	return nil, fmt.Errorf("対応する AUTH メカニズムがありません (deliverer=%s, server=%s)", d.name, mechs)
}

// loginAuth は AUTH LOGIN メカニズムの実装。
// net/smtp は PLAIN と CRAM-MD5 しか提供しないため自前で実装する。
type loginAuth struct {
	username string
	password string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	// 平文接続で資格情報を送らない（net/smtp の PlainAuth と同じ方針）
	if !server.TLS && !isLocalhost(server.Name) {
		return "", nil, errors.New("LOGIN 認証は TLS 接続でのみ使用できます")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(fromServer))) {
	case "username:":
		return []byte(a.username), nil
	case "password:":
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("LOGIN 認証: 未知のサーバープロンプト %q", fromServer)
	}
}

func isLocalhost(name string) bool {
	return name == "localhost" || name == "127.0.0.1" || name == "::1"
}
