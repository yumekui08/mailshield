// Package pwreset はパスワードリセットのトークン管理を担う。
// Redis を使い、ワンタイムトークンを短命なキーとして管理する。
package pwreset

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix  = "pwreset:"
	TokenTTL   = 30 * time.Minute
)

var ErrTokenNotFound = errors.New("リセットトークンが見つかりません（期限切れか無効）")

// Store は Redis を使ったパスワードリセットトークンの保管庫である。
type Store struct {
	client *redis.Client
}

func NewStore(client *redis.Client) *Store {
	return &Store{client: client}
}

// GenerateToken はユーザー ID に紐づくリセットトークンを生成して Redis に保存し、そのトークンを返す。
// 既存トークンがあれば上書きする（再送信用）。
func (s *Store) GenerateToken(ctx context.Context, userID string) (string, error) {
	token := uuid.New().String()
	if err := s.client.Set(ctx, keyPrefix+token, userID, TokenTTL).Err(); err != nil {
		return "", fmt.Errorf("リセットトークン保存失敗: %w", err)
	}
	return token, nil
}

// ConsumeToken はトークンを検証し、対応するユーザー ID を返す。
// トークンは取得後即座に削除される（ワンタイム）。
func (s *Store) ConsumeToken(ctx context.Context, token string) (string, error) {
	userID, err := s.client.GetDel(ctx, keyPrefix+token).Result()
	if err == redis.Nil {
		return "", ErrTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("リセットトークン取得失敗: %w", err)
	}
	return userID, nil
}
