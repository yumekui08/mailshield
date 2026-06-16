// Package sanitize は HTML メール本文を無害化する変換ワーカーを実装する。
// bluemonday を使って XSS・悪意あるスクリプト・危険な属性を除去する。
package sanitize

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jhillyerd/enmime"
	"github.com/microcosm-cc/bluemonday"
	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

const workerName = "sanitize-worker"

type sanitizePolicy string

const (
	policyStrict   sanitizePolicy = "strict"   // テキストのみ残す
	policyStandard sanitizePolicy = "standard" // 安全なタグ・属性のみ許可
)

// Config は sanitize-worker の設定を保持する。
type Config struct {
	Policy sanitizePolicy `yaml:"policy"` // strict | standard
}

// Worker は HTML メール本文を無害化する変換ワーカーである。
type Worker struct {
	policy *bluemonday.Policy
}

// New は sanitize-worker を初期化する。
func New(workerConfigDir string) (*Worker, error) {
	cfg, err := loadConfig(workerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("sanitize-worker 設定ロード失敗: %w", err)
	}

	var p *bluemonday.Policy
	switch cfg.Policy {
	case policyStrict:
		p = bluemonday.StrictPolicy()
	default: // standard
		p = bluemonday.UGCPolicy()
	}

	return &Worker{policy: p}, nil
}

func (w *Worker) Name() string { return workerName }

// Transform は EML の HTML 本文を無害化して返す。
// HTML 本文がない場合は変換せずそのまま返す。
func (w *Worker) Transform(_ context.Context, mail *domain.Mail) (*domain.Mail, error) {
	env, err := enmime.ReadEnvelope(bytes.NewReader(mail.RawEML))
	if err != nil {
		return nil, fmt.Errorf("EML パース失敗: %w", err)
	}

	if env.HTML == "" {
		return mail, nil
	}

	sanitized := w.policy.Sanitize(env.HTML)
	if sanitized == env.HTML {
		return mail, nil // 変更なし
	}

	// HTML 本文を置き換えて EML を再構築
	b := enmime.Builder().
		From("", mail.FromAddress).
		Subject(mail.Subject).
		Date(mail.ReceivedAt)
	for _, to := range mail.ToAddresses {
		b = b.To("", to)
	}
	if env.Text != "" {
		b = b.Text([]byte(env.Text))
	}
	b = b.HTML([]byte(sanitized))

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

	modified := *mail
	modified.RawEML = buf.Bytes()
	return &modified, nil
}

func loadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, workerName+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{Policy: policyStandard}, nil
		}
		return nil, fmt.Errorf("設定ファイル読み込み失敗 (%s): %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("設定ファイルパース失敗 (%s): %w", path, err)
	}
	if cfg.Policy == "" {
		cfg.Policy = policyStandard
	}
	return &cfg, nil
}
