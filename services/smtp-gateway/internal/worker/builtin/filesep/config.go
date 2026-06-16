package filesep

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const workerName = "filesep-worker"

type mode string

const (
	modeInline   mode = "inline"
	modeSeparate mode = "separate"
)

// Config は filesep-worker の設定を保持する。
type Config struct {
	Mode             mode     `yaml:"mode"`
	InlineTemplate   string   `yaml:"inline_template"`
	SeparateTemplate string   `yaml:"separate_template"`
	LinkExpiryHours  int      `yaml:"link_expiry_hours"`
	MinSizeBytes     int64    `yaml:"min_size_bytes"`
	Extensions       []string `yaml:"extensions"`    // 空 = すべて対象
	SeparateFrom     string   `yaml:"separate_from"` // separate モード時の送信元アドレス
	FrontendURL      string   `yaml:"frontend_url"`  // ダウンロードリンクのベース URL（例: https://mail.example.com）
}

func loadConfig(workerConfigDir string) (*Config, error) {
	path := filepath.Join(workerConfigDir, workerName+".yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultConfig(), nil
		}
		return nil, fmt.Errorf("filesep設定ファイル読み込み失敗 (%s): %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("filesep設定ファイルパース失敗 (%s): %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("filesep設定が不正 (%s): %w", path, err)
	}
	return &cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Mode:            modeInline,
		LinkExpiryHours: 72,
		MinSizeBytes:    0,
	}
}

func (c *Config) validate() error {
	if c.Mode == "" {
		c.Mode = modeInline
	}
	if c.Mode != modeInline && c.Mode != modeSeparate {
		return fmt.Errorf("mode は 'inline' または 'separate' のみ有効です: %q", c.Mode)
	}
	if c.LinkExpiryHours <= 0 {
		c.LinkExpiryHours = 72
	}
	if c.Mode == modeSeparate && c.SeparateFrom == "" {
		return errors.New("separate モードには separate_from が必須です")
	}
	// 拡張子を小文字・ドット付きに正規化
	for i, ext := range c.Extensions {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		c.Extensions[i] = ext
	}
	return nil
}

// shouldSeparate はファイル名・サイズがフィルタ条件に一致するか判定する。
func (c *Config) shouldSeparate(filename string, sizeBytes int64) bool {
	if sizeBytes < c.MinSizeBytes {
		return false
	}
	if len(c.Extensions) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(filename))
	for _, allowed := range c.Extensions {
		if allowed == ext {
			return true
		}
	}
	return false
}
