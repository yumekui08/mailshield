package sanitize_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jhillyerd/enmime"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/sanitize"
)

// buildEML はテスト用の EML バイト列を生成する。htmlBody が空の場合は text/plain のみ。
func buildEML(t *testing.T, subject, textBody, htmlBody string) []byte {
	t.Helper()
	b := enmime.Builder().
		From("Sender", "sender@example.com").
		To("Recipient", "recipient@example.com").
		Subject(subject).
		Date(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if textBody != "" {
		b = b.Text([]byte(textBody))
	}
	if htmlBody != "" {
		b = b.HTML([]byte(htmlBody))
	}
	root, err := b.Build()
	if err != nil {
		t.Fatalf("EML ビルド失敗: %v", err)
	}
	var buf bytes.Buffer
	if err := root.Encode(&buf); err != nil {
		t.Fatalf("EML エンコード失敗: %v", err)
	}
	return buf.Bytes()
}

// newMail は buildEML から domain.Mail を生成するヘルパー。
func newMail(t *testing.T, subject, textBody, htmlBody string) *domain.Mail {
	t.Helper()
	return &domain.Mail{
		MessageID:   "test-id",
		RawEML:      buildEML(t, subject, textBody, htmlBody),
		ReceivedAt:  time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recipient@example.com"},
		Subject:     subject,
	}
}

// workerWithPolicy は指定ポリシーの設定ファイルを一時ディレクトリに書いて Worker を返す。
func workerWithPolicy(t *testing.T, policy string) *sanitize.Worker {
	t.Helper()
	dir := t.TempDir()
	content := "policy: " + policy + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sanitize-worker.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("設定ファイル書き込み失敗: %v", err)
	}
	w, err := sanitize.New(dir)
	if err != nil {
		t.Fatalf("sanitize.New 失敗: %v", err)
	}
	return w
}

// parseHTML は EML バイト列から HTML 本文を取り出すヘルパー。
func parseHTML(t *testing.T, raw []byte) string {
	t.Helper()
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("EML パース失敗: %v", err)
	}
	return env.HTML
}

// ─── テストケース ──────────────────────────────────────────────────────────

// テキストのみのメールは変更されない
func TestTransform_PlainText_Unchanged(t *testing.T) {
	w := workerWithPolicy(t, "standard")
	mail := newMail(t, "test", "plain text only", "")
	original := mail.RawEML

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("Transform エラー: %v", err)
	}
	if !bytes.Equal(result.RawEML, original) {
		t.Error("HTML なしメールは変更されるべきでない")
	}
}

// 安全な HTML は変更されない
func TestTransform_SafeHTML_Unchanged(t *testing.T) {
	w := workerWithPolicy(t, "standard")
	safeHTML := `<p>Hello <b>World</b>. <a href="https://example.com">link</a></p>`
	mail := newMail(t, "safe", "Hello World", safeHTML)

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("Transform エラー: %v", err)
	}
	got := parseHTML(t, result.RawEML)
	if got == "" {
		t.Fatal("HTML 本文が消えた")
	}
	for _, want := range []string{"Hello", "World", "link"} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Errorf("安全なコンテンツ %q が除去されてしまった\ngot: %s", want, got)
		}
	}
}

// <script> タグが除去される
func TestTransform_ScriptTag_Removed(t *testing.T) {
	w := workerWithPolicy(t, "standard")
	html := `<p>Hello</p><script>alert('xss')</script>`
	mail := newMail(t, "script", "Hello", html)

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("Transform エラー: %v", err)
	}
	got := parseHTML(t, result.RawEML)
	if bytes.Contains([]byte(got), []byte("<script>")) {
		t.Errorf("<script> タグが残っている\ngot: %s", got)
	}
	if bytes.Contains([]byte(got), []byte("alert")) {
		t.Errorf("スクリプト本体が残っている\ngot: %s", got)
	}
}

// javascript: スキームが除去される
func TestTransform_JavascriptHref_Removed(t *testing.T) {
	w := workerWithPolicy(t, "standard")
	html := `<a href="javascript:alert(1)">click me</a>`
	mail := newMail(t, "jslink", "click me", html)

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("Transform エラー: %v", err)
	}
	got := parseHTML(t, result.RawEML)
	if bytes.Contains([]byte(got), []byte("javascript:")) {
		t.Errorf("javascript: href が残っている\ngot: %s", got)
	}
}

// onclick 等のイベントハンドラー属性が除去される
func TestTransform_EventHandler_Removed(t *testing.T) {
	w := workerWithPolicy(t, "standard")
	html := `<p onclick="alert(1)" onmouseover="steal()">text</p>`
	mail := newMail(t, "event", "text", html)

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("Transform エラー: %v", err)
	}
	got := parseHTML(t, result.RawEML)
	for _, attr := range []string{"onclick", "onmouseover"} {
		if bytes.Contains([]byte(got), []byte(attr)) {
			t.Errorf("イベントハンドラー %q が残っている\ngot: %s", attr, got)
		}
	}
	if !bytes.Contains([]byte(got), []byte("text")) {
		t.Error("テキストコンテンツが意図せず除去された")
	}
}

