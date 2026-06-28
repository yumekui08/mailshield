// Package urlrewrite はメール本文内の URL をプロキシ経由の URL に書き換える
// transform ワーカーを実装する。
// HTML の href/src 属性とプレーンテキスト本文の両方を対象とする。
// proxy_base_url が未設定の場合はメールを変更せずそのまま返す。
package urlrewrite

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jhillyerd/enmime"
	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

const workerName = "url-rewrite-worker"

// Config は url-rewrite-worker の設定を保持する。
type Config struct {
	// ProxyBaseURL は書き換え後 URL のベース。末尾に元 URL（エンコード済み）を付加する。
	// 例: "https://safelink.example.com/check?url="
	ProxyBaseURL string `yaml:"proxy_base_url"`
	// URLEncode は元 URL のエンコード方式（base64 / rawurl / none）。
	URLEncode   string   `yaml:"url_encode"`
	RewriteHTML bool     `yaml:"rewrite_html"`
	RewriteText bool     `yaml:"rewrite_text"`
	SkipDomains []string `yaml:"skip_domains"`
}

// Worker は URL 書き換え変換ワーカーである。
type Worker struct {
	proxyBaseURL string
	urlEncode    string
	rewriteHTML  bool
	rewriteText  bool
	skipDomains  []string // 小文字に正規化済み
}

// urlInTextPattern はプレーンテキスト中の URL を検出する。
var urlInTextPattern = regexp.MustCompile(`https?://\S+`)

// htmlAttrPattern は HTML の href/src 属性内の URL を検出する。
// スキームを問わずすべての属性値を対象とし、isRewritableURL で http/https のみを書き換える。
var htmlAttrPattern = regexp.MustCompile(`(?i)(href|src)="([^"]*)"`)

// New は url-rewrite-worker を初期化する。
func New(workerConfigDir string) (*Worker, error) {
	cfg, err := loadConfig(workerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("url-rewrite-worker 設定ロード失敗: %w", err)
	}

	skipDomains := make([]string, len(cfg.SkipDomains))
	for i, d := range cfg.SkipDomains {
		skipDomains[i] = strings.ToLower(d)
	}

	return &Worker{
		proxyBaseURL: cfg.ProxyBaseURL,
		urlEncode:    cfg.URLEncode,
		rewriteHTML:  cfg.RewriteHTML,
		rewriteText:  cfg.RewriteText,
		skipDomains:  skipDomains,
	}, nil
}

func (w *Worker) Name() string { return workerName }

// Transform はメール本文内の URL をプロキシ URL に書き換えて返す。
func (w *Worker) Transform(_ context.Context, m *domain.Mail) (*domain.Mail, error) {
	if w.proxyBaseURL == "" {
		return m, nil
	}

	env, err := enmime.ReadEnvelope(bytes.NewReader(m.RawEML))
	if err != nil {
		return nil, fmt.Errorf("EML パース失敗: %w", err)
	}

	newText := env.Text
	newHTML := env.HTML
	changed := false

	if w.rewriteText && env.Text != "" {
		if rewritten := w.rewriteText_(env.Text); rewritten != env.Text {
			newText = rewritten
			changed = true
		}
	}
	if w.rewriteHTML && env.HTML != "" {
		if rewritten := w.rewriteHTML_(env.HTML); rewritten != env.HTML {
			newHTML = rewritten
			changed = true
		}
	}

	if !changed {
		return m, nil
	}

	b := enmime.Builder().
		From("", m.FromAddress).
		Subject(m.Subject).
		Date(m.ReceivedAt)
	for _, to := range m.ToAddresses {
		b = b.To("", to)
	}
	if newText != "" {
		b = b.Text([]byte(newText))
	}
	if newHTML != "" {
		b = b.HTML([]byte(newHTML))
	}
	for _, att := range env.Attachments {
		b = b.AddAttachment(att.Content, att.ContentType, att.FileName)
	}
	for _, inline := range env.Inlines {
		b = b.AddInline(inline.Content, inline.ContentType, inline.FileName, inline.ContentID)
	}

	root, err := b.Build()
	if err != nil {
		return nil, fmt.Errorf("EML 再構築失敗: %w", err)
	}
	for _, h := range []string{"Message-ID", "CC", "Reply-To", "In-Reply-To", "References"} {
		if v := env.GetHeader(h); v != "" {
			root.Header.Set(h, v)
		}
	}

	var buf bytes.Buffer
	if err := root.Encode(&buf); err != nil {
		return nil, fmt.Errorf("EML エンコード失敗: %w", err)
	}

	modified := *m
	modified.RawEML = buf.Bytes()
	return &modified, nil
}

// isRewritableURL は URL のスキームが http または https であることを確認する。
// javascript:, data:, mailto:, ftp: 等の危険または不要なスキームを持つ URL は
// プロキシ経由の書き換えを行わない。
func isRewritableURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return scheme == "http" || scheme == "https"
}

// rewriteText_ はプレーンテキスト本文の URL を書き換える。
func (w *Worker) rewriteText_(text string) string {
	return urlInTextPattern.ReplaceAllStringFunc(text, func(match string) string {
		rawURL := strings.TrimRight(match, ".,;:!?\"')")
		if !isRewritableURL(rawURL) {
			return match
		}
		if w.shouldSkip(rawURL) {
			return match
		}
		rewritten := w.proxyBaseURL + w.encodeURL(rawURL)
		// TrimRight で削った末尾文字があれば元に戻す
		return rewritten + match[len(rawURL):]
	})
}

// rewriteHTML_ は HTML 本文の href/src 属性内の URL を書き換える。
// javascript:, data: 等の危険なスキームはプロキシ経由に書き換えない。
func (w *Worker) rewriteHTML_(html string) string {
	return htmlAttrPattern.ReplaceAllStringFunc(html, func(match string) string {
		parts := htmlAttrPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		attr := parts[1]
		rawURL := parts[2]
		if !isRewritableURL(rawURL) {
			return match
		}
		if w.shouldSkip(rawURL) {
			return match
		}
		return attr + `="` + w.proxyBaseURL + w.encodeURL(rawURL) + `"`
	})
}

func (w *Worker) encodeURL(rawURL string) string {
	switch w.urlEncode {
	case "base64":
		return base64.StdEncoding.EncodeToString([]byte(rawURL))
	case "rawurl":
		return url.QueryEscape(rawURL)
	default: // none
		return rawURL
	}
}

func (w *Worker) shouldSkip(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range w.skipDomains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func loadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, workerName+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultConfig(), nil
		}
		return nil, fmt.Errorf("設定ファイル読み込み失敗 (%s): %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("設定ファイルパース失敗 (%s): %w", path, err)
	}
	if cfg.URLEncode == "" {
		cfg.URLEncode = "base64"
	}
	return &cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		ProxyBaseURL: "",
		URLEncode:    "base64",
		RewriteHTML:  true,
		RewriteText:  true,
		SkipDomains:  []string{"localhost", "127.0.0.1"},
	}
}
