// Package middleware はHTTPミドルウェアを提供する。
package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/koizumib/mailshield/services/api-server/internal/auth"
	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

type contextKey string

const contextKeySession contextKey = "session"

// errorResponse はエラーレスポンスの形式を定義する。
type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError はJSON形式のエラーレスポンスを書き込む。
func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Error: errorDetail{
			Code:    code,
			Message: message,
		},
	})
}

// Authenticate はCookieからセッションを検索して認証を行うミドルウェアである。
// セッションが有効な場合はcontextにセッション情報を格納して次のハンドラーに渡す。
// 未認証の場合は401を返す。
func Authenticate(store *auth.SessionStore, cfg *config.SessionConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(cfg.CookieName)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
				return
			}

			sessionID := cookie.Value
			if sessionID == "" {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
				return
			}

			session, err := store.Get(r.Context(), sessionID)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "セッションが無効です")
				return
			}

			ctx := context.WithValue(r.Context(), contextKeySession, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthenticateOrAPIKey はCookieセッションを優先し、なければ Bearer API キーで認証するミドルウェアである。
func AuthenticateOrAPIKey(store *auth.SessionStore, cfg *config.SessionConfig, repo repository.Repository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Cookie セッション優先
			if cookie, err := r.Cookie(cfg.CookieName); err == nil && cookie.Value != "" {
				if session, err := store.Get(r.Context(), cookie.Value); err == nil {
					ctx := context.WithValue(r.Context(), contextKeySession, session)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// 2. Bearer API キー
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				rawKey := strings.TrimPrefix(authHeader, "Bearer ")
				hashBytes := sha256.Sum256([]byte(rawKey))
				keyHash := hex.EncodeToString(hashBytes[:])

				key, err := repo.FindAPIKeyByHash(r.Context(), keyHash)
				if err != nil {
					slog.Warn("API キー検索失敗", "error", err)
				} else if key != nil && key.IsActive() {
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						if err := repo.UpdateAPIKeyLastUsed(ctx, key.ID); err != nil {
							slog.Warn("API キー最終使用日時更新失敗", "id", key.ID, "error", err)
						}
					}()

					sub := "apikey:" + key.ID
					session := &domain.Session{
						ID:   key.ID,
						Role: key.Role,
						User: domain.UserClaims{
							Sub:  sub,
							Name: key.Name,
						},
					}
					ctx := context.WithValue(r.Context(), contextKeySession, session)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
		})
	}
}

// GetSession はcontextからセッションを取得するヘルパー関数である。
func GetSession(ctx context.Context) *domain.Session {
	session, _ := ctx.Value(contextKeySession).(*domain.Session)
	return session
}

// WithSession はcontextにセッションを格納して返すヘルパー関数である。
// 主にテストで使用する。
func WithSession(ctx context.Context, session *domain.Session) context.Context {
	return context.WithValue(ctx, contextKeySession, session)
}

// RequireRole は指定されたロールのいずれかを持つユーザーのみ許可するミドルウェアである。
// 認証ミドルウェアの後に使用する必要がある。
func RequireRole(roles ...domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := GetSession(r.Context())
			if session == nil {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "認証が必要です")
				return
			}

			for _, role := range roles {
				if session.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeError(w, http.StatusForbidden, "FORBIDDEN", "権限がありません")
		})
	}
}

// viewerOrAbove はViewer以上のロールを要求するミドルウェアを返す。
func ViewerOrAbove() func(http.Handler) http.Handler {
	return RequireRole(domain.RoleViewer, domain.RoleOperator, domain.RoleAdmin)
}

// operatorOrAbove はOperator以上のロールを要求するミドルウェアを返す。
func OperatorOrAbove() func(http.Handler) http.Handler {
	return RequireRole(domain.RoleOperator, domain.RoleAdmin)
}
