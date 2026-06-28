package urlrewrite

import (
	"context"
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

const proxyBase = "https://safelink.example.com/check?url="

func newWorker(proxyBase, encode string, html, text bool, skip []string) *Worker {
	return &Worker{
		proxyBaseURL: proxyBase,
		urlEncode:    encode,
		rewriteHTML:  html,
		rewriteText:  text,
		skipDomains:  skip,
	}
}

func buildTextEML(body string) []byte {
	return []byte("From: sender@example.com\r\nTo: recv@example.com\r\nSubject: Test\r\n\r\n" + body)
}

func buildHTMLEML(htmlBody string) []byte {
	return []byte(
		"From: sender@example.com\r\nTo: recv@example.com\r\nSubject: Test\r\n" +
			"Content-Type: text/html\r\n\r\n" + htmlBody,
	)
}

// 変換後 EML からテキスト本文を取り出す簡易ヘルパー。
func extractBody(eml []byte) string {
	parts := strings.SplitN(string(eml), "\r\n\r\n", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	parts = strings.SplitN(string(eml), "\n\n", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return string(eml)
}

// --- proxy_base_url 未設定 ---

func TestURLRewrite_NoProxy_NoChange(t *testing.T) {
	w := newWorker("", "base64", true, true, nil)
	m := &domain.Mail{
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recv@example.com"},
		RawEML:      buildTextEML("Click https://evil.com/path"),
	}
	result, err := w.Transform(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != m {
		t.Errorf("want same pointer (no-op), got different")
	}
}

// --- プレーンテキスト書き換え ---

func TestURLRewrite_PlainText_Base64(t *testing.T) {
	w := newWorker(proxyBase, "base64", false, true, nil)
	m := &domain.Mail{
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recv@example.com"},
		RawEML:      buildTextEML("Visit https://evil.com/page for details."),
	}
	result, err := w.Transform(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := extractBody(result.RawEML)
	if strings.Contains(body, "https://evil.com/page") {
		t.Errorf("original URL still present in body")
	}
	if !strings.Contains(body, proxyBase) {
		t.Errorf("proxy URL not found in body")
	}
}

func TestURLRewrite_PlainText_TrailingPunctuation(t *testing.T) {
	// base64 エンコードを使うことで「元 URL に . が含まれるか否か」を
	// base64 出力の違いとして明確に検証できる。
	// none エンコードではプロキシ URL の直後にドットが来るため判別不能。
	w := newWorker(proxyBase, "base64", false, true, nil)
	m := &domain.Mail{
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recv@example.com"},
		RawEML:      buildTextEML("See https://example.com/path. Next sentence."),
	}
	result, err := w.Transform(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := extractBody(result.RawEML)
	// 末尾の "." はURLに含まれず、後続の文が壊れていないこと
	if strings.Contains(body, proxyBase+"https://example.com/path.") {
		t.Errorf("trailing dot incorrectly included in rewritten URL")
	}
	if !strings.Contains(body, ". Next sentence.") {
		t.Errorf("sentence after URL was corrupted")
	}
}

func TestURLRewrite_PlainText_RawURL(t *testing.T) {
	w := newWorker(proxyBase, "rawurl", false, true, nil)
	m := &domain.Mail{
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recv@example.com"},
		RawEML:      buildTextEML("https://evil.com/path?a=1&b=2"),
	}
	result, err := w.Transform(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := extractBody(result.RawEML)
	if !strings.Contains(body, "https%3A%2F%2F") {
		t.Errorf("rawurl encoding not applied")
	}
}

// --- HTML 書き換え ---

func TestURLRewrite_HTML_HrefRewritten(t *testing.T) {
	w := newWorker(proxyBase, "none", true, false, nil)
	html := `<a href="https://evil.com/click">Click here</a>`
	m := &domain.Mail{
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recv@example.com"},
		RawEML:      buildHTMLEML(html),
	}
	result, err := w.Transform(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(result.RawEML), `href="https://evil.com/click"`) {
		t.Errorf("original href still present")
	}
	if !strings.Contains(string(result.RawEML), proxyBase+"https://evil.com/click") {
		t.Errorf("proxy href not found")
	}
}

func TestURLRewrite_HTML_SrcRewritten(t *testing.T) {
	w := newWorker(proxyBase, "none", true, false, nil)
	html := `<img src="https://tracker.example.com/pixel.gif">`
	m := &domain.Mail{
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recv@example.com"},
		RawEML:      buildHTMLEML(html),
	}
	result, err := w.Transform(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(result.RawEML), `src="https://tracker.example.com`) {
		t.Errorf("original src still present")
	}
}

// --- スキップドメイン ---

func TestURLRewrite_SkipDomain(t *testing.T) {
	// base64 を使うことで外部 URL が「元の URL 文字列として」body に残らないことを検証できる。
	// none エンコードではプロキシ URL 引数に元 URL が含まれるため判別不能。
	w := newWorker(proxyBase, "base64", false, true, []string{"internal.test"})
	m := &domain.Mail{
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recv@example.com"},
		RawEML:      buildTextEML("Internal: https://portal.internal.test/app External: https://evil.com/bad"),
	}
	result, err := w.Transform(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := extractBody(result.RawEML)
	if !strings.Contains(body, "https://portal.internal.test/app") {
		t.Errorf("internal URL was incorrectly rewritten")
	}
	if strings.Contains(body, "https://evil.com/bad") {
		t.Errorf("external URL was not rewritten")
	}
}

// --- URL がない場合は変更なし ---

func TestURLRewrite_NoURL_NoChange(t *testing.T) {
	w := newWorker(proxyBase, "base64", true, true, nil)
	original := buildTextEML("No links here, just plain text.")
	m := &domain.Mail{
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recv@example.com"},
		RawEML:      original,
	}
	result, err := w.Transform(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != m {
		t.Errorf("want same pointer (no-op), got different pointer")
	}
}

// --- 危険スキームの URL は書き換えない（B-24） ---

// javascript: スキームの href はプロキシ経由に書き換えられない
func TestURLRewrite_JavascriptScheme_NotRewritten(t *testing.T) {
	w := newWorker(proxyBase, "none", true, false, nil)
	html := `<a href="javascript:alert(1)">Click</a><a href="https://safe.example.com">Safe</a>`
	m := &domain.Mail{
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recv@example.com"},
		RawEML:      buildHTMLEML(html),
	}
	result, err := w.Transform(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := string(result.RawEML)
	// javascript: URL はそのまま残っていること（プロキシ経由に変換しない）
	if !strings.Contains(body, `href="javascript:alert(1)"`) {
		t.Errorf("javascript: href was unexpectedly rewritten\nbody: %s", body)
	}
	// http/https の URL は書き換えられていること
	if strings.Contains(body, `href="https://safe.example.com"`) {
		t.Errorf("https URL was not rewritten\nbody: %s", body)
	}
	if !strings.Contains(body, proxyBase) {
		t.Errorf("proxy URL not found for https link\nbody: %s", body)
	}
}

// data: スキームの src はプロキシ経由に書き換えられない
func TestURLRewrite_DataScheme_NotRewritten(t *testing.T) {
	w := newWorker(proxyBase, "none", true, false, nil)
	dataURI := `data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7`
	html := `<img src="` + dataURI + `">`
	m := &domain.Mail{
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recv@example.com"},
		RawEML:      buildHTMLEML(html),
	}
	result, err := w.Transform(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := string(result.RawEML)
	if !strings.Contains(body, dataURI) {
		t.Errorf("data: URI was unexpectedly rewritten\nbody: %s", body)
	}
}

// http/https URL はプロキシ経由に書き換えられること
func TestURLRewrite_HttpScheme_IsRewritten(t *testing.T) {
	w := newWorker(proxyBase, "none", true, true, nil)
	html := `<a href="http://example.com/page">HTTP link</a>`
	m := &domain.Mail{
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recv@example.com"},
		RawEML:      buildHTMLEML(html),
	}
	result, err := w.Transform(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := string(result.RawEML)
	if strings.Contains(body, `href="http://example.com/page"`) {
		t.Errorf("http URL was not rewritten\nbody: %s", body)
	}
	if !strings.Contains(body, proxyBase+"http://example.com/page") {
		t.Errorf("proxy URL not found\nbody: %s", body)
	}
}
