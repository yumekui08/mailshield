// Package tika は Apache Tika を使った DLP（情報漏洩防止）検査ワーカーを実装する。
// Tika の REST API で添付ファイルのテキストを抽出し、設定したパターンに一致するか検査する。
package tika

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/jhillyerd/enmime"
	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

const workerName = "dlp-worker"

// PatternConfig は DLP 検査パターンの設定を保持する。
type PatternConfig struct {
	Name  string `yaml:"name"`
	Regex string `yaml:"regex"`
	Score int    `yaml:"score"` // このパターンが検知されたときのスコア加算
}

// Config は dlp-worker の設定を保持する。
type Config struct {
	TikaURL        string `yaml:"tika_url"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	// MaxResponseBytes は Tika からのレスポンス読み取りサイズ上限（OOM防止）。
	MaxResponseBytes int `yaml:"max_response_bytes"`
	// DefaultPatternScore はパターンの score が 0 以下のとき適用されるデフォルトスコア。
	DefaultPatternScore int             `yaml:"default_pattern_score"`
	Patterns            []PatternConfig `yaml:"patterns"`
}

type compiledPattern struct {
	name  string
	re    *regexp.Regexp
	score int
}

// Worker は Tika を使った DLP 検査ワーカーである。
type Worker struct {
	tikaURL          string
	timeout          time.Duration
	maxResponseBytes int
	patterns         []compiledPattern
	client           *http.Client
}

// New は dlp-worker を初期化する。
func New(workerConfigDir string) (*Worker, error) {
	cfg, err := loadConfig(workerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("dlp-worker 設定ロード失敗: %w", err)
	}

	patterns, err := compilePatterns(cfg.Patterns, cfg.DefaultPatternScore)
	if err != nil {
		return nil, fmt.Errorf("dlp-worker パターンコンパイル失敗: %w", err)
	}

	return &Worker{
		tikaURL:          cfg.TikaURL,
		timeout:          time.Duration(cfg.TimeoutSeconds) * time.Second,
		maxResponseBytes: cfg.MaxResponseBytes,
		patterns:         patterns,
		client:           &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second},
	}, nil
}

func (w *Worker) Name() string { return workerName }

// Inspect は EML のボディテキストと添付ファイルを DLP パターンで検査する。
// ボディテキスト（plain/HTML）は直接 regex で検査する（Tika 不要）。
// 添付ファイルは Tika でテキストを抽出してから regex で検査する。
func (w *Worker) Inspect(ctx context.Context, mail *domain.Mail) (*domain.InspectResult, error) {
	env, err := enmime.ReadEnvelope(bytes.NewReader(mail.RawEML))
	if err != nil {
		return nil, fmt.Errorf("EML パース失敗: %w", err)
	}

	result := &domain.InspectResult{
		WorkerName: workerName,
		Details:    make(map[string]any),
	}

	scoreMap := make(map[string]int) // pattern name → 最大スコア（重複加算防止）

	scanText := func(text string) {
		for _, p := range w.patterns {
			if p.re.MatchString(text) {
				if p.score > scoreMap[p.name] {
					scoreMap[p.name] = p.score
				}
			}
		}
	}

	// ボディテキストを直接検査（Tika 不要）
	if env.Text != "" {
		scanText(env.Text)
	}
	if env.HTML != "" {
		scanText(env.HTML)
	}

	// 添付ファイルは Tika でテキスト抽出してから検査
	for _, att := range env.Attachments {
		text, err := w.extractText(ctx, att.Content, att.ContentType)
		if err != nil {
			continue
		}
		scanText(text)
	}

	totalScore := 0
	var hitPatterns []string
	for name, score := range scoreMap {
		hitPatterns = append(hitPatterns, name)
		totalScore += score
	}

	if len(hitPatterns) > 0 {
		result.Detected = true
		if totalScore > 100 {
			totalScore = 100
		}
		result.Score = totalScore
		result.Details["matched_patterns"] = hitPatterns
	}

	return result, nil
}

// extractText は Tika REST API でバイナリデータからテキストを抽出する。
func (w *Worker) extractText(ctx context.Context, data []byte, contentType string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		w.tikaURL+"/tika", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("Tika リクエスト生成失敗: %w", err)
	}
	req.Header.Set("Accept", "text/plain")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Tika リクエスト失敗: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Tika 非200レスポンス: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(w.maxResponseBytes)))
	if err != nil {
		return "", fmt.Errorf("Tika レスポンス読み取り失敗: %w", err)
	}
	return string(body), nil
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
	if cfg.TikaURL == "" {
		cfg.TikaURL = "http://tika:9998"
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = 30
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 10 * 1024 * 1024
	}
	if cfg.DefaultPatternScore == 0 {
		cfg.DefaultPatternScore = 50
	}
	return &cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		TikaURL:             "http://tika:9998",
		TimeoutSeconds:      30,
		MaxResponseBytes:    10 * 1024 * 1024,
		DefaultPatternScore: 50,
		Patterns:            []PatternConfig{},
	}
}

func compilePatterns(cfgs []PatternConfig, defaultScore int) ([]compiledPattern, error) {
	if defaultScore <= 0 {
		defaultScore = 50
	}
	compiled := make([]compiledPattern, 0, len(cfgs))
	for _, c := range cfgs {
		re, err := regexp.Compile(c.Regex)
		if err != nil {
			return nil, fmt.Errorf("パターン %q のコンパイル失敗: %w", c.Name, err)
		}
		score := c.Score
		if score <= 0 {
			score = defaultScore
		}
		compiled = append(compiled, compiledPattern{name: c.Name, re: re, score: score})
	}
	return compiled, nil
}
