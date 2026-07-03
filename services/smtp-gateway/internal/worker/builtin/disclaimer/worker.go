// Package disclaimer はメールのテキスト部・HTML 部にフッターを追加する変換ワーカーを実装する。
// 送信メールへの法的免責事項・企業署名の自動付与に使用する。
package disclaimer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jhillyerd/enmime"
	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/eml"
)

const workerName = "disclaimer-worker"

// Config はdisclaimer-workerの設定を保持する。
type Config struct {
	// TextFooter はテキスト本文に追加するフッター文字列（省略可）。
	TextFooter string `yaml:"text_footer"`
	// HTMLFooter はHTML本文の </body> 直前に挿入するHTMLフラグメント（省略可）。
	HTMLFooter string `yaml:"html_footer"`
	// Marker は重複フッター防止のための識別マーカー。
	// この文字列がすでに本文中に存在する場合はフッターを追加しない。
	Marker string `yaml:"marker"`
}

// Worker はメールにフッターを追加する変換ワーカーである。
type Worker struct {
	cfg *Config
}

// New はdisclaimer-workerを初期化する。
// workerConfigDir/disclaimer-worker.yaml が存在しない場合はフッターなしで動作する。
func New(workerConfigDir string) (*Worker, error) {
	cfg, err := loadConfig(workerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("disclaimer-worker 設定ロード失敗: %w", err)
	}
	return &Worker{cfg: cfg}, nil
}

func (w *Worker) Name() string { return workerName }

// Transform はメールのテキスト部・HTML 部にフッターを追加して返す。
// どちらの部分も空または設定されていない場合は変換せずに返す。
// 重複チェック: Marker が既に本文中に存在する場合はスキップする。
func (w *Worker) Transform(_ context.Context, mail *domain.Mail) (*domain.Mail, error) {
	if w.cfg.TextFooter == "" && w.cfg.HTMLFooter == "" {
		return mail, nil
	}

	env, err := enmime.ReadEnvelope(bytes.NewReader(mail.RawEML))
	if err != nil {
		return nil, fmt.Errorf("EML パース失敗: %w", err)
	}

	// 重複チェック
	if strings.Contains(env.Text, w.cfg.Marker) || strings.Contains(env.HTML, w.cfg.Marker) {
		return mail, nil
	}

	newText := env.Text
	if newText != "" && w.cfg.TextFooter != "" {
		newText = newText + "\r\n\r\n" + w.cfg.Marker + "\r\n" + w.cfg.TextFooter
	}

	newHTML := env.HTML
	if newHTML != "" && w.cfg.HTMLFooter != "" {
		htmlMarker := "<!-- " + w.cfg.Marker + " -->"
		footerBlock := htmlMarker + "\n" + w.cfg.HTMLFooter
		lower := strings.ToLower(newHTML)
		if idx := strings.LastIndex(lower, "</body>"); idx >= 0 {
			newHTML = newHTML[:idx] + footerBlock + newHTML[idx:]
		} else {
			newHTML = newHTML + footerBlock
		}
	}

	if newText == env.Text && newHTML == env.HTML {
		return mail, nil
	}

	// EML 再構築（元ヘッダーは eml.Rebuild が保持する）
	newEML, err := eml.Rebuild(env, eml.RebuildInput{
		From:    mail.FromAddress,
		To:      mail.ToAddresses,
		Subject: mail.Subject,
		Date:    mail.ReceivedAt,
		Text:    newText,
		HTML:    newHTML,
	})
	if err != nil {
		return nil, err
	}

	modified := *mail
	modified.RawEML = newEML
	return &modified, nil
}

func loadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, workerName+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{Marker: "mailshield-disclaimer"}, nil
		}
		return nil, fmt.Errorf("設定ファイル読み込み失敗 (%s): %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("設定ファイルパース失敗 (%s): %w", path, err)
	}
	if cfg.Marker == "" {
		cfg.Marker = "mailshield-disclaimer"
	}
	return &cfg, nil
}
