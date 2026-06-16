// Package logging は log/slog のハンドラーを構築するユーティリティを提供する。
// 出力先は stdout（デフォルト）と syslog から選択でき、
// フォーマットは json / text から選択できる。
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"log/syslog"
	"os"
	"strings"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
)

// Setup は cfg.Log の設定に従って slog のデフォルトロガーを初期化する。
// 呼び出し後は slog.Info() 等がそのまま使える。
func Setup(cfg *config.LogConfig) error {
	level := parseLevel(cfg.Level)

	w, err := openWriter(cfg)
	if err != nil {
		return err
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}

	if strings.ToLower(cfg.Format) == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}

	slog.SetDefault(slog.New(handler))
	return nil
}

// openWriter は cfg.Output に応じた io.Writer を返す。
func openWriter(cfg *config.LogConfig) (io.Writer, error) {
	switch strings.ToLower(cfg.Output) {
	case "syslog":
		tag := cfg.SyslogTag
		if tag == "" {
			tag = "smtp-gateway"
		}
		w, err := syslog.New(syslog.LOG_MAIL|syslog.LOG_INFO, tag)
		if err != nil {
			return nil, fmt.Errorf("syslog 接続失敗: %w", err)
		}
		return w, nil
	default:
		// stdout（デフォルト）
		return os.Stdout, nil
	}
}

// parseLevel は文字列表現のログレベルを slog.Level に変換する。
// 不明な値は INFO にフォールバックする。
func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
