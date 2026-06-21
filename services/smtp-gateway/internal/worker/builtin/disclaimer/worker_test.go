package disclaimer

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jhillyerd/enmime"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

func makeTextMail(subject, body string) *domain.Mail {
	raw := "From: sender@example.com\r\nTo: receiver@example.com\r\nSubject: " + subject +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body
	return &domain.Mail{
		MessageID:   "test-id",
		RawEML:      []byte(raw),
		FromAddress: "sender@example.com",
		ToAddresses: []string{"receiver@example.com"},
		Subject:     subject,
		ReceivedAt:  time.Now(),
	}
}

func makeHTMLMail(subject, textBody, htmlBody string) *domain.Mail {
	raw := "From: sender@example.com\r\nTo: receiver@example.com\r\nSubject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"boundary\"\r\n\r\n" +
		"--boundary\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + textBody + "\r\n" +
		"--boundary\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + htmlBody + "\r\n" +
		"--boundary--\r\n"
	return &domain.Mail{
		MessageID:   "test-html-id",
		RawEML:      []byte(raw),
		FromAddress: "sender@example.com",
		ToAddresses: []string{"receiver@example.com"},
		Subject:     subject,
		ReceivedAt:  time.Now(),
	}
}

// parseResult はテスト用に結果 EML を enmime でパースして返す。
func parseResult(t *testing.T, rawEML []byte) *enmime.Envelope {
	t.Helper()
	env, err := enmime.ReadEnvelope(bytes.NewReader(rawEML))
	if err != nil {
		t.Fatalf("結果 EML のパース失敗: %v", err)
	}
	return env
}

func TestWorker_TextOnly(t *testing.T) {
	w := &Worker{cfg: &Config{
		TextFooter: "免責事項: このメールは機密情報を含む場合があります。",
		Marker:     "mailshield-disclaimer",
	}}

	mail := makeTextMail("Hello", "本文です。")
	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}

	env := parseResult(t, result.RawEML)
	if !strings.Contains(env.Text, "mailshield-disclaimer") {
		t.Error("マーカーがテキスト本文に含まれていない")
	}
	if !strings.Contains(env.Text, "免責事項") {
		t.Error("フッターがテキスト本文に含まれていない")
	}
	if !strings.Contains(env.Text, "本文です。") {
		t.Error("元の本文が失われている")
	}
}

func TestWorker_HTMLFooterInsertedBeforeBodyTag(t *testing.T) {
	w := &Worker{cfg: &Config{
		HTMLFooter: `<div class="disclaimer">免責事項</div>`,
		Marker:     "mailshield-disclaimer",
	}}

	htmlBody := "<html><body><p>HTML本文</p></body></html>"
	mail := makeHTMLMail("Test", "テキスト本文", htmlBody)
	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}

	env := parseResult(t, result.RawEML)
	if !strings.Contains(env.HTML, "免責事項") {
		t.Error("HTMLフッターが含まれていない")
	}
	// フッターが </body> より前にある
	footerIdx := strings.Index(env.HTML, "免責事項")
	bodyCloseIdx := strings.Index(strings.ToLower(env.HTML), "</body>")
	if bodyCloseIdx >= 0 && footerIdx > bodyCloseIdx {
		t.Error("フッターが </body> の後に挿入されている")
	}
	// 元の本文が保持されている
	if !strings.Contains(env.HTML, "HTML本文") {
		t.Error("元の HTML 本文が失われている")
	}
}

func TestWorker_NoDuplicateFooter(t *testing.T) {
	w := &Worker{cfg: &Config{
		TextFooter: "フッター",
		Marker:     "mailshield-disclaimer",
	}}

	// すでにマーカーが含まれているメール
	mail := makeTextMail("Test", "本文\r\n\r\nmailshield-disclaimer\r\nフッター")
	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}

	// 元のメールが変更されていないこと（= 同じ RawEML が返る）
	if string(result.RawEML) != string(mail.RawEML) {
		t.Error("重複フッターチェック: 元のメールと異なる内容が返された")
	}
}

func TestWorker_NoFooterConfigured(t *testing.T) {
	w := &Worker{cfg: &Config{Marker: "mailshield-disclaimer"}}

	mail := makeTextMail("Test", "本文")
	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	// フッターなし → 変更なし
	if string(result.RawEML) != string(mail.RawEML) {
		t.Error("フッター未設定時にメールが変更されている")
	}
}

func TestWorker_HTMLWithoutBodyTag(t *testing.T) {
	w := &Worker{cfg: &Config{
		HTMLFooter: `<p>フッター</p>`,
		Marker:     "mailshield-disclaimer",
	}}

	mail := makeHTMLMail("Test", "テキスト", "<p>本文</p>") // </body> タグなし
	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}

	env := parseResult(t, result.RawEML)
	if !strings.Contains(env.HTML, "フッター") {
		t.Error("</body>なしのHTML: フッターが末尾に追加されていない")
	}
}

func TestWorker_TextAndHTMLBoth(t *testing.T) {
	w := &Worker{cfg: &Config{
		TextFooter: "テキストフッター",
		HTMLFooter: `<p>HTMLフッター</p>`,
		Marker:     "mailshield-disclaimer",
	}}

	mail := makeHTMLMail("Test", "テキスト本文", "<html><body><p>HTML本文</p></body></html>")
	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}

	env := parseResult(t, result.RawEML)
	if !strings.Contains(env.Text, "テキストフッター") {
		t.Error("テキストフッターが含まれていない")
	}
	if !strings.Contains(env.HTML, "HTMLフッター") {
		t.Error("HTMLフッターが含まれていない")
	}
}
