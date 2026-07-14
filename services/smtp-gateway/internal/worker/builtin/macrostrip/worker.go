// Package macrostrip は Office 文書のマクロ（VBA）を除去する transform ワーカーを実装する。
// m-FILTER の「メール無害化（マクロ除去）」に相当する。
// 原本 EML は別途保存されているため、無害化前に戻すことができる。
package macrostrip

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/jhillyerd/enmime"
	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/eml"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/officefile"
)

const workerName = "macro-strip"

// Config は macro-strip の設定を保持する。
type Config struct {
	// StripOOXML は OOXML（.docm/.xlsm 等）から VBA パートを除去するか（デフォルト true）。
	StripOOXML *bool `yaml:"strip_ooxml"`
	// DropOLEMacro は OLE（旧 .doc/.xls）でマクロを含むものを添付ごと除去するか。
	// OLE の VBA を安全に部分除去する手段がないため、除去する場合は添付を丸ごと落とす（デフォルト false）。
	DropOLEMacro bool `yaml:"drop_ole_macro"`
}

// Worker はマクロ除去ワーカーである。
type Worker struct {
	stripOOXML   bool
	dropOLEMacro bool
}

// New は macro-strip を初期化する。
func New(workerConfigDir string) (*Worker, error) {
	cfg, err := loadConfig(workerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("macro-strip 設定ロード失敗: %w", err)
	}
	stripOOXML := true
	if cfg.StripOOXML != nil {
		stripOOXML = *cfg.StripOOXML
	}
	return &Worker{stripOOXML: stripOOXML, dropOLEMacro: cfg.DropOLEMacro}, nil
}

func (w *Worker) Name() string { return workerName }

// Transform は添付内の Office マクロを除去した EML を返す。
// マクロを含む添付が1つもない場合は元のメールをそのまま返す（EML 再構築しない）。
func (w *Worker) Transform(_ context.Context, mail *domain.Mail) (*domain.Mail, error) {
	env, err := enmime.ReadEnvelope(bytes.NewReader(mail.RawEML))
	if err != nil {
		// パース不能はこのワーカーでは何もしない（他の変換・ポリシーに委ねる）
		return mail, nil
	}

	stripped := make(map[*enmime.Part]bool) // 添付ごと除去する対象
	changed := false
	var actions []string

	for _, att := range env.Attachments {
		switch {
		case w.stripOOXML && officefile.IsZip(att.Content):
			if newContent, ok := officefile.StripOOXMLMacro(att.Content); ok {
				att.Content = newContent
				changed = true
				actions = append(actions, "ooxml_stripped:"+att.FileName)
			}
		case w.dropOLEMacro && officefile.IsOLE(att.Content) && officefile.OLEHasMacro(att.Content):
			stripped[att] = true
			changed = true
			actions = append(actions, "ole_dropped:"+att.FileName)
		}
	}

	if !changed {
		return mail, nil
	}

	newEML, err := eml.Rebuild(env, eml.RebuildInput{
		From:    mail.FromAddress,
		To:      mail.ToAddresses,
		Subject: mail.Subject,
		Date:    mail.ReceivedAt,
		Text:    env.Text,
		HTML:    env.HTML,
		SkipPart: func(p *enmime.Part) bool {
			return stripped[p]
		},
	})
	if err != nil {
		return nil, fmt.Errorf("macro-strip EML 再構築失敗: %w", err)
	}

	slog.Info("マクロ除去実行",
		"message_id", mail.MessageID,
		"actions", strings.Join(actions, ","))

	modified := *mail
	modified.RawEML = newEML
	modified.SizeBytes = int64(len(newEML))
	return &modified, nil
}

func loadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, workerName+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("設定ファイル読み込み失敗 (%s): %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("設定ファイルパース失敗 (%s): %w", path, err)
	}
	return &cfg, nil
}
