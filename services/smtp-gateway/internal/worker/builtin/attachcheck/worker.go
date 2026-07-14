// Package attachcheck は添付ファイルの偽装を検査する inspect ワーカーを実装する。
// 多重拡張子・禁止拡張子・実体（magic bytes）と拡張子の不一致・Office マクロ・
// 暗号化（検査不能）添付をスコアリングする。ファイルの展開・実行は一切行わない。
package attachcheck

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
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/officefile"
)

const workerName = "attachment-inspector"

// ScoresConfig は各検知項目のスコアを保持する。
type ScoresConfig struct {
	MultipleExtension int `yaml:"multiple_extension"`
	BannedExtension   int `yaml:"banned_extension"`
	Executable        int `yaml:"executable"`
	ExtensionMismatch int `yaml:"extension_mismatch"`
	Macro             int `yaml:"macro"`
	Encrypted         int `yaml:"encrypted"`
	BannedInArchive   int `yaml:"banned_in_archive"`
}

// Config は attachment-inspector の設定を保持する。
type Config struct {
	// Threshold はこのスコア以上で detected=true にする閾値。
	Threshold int          `yaml:"threshold"`
	Scores    ScoresConfig `yaml:"scores"`
	// BannedExtensions は拒否する拡張子（先頭ドットなし・小文字）。
	BannedExtensions []string `yaml:"banned_extensions"`
	// ArchiveExtensions はアーカイブ内の危険拡張子検査を行う対象拡張子。
	ArchiveExtensions []string `yaml:"archive_extensions"`
}

// Worker は添付ファイル検査ワーカーである。
type Worker struct {
	threshold         int
	scores            ScoresConfig
	banned            map[string]bool
	archiveExtensions map[string]bool
}

// New は attachment-inspector を初期化する。
func New(workerConfigDir string) (*Worker, error) {
	cfg, err := loadConfig(workerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("attachment-inspector 設定ロード失敗: %w", err)
	}
	banned := make(map[string]bool, len(cfg.BannedExtensions))
	for _, e := range cfg.BannedExtensions {
		banned[normalizeExt(e)] = true
	}
	archive := make(map[string]bool, len(cfg.ArchiveExtensions))
	for _, e := range cfg.ArchiveExtensions {
		archive[normalizeExt(e)] = true
	}
	return &Worker{
		threshold:         cfg.Threshold,
		scores:            cfg.Scores,
		banned:            banned,
		archiveExtensions: archive,
	}, nil
}

func (w *Worker) Name() string { return workerName }

// Inspect は全添付ファイルを検査してスコアを返す。
func (w *Worker) Inspect(_ context.Context, m *domain.Mail) (*domain.InspectResult, error) {
	result := &domain.InspectResult{
		WorkerName: workerName,
		Details:    make(map[string]any),
	}

	env, err := enmime.ReadEnvelope(bytes.NewReader(m.RawEML))
	if err != nil {
		// パース不能は検査対象なしとして扱う（他ワーカー・ポリシーに委ねる）
		return result, nil
	}

	totalScore := 0
	var reasons []string
	var flagged []string // 問題を検知したファイル名

	for _, att := range env.Attachments {
		score, attReasons := w.inspectAttachment(att.FileName, att.Content)
		if score > 0 {
			totalScore += score
			reasons = append(reasons, attReasons...)
			flagged = append(flagged, att.FileName)
		}
	}

	if len(reasons) > 0 {
		result.Details["reasons"] = reasons
	}
	if len(flagged) > 0 {
		result.Details["files"] = flagged
	}
	if totalScore > 100 {
		totalScore = 100
	}
	result.Score = totalScore
	result.Detected = totalScore >= w.threshold
	return result, nil
}

