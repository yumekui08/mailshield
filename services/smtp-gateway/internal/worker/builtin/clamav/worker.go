// Package clamav は ClamAV clamd を使ったウイルス検査ワーカーを実装する。
package clamav

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

const workerName = "av-worker"

// Config は av-worker の設定を保持する。
type Config struct {
	Host                           string `yaml:"host"`
	Port                           int    `yaml:"port"`
	TimeoutSeconds                 int    `yaml:"timeout_seconds"`
	// ChunkDeadlineExtensionSeconds はチャンク転送ごとのローリングデッドライン延長幅（秒）。
	ChunkDeadlineExtensionSeconds  int    `yaml:"chunk_deadline_extension_seconds"`
}

// Worker は ClamAV を使った検査ワーカーである。
type Worker struct {
	addr                 string
	timeout              time.Duration
	chunkDeadlineExt     time.Duration
}

// New は av-worker を初期化する。
// workerConfigDir から av-worker.yaml を読み込む。
func New(workerConfigDir string) (*Worker, error) {
	cfg, err := loadConfig(workerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("av-worker 設定ロード失敗: %w", err)
	}
	return &Worker{
		addr:             fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		timeout:          time.Duration(cfg.TimeoutSeconds) * time.Second,
		chunkDeadlineExt: time.Duration(cfg.ChunkDeadlineExtensionSeconds) * time.Second,
	}, nil
}

func (w *Worker) Name() string { return workerName }

// Inspect は EML（添付ファイルを含む）を ClamAV でスキャンする。
// ctx のデッドラインが timeout より短い場合は ctx の期限でスキャンが中断される。
func (w *Worker) Inspect(ctx context.Context, mail *domain.Mail) (*domain.InspectResult, error) {
	result, err := scan(ctx, w.addr, w.timeout, w.chunkDeadlineExt, mail.RawEML)
	if err != nil {
		return nil, fmt.Errorf("ClamAV スキャン失敗: %w", err)
	}

	r := &domain.InspectResult{
		WorkerName: workerName,
		Details:    make(map[string]any),
	}
	if result.Detected {
		r.Detected = true
		r.Score = 100
		r.Details["virus_name"] = result.VirusName
	}
	return r, nil
}

func loadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, workerName+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{Host: "clamav", Port: 3310, TimeoutSeconds: 30, ChunkDeadlineExtensionSeconds: 10}, nil
		}
		return nil, fmt.Errorf("設定ファイル読み込み失敗 (%s): %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("設定ファイルパース失敗 (%s): %w", path, err)
	}
	if cfg.Host == "" {
		cfg.Host = "clamav"
	}
	if cfg.Port == 0 {
		cfg.Port = 3310
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = 30
	}
	if cfg.ChunkDeadlineExtensionSeconds == 0 {
		cfg.ChunkDeadlineExtensionSeconds = 10
	}
	return &cfg, nil
}
