package middleware

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimiter はクライアント IP 単位のスライディングウィンドウ・レート制限を提供する。
// ログイン・パスワードリセット・OTP 発行などの認証系エンドポイントへの
// ブルートフォース攻撃・メール送信の濫用を防ぐことが目的である。
//
// 状態はプロセス内メモリに保持する（再起動でリセットされる）。
// 複数レプリカ構成では各レプリカが独立してカウントするため、
// 実効上限は max × レプリカ数 となる点に注意すること。
type RateLimiter struct {
	mu        sync.Mutex
	max       int
	window    time.Duration
	hits      map[string][]time.Time
	lastSweep time.Time
	now       func() time.Time // テストで差し替える
}

// NewRateLimiter は max 回 / window のレート制限を返す。
func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		max:    max,
		window: window,
		hits:   make(map[string][]time.Time),
		now:    time.Now,
	}
}

// Allow は key のリクエストを受け付けてよいかを判定し、受け付ける場合は1回分を記録する。
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)

	// ウィンドウを過ぎた全キーの掃除（メモリの無限成長を防ぐ）
	if now.Sub(l.lastSweep) > l.window {
		for k, ts := range l.hits {
			if len(ts) == 0 || ts[len(ts)-1].Before(cutoff) {
				delete(l.hits, k)
			}
		}
		l.lastSweep = now
	}

	ts := l.hits[key]
	// ウィンドウ内のみ残す
	valid := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= l.max {
		l.hits[key] = valid
		return false
	}
	l.hits[key] = append(valid, now)
	return true
}

// Middleware はクライアント IP をキーにレート制限を適用する http ミドルウェアを返す。
// 上限超過時は 429 と Retry-After ヘッダーを返す。
// chi の RealIP ミドルウェアの後段で使用すること。
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !l.Allow(ip) {
			w.Header().Set("Retry-After", strconv.Itoa(int(l.window.Seconds())))
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED",
				"リクエストが多すぎます。しばらく待ってから再試行してください")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// PassThrough はレート制限無効時に使うノーオペのミドルウェアである。
func PassThrough(next http.Handler) http.Handler {
	return next
}
