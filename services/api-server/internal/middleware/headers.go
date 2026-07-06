package middleware

import "net/http"

// SecurityHeaders は全レスポンスに防御的な HTTP ヘッダーを付与するミドルウェアである。
//   - X-Content-Type-Options: MIME スニッフィング防止
//   - X-Frame-Options: クリックジャッキング防止（API はフレーム埋め込み不要）
//   - Referrer-Policy: トークン入り URL（添付ファイルダウンロード等）のリファラー漏洩防止
//   - Cache-Control: 認証済み API レスポンスの共有キャッシュ・ブラウザキャッシュ防止
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
