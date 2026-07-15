// Package reinject はメールを下流 MTA へ SMTP 再インジェクトする共通処理を提供する。
// 承認フローの解放・遅延送信の解放など、保留していたメールを配送する場面で共用する。
package reinject

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

// Reinjector は保留メールを下流 MTA へ再インジェクトする。
type Reinjector struct {
	host string
	port int
}

// New は Reinjector を返す。host:port は content_filter をバイパスする再インジェクト先。
func New(host string, port int) *Reinjector {
	return &Reinjector{host: host, port: port}
}

// Send は EML を MAIL FROM / RCPT TO 付きで下流 MTA へ送信する。
// ctx のデッドラインを SMTP 操作全体に適用する。
func (r *Reinjector) Send(ctx context.Context, from string, to []string, eml []byte) error {
	addr := fmt.Sprintf("%s:%d", r.host, r.port)
	conn, err := (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("SMTP 接続失敗 (addr=%s): %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	c, err := smtp.NewClient(conn, r.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("SMTP クライアント作成失敗: %w", err)
	}
	defer c.Close()

	if err := c.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM 失敗: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO 失敗 (%s): %w", rcpt, err)
		}
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA 失敗: %w", err)
	}
	if _, err := wc.Write(eml); err != nil {
		return fmt.Errorf("メール送信失敗: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("DATA 完了失敗: %w", err)
	}
	return c.Quit()
}
