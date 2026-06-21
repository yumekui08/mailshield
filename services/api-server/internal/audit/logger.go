// Package audit は監査ログの記録を担う。
// 書き込みはベストエフォート（失敗しても本体操作を妨げない）。
package audit

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// Writer は監査ログを永続化するインターフェース。
type Writer interface {
	CreateAuditLog(ctx context.Context, log *domain.AuditLog) error
}

// Logger はベストエフォートで監査ログを記録する。
type Logger struct {
	writer Writer
}

// New は Logger を返す。
func New(writer Writer) *Logger {
	return &Logger{writer: writer}
}

// Log は監査ログを1件記録する。
// 書き込みはゴルーチンで非同期実行し、呼び出し元のレスポンスをブロックしない。
// 書き込み失敗は WARN ログに記録するだけで呼び出し元に伝播しない。
// writer が nil の場合（テスト等）は何もしない。
func (l *Logger) Log(entry domain.AuditLog) {
	if l == nil || l.writer == nil {
		return
	}
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := l.writer.CreateAuditLog(ctx, &entry); err != nil {
			slog.Warn("監査ログ記録失敗",
				"event_type", entry.EventType,
				"actor_id", entry.ActorID,
				"error", err,
			)
		}
	}()
}

// ClientIP はリクエストからクライアント IP アドレスを抽出する。
// リバースプロキシを考慮して X-Forwarded-For を優先する。
func ClientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// StrPtr は文字列ポインタを返すユーティリティ。
func StrPtr(s string) *string {
	return &s
}