// <iframe> が除去される
func TestTransform_Iframe_Removed(t *testing.T) {
	w := workerWithPolicy(t, "standard")
	html := `<p>content</p><iframe src="https://evil.example.com"></iframe>`
	mail := newMail(t, "iframe", "content", html)

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("Transform エラー: %v", err)
	}
	got := parseHTML(t, result.RawEML)
	if bytes.Contains([]byte(got), []byte("iframe")) {
		t.Errorf("<iframe> が残っている\ngot: %s", got)
	}
}

// strict ポリシーは HTML タグをすべて除去する
func TestTransform_StrictPolicy_AllTagsRemoved(t *testing.T) {
	w := workerWithPolicy(t, "strict")
	html := `<p>Hello <b>World</b></p><script>bad()</script>`
	mail := newMail(t, "strict", "Hello World", html)

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("Transform エラー: %v", err)
	}
	got := parseHTML(t, result.RawEML)
	if bytes.Contains([]byte(got), []byte("<")) {
		t.Errorf("strict ポリシーで HTML タグが残っている\ngot: %s", got)
	}
	if !bytes.Contains([]byte(got), []byte("Hello")) {
		t.Errorf("テキストコンテンツが消えてしまった\ngot: %s", got)
	}
}

// 設定ファイルが存在しない場合は standard ポリシーで動作する
func TestNew_NoConfigFile_DefaultsToStandard(t *testing.T) {
	emptyDir := t.TempDir()
	w, err := sanitize.New(emptyDir)
	if err != nil {
		t.Fatalf("設定ファイルなしで New が失敗: %v", err)
	}
	html := `<p>Hello</p><script>bad()</script>`
	mail := newMail(t, "default", "Hello", html)

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("Transform エラー: %v", err)
	}
	got := parseHTML(t, result.RawEML)
	if bytes.Contains([]byte(got), []byte("<script>")) {
		t.Errorf("デフォルトポリシーで <script> が残っている\ngot: %s", got)
	}
}

// Worker.Name は "sanitize-worker" を返す
func TestName(t *testing.T) {
	w := workerWithPolicy(t, "standard")
	if got := w.Name(); got != "sanitize-worker" {
		t.Errorf("Name() = %q, want %q", got, "sanitize-worker")
	}
}

// buildEMLWithHeaders はカスタムヘッダーを含むテスト用 EML バイト列を生成する。
// enmime.Builder はカスタムヘッダーの追加 API を持たないため、
// Build 後に root.Header へ直接書き込む。
func buildEMLWithHeaders(t *testing.T, subject, htmlBody string, extraHeaders map[string]string) []byte {
	t.Helper()
	b := enmime.Builder().
		From("Sender", "sender@example.com").
		To("Recipient", "recipient@example.com").
		Subject(subject).
		Date(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if htmlBody != "" {
		b = b.HTML([]byte(htmlBody))
	}
	root, err := b.Build()
	if err != nil {
		t.Fatalf("EML ビルド失敗: %v", err)
	}
	for k, v := range extraHeaders {
		root.Header.Set(k, v)
	}
	var buf bytes.Buffer
	if err := root.Encode(&buf); err != nil {
		t.Fatalf("EML エンコード失敗: %v", err)
	}
	return buf.Bytes()
}

// parseHeader は EML バイト列から指定ヘッダー値を取り出すヘルパー。
func parseHeader(t *testing.T, raw []byte, name string) string {
	t.Helper()
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("EML パース失敗: %v", err)
	}
	return env.GetHeader(name)
}

// sanitize-worker は HTML 無害化後も Authentication-Results ヘッダーを保持すること（B-23）
func TestTransform_AuthenticationResultsHeaderPreserved(t *testing.T) {
	w := workerWithPolicy(t, "standard")

	authResults := "mx.example.com; dkim=pass; spf=pass; dmarc=pass"
	html := `<p>Hello</p><script>alert('xss')</script>`
	raw := buildEMLWithHeaders(t, "auth-test", html, map[string]string{
		"Authentication-Results": authResults,
		"X-Spam-Status":          "No, score=0.1",
		"DKIM-Signature":         "v=1; a=rsa-sha256; d=example.com; s=default",
	})
	mail := &domain.Mail{
		MessageID:   "test-auth",
		RawEML:      raw,
		ReceivedAt:  time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recipient@example.com"},
		Subject:     "auth-test",
	}

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatalf("Transform エラー: %v", err)
	}

	// HTML サニタイズが正常に行われたこと
	got := parseHTML(t, result.RawEML)
	if bytes.Contains([]byte(got), []byte("<script>")) {
		t.Errorf("<script> タグが残っている\ngot: %s", got)
	}

	// Authentication-Results ヘッダーが保持されていること
	gotAuth := parseHeader(t, result.RawEML, "Authentication-Results")
	if gotAuth == "" {
		t.Error("Authentication-Results ヘッダーが消えた")
	}
	if gotAuth != authResults {
		t.Errorf("Authentication-Results が変わった\ngot:  %s\nwant: %s", gotAuth, authResults)
	}

	// X-Spam-Status ヘッダーが保持されていること
	if v := parseHeader(t, result.RawEML, "X-Spam-Status"); v == "" {
		t.Error("X-Spam-Status ヘッダーが消えた")
	}

	// DKIM-Signature ヘッダーが保持されていること
	if v := parseHeader(t, result.RawEML, "DKIM-Signature"); v == "" {
		t.Error("DKIM-Signature ヘッダーが消えた")
	}
}
