// Package otp は添付ファイルダウンロード用のワンタイムパスワード（OTP）フローを実装する。
// Redis を使い、コード・セッションをそれぞれ短命なキーとして管理する。
package otp

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	codeKeyPrefix     = "otp_code:"     // otp_code:{token}:{email}
	attemptsKeyPrefix = "otp_attempts:" // otp_attempts:{token}:{email}
	sessKeyPrefix     = "otp_sess:"     // otp_sess:{session_id}
	CodeTTL           = 10 * time.Minute
	SessionTTL        = 30 * time.Minute
	maxAttempts       = int64(5)
)

type sessValue struct {
	Token string `json:"token"`
	Email string `json:"email"`
}

// Store は OTP コード・セッション管理の抽象インターフェースである。
// Redis 実装（RedisStore）と MariaDB 実装（MariaDBStore）が存在する。
type Store interface {
	GenerateCode(ctx context.Context, token, email string) (string, error)
	Verify(ctx context.Context, token, email, code string) (string, error)
	ValidateSession(ctx context.Context, sessID, token string) (string, error)
}

// RedisStore は Redis を使った OTP コード・セッションの保管庫である。
type RedisStore struct {
	client *redis.Client
}

func NewStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client}
}

// GenerateCode は 6 桁の数字コードを生成して Redis に保存し、そのコードを返す。
// 既存コードがあれば上書きする（再送信用）。
func (s *RedisStore) GenerateCode(ctx context.Context, token, email string) (string, error) {
	code, err := generateCode()
	if err != nil {
		return "", fmt.Errorf("OTP コード生成失敗: %w", err)
	}

	if err := s.client.Set(ctx, codeKey(token, email), code, CodeTTL).Err(); err != nil {
		return "", fmt.Errorf("OTP コード保存失敗: %w", err)
	}
	// 試行カウンタはリセットしない。
	// 再送信のたびにリセットするとブルートフォースでカウンタ回避が可能になる。
	// カウンタは maxAttempts 到達後 CodeTTL 経過で自然消滅する。

	return code, nil
}

// Verify はコードを検証し、正しければ OTP セッション ID を返す。
// コードは検証後に削除される（ワンタイム）。
// 試行回数が maxAttempts を超えた場合はエラーを返す。
func (s *RedisStore) Verify(ctx context.Context, token, email, code string) (string, error) {
	aKey := attKey(token, email)
	attempts, err := s.client.Incr(ctx, aKey).Result()
	if err != nil {
		return "", fmt.Errorf("試行回数取得失敗: %w", err)
	}
	s.client.Expire(ctx, aKey, CodeTTL)

	if attempts > maxAttempts {
		return "", ErrTooManyAttempts
	}

	stored, err := s.client.Get(ctx, codeKey(token, email)).Result()
	if err == redis.Nil {
		return "", ErrCodeNotFound
	}
	if err != nil {
		return "", fmt.Errorf("OTP コード取得失敗: %w", err)
	}

	if stored != code {
		return "", ErrInvalidCode
	}

	// 正しければコードと試行カウンタを削除（ワンタイム）
	s.client.Del(ctx, codeKey(token, email))
	s.client.Del(ctx, aKey)

	// OTP セッションを発行
	sessID := uuid.New().String()
	sv := sessValue{Token: token, Email: email}
	data, _ := json.Marshal(sv)
	if err := s.client.Set(ctx, sessKeyPrefix+sessID, data, SessionTTL).Err(); err != nil {
		return "", fmt.Errorf("OTP セッション作成失敗: %w", err)
	}

	return sessID, nil
}

// ValidateSession はセッション ID とトークンを照合し、対応するメールアドレスを返す。
func (s *RedisStore) ValidateSession(ctx context.Context, sessID, token string) (string, error) {
	data, err := s.client.Get(ctx, sessKeyPrefix+sessID).Bytes()
	if err == redis.Nil {
		return "", ErrSessionNotFound
	}
	if err != nil {
		return "", fmt.Errorf("OTP セッション取得失敗: %w", err)
	}

	var sv sessValue
	if err := json.Unmarshal(data, &sv); err != nil {
		return "", fmt.Errorf("OTP セッション解析失敗: %w", err)
	}
	if sv.Token != token {
		return "", ErrSessionNotFound
	}

	return sv.Email, nil
}

func codeKey(token, email string) string {
	return fmt.Sprintf("%s%s:%s", codeKeyPrefix, token, email)
}

func attKey(token, email string) string {
	return fmt.Sprintf("%s%s:%s", attemptsKeyPrefix, token, email)
}

func generateCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