// inspectAttachment は1つの添付ファイルを検査してスコアと理由を返す。
func (w *Worker) inspectAttachment(filename string, content []byte) (int, []string) {
	score := 0
	var reasons []string
	add := func(s int, reason string) {
		score += s
		reasons = append(reasons, reason+":"+filename)
	}

	lowerName := strings.ToLower(filename)
	ext := normalizeExt(filepath.Ext(lowerName))

	// 多重拡張子（例: invoice.pdf.exe）: 末尾から2つの拡張子を見て、
	// 末尾が実行系・スクリプト系ならより疑わしい
	if hasMultipleExtension(lowerName) {
		add(w.scores.MultipleExtension, "multiple_extension")
	}

	// 禁止拡張子
	if w.banned[ext] {
		add(w.scores.BannedExtension, "banned_extension")
	}

	// 実体判定（magic bytes）
	switch {
	case officefile.IsExecutable(content):
		// 拡張子が実行形式を偽装している（.pdf なのに中身が PE 等）
		add(w.scores.Executable, "executable")
		if ext != "exe" && ext != "dll" && ext != "com" && ext != "scr" {
			add(w.scores.ExtensionMismatch, "extension_mismatch")
		}
	case officefile.IsZip(content):
		w.inspectZip(content, add)
	case officefile.IsOLE(content):
		if officefile.OLEIsEncryptedOffice(content) {
			add(w.scores.Encrypted, "encrypted_office")
		} else if officefile.OLEHasMacro(content) {
			add(w.scores.Macro, "ole_macro")
		}
	}

	return score, reasons
}

// inspectZip は ZIP / OOXML の内部を検査する。
func (w *Worker) inspectZip(content []byte, add func(int, string)) {
	// 暗号化 ZIP（検査不能 = 受信 PPAP）
	if officefile.ZipHasEncryptedEntry(content) {
		add(w.scores.Encrypted, "encrypted_zip")
		return // 暗号化されていると中身を見られないのでここで打ち切り
	}
	// OOXML マクロ
	if officefile.OOXMLHasMacro(content) {
		add(w.scores.Macro, "ooxml_macro")
	}
	// アーカイブ内の禁止拡張子・実行形式
	for _, name := range officefile.ZipEntryNames(content) {
		inner := normalizeExt(filepath.Ext(strings.ToLower(name)))
		if w.banned[inner] {
			add(w.scores.BannedInArchive, "banned_in_archive")
			return // 1件見つければ十分
		}
	}
}

// hasMultipleExtension は「拡張子が2つ以上あり、末尾が危険拡張子」かを返す。
// 誤検知を抑えるため、単に2つの拡張子ではなく末尾が実行/スクリプト系のときのみ true。
func hasMultipleExtension(lowerName string) bool {
	base := filepath.Base(lowerName)
	parts := strings.Split(base, ".")
	if len(parts) < 3 { // name.ext1.ext2 で 3 要素以上
		return false
	}
	last := parts[len(parts)-1]
	dangerous := map[string]bool{
		"exe": true, "scr": true, "com": true, "bat": true, "cmd": true,
		"js": true, "vbs": true, "jar": true, "ps1": true, "lnk": true,
		"pif": true, "hta": true, "wsf": true,
	}
	// 末尾直前がよくある文書拡張子（偽装の典型: report.pdf.exe）
	secondLast := parts[len(parts)-2]
	docLike := map[string]bool{
		"pdf": true, "doc": true, "docx": true, "xls": true, "xlsx": true,
		"ppt": true, "pptx": true, "txt": true, "jpg": true, "png": true, "zip": true,
	}
	return dangerous[last] && docLike[secondLast]
}

func normalizeExt(ext string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
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
	def := defaultConfig()
	if cfg.Threshold == 0 {
		cfg.Threshold = def.Threshold
	}
	if cfg.Scores == (ScoresConfig{}) {
		cfg.Scores = def.Scores
	}
	if len(cfg.BannedExtensions) == 0 {
		cfg.BannedExtensions = def.BannedExtensions
	}
	if len(cfg.ArchiveExtensions) == 0 {
		cfg.ArchiveExtensions = def.ArchiveExtensions
	}
	return &cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Threshold: 50,
		Scores: ScoresConfig{
			MultipleExtension: 60,
			BannedExtension:   70,
			Executable:        80,
			ExtensionMismatch: 40,
			Macro:             50,
			Encrypted:         40,
			BannedInArchive:   60,
		},
		BannedExtensions: []string{
			"exe", "scr", "com", "bat", "cmd", "pif", "js", "vbs",
			"jar", "ps1", "lnk", "hta", "wsf", "msi", "dll",
		},
		ArchiveExtensions: []string{"zip"},
	}
}
