// Package notify は smtp-gateway が送信するシステムメール（隔離通知等）を担う。
package notify

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// QuarantineNotifier は隔離即時通知メールを送信する。
type QuarantineNotifier struct {
	smtpHost    string
	smtpPort    int
	fromAddress string
	uiBaseURL   string
}

// New は QuarantineNotifier を返す。
func New(smtpHost string, smtpPort int, fromAddress, uiBaseURL string) *QuarantineNotifier {
	return &QuarantineNotifier{
		smtpHost:    smtpHost,
		smtpPort:    smtpPort,
		fromAddress: fromAddress,
		uiBaseURL:   strings.TrimRight(uiBaseURL, "/"),
	}
}

// SendAsync は隔離通知メールを各受信者へ非同期で送信する。
// 送信失敗は WARN ログのみで配送フローに影響しない。
func (n *QuarantineNotifier) SendAsync(messageID, subject, fromAddr string, toAddresses []string) {
	go func() {
		for _, to := range toAddresses {
			if err := n.send(to, subject, fromAddr, messageID); err != nil {
				slog.Warn("隔離通知送信失敗",
					"message_id", messageID,
					"to", to,
					"error", err,
				)
			} else {
				slog.Info("隔離通知送信完了",
					"message_id", messageID,
					"to", to,
				)
			}
		}
	}()
}

func (n *QuarantineNotifier) send(to, originalSubject, originalFrom, messageID string) error {
	quarantineURL := fmt.Sprintf("%s/quarantine", n.uiBaseURL)

	bodyLines := []string{
		fmt.Sprintf("%s 様", to),
		"",
		"以下のメールが MailShield によって隔離されました。",
		"",
		fmt.Sprintf("  送信者 : %s", originalFrom),
		fmt.Sprintf("  件名   : %s", originalSubject),
		"",
		"このメールが正当なものであれば、MailShield にログインして解放してください。",
		"",
		quarantineURL,
		"",
		"身に覚えのない場合は、そのまま無視してください。",
		"※このメールへの返信はできません。",
	}
	body := strings.Join(bodyLines, "\r\n")

	notifSubject := fmt.Sprintf("[MailShield] メールが隔離されました: %s", originalSubject)
	encodedSubject := "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(notifSubject)) + "?="
	encodedBody := base64.StdEncoding.EncodeToString([]byte(body))

	var msg bytes.Buffer
	fmt.Fprintf(&msg, "From: %s\r\n", n.fromAddress)
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

	addr := fmt.Sprintf("%s:%d", n.smtpHost, n.smtpPort)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("SMTP 接続失敗: %w", err)
	}
	// 接続全体のデッドライン: 30秒以内に全SMTP操作を完了する。
	// これにより、リレーが途中で応答しなくなった場合でもゴルーチンが無期限にブロックされない。
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		conn.Close()
		return fmt.Errorf("SMTP デッドライン設定失敗: %w", err)
	}
	c, err := smtp.NewClient(conn, n.smtpHost)
	if err != nil {
		return fmt.Errorf("SMTP クライアント作成失敗: %w", err)
	}
	defer c.Close()

	if err := c.Mail(n.fromAddress); err != nil {
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
