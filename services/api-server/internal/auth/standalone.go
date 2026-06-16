package auth

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// StandaloneProvider はメール+パスワードによるスタンドアロン認証を提供する。
type StandaloneProvider struct {
	repo    repository.Repository
	authCfg *config.AuthConfig
}

// NewStandaloneProvider はStandaloneProviderを返す。
func NewStandaloneProvider(repo repository.Repository, authCfg *config.AuthConfig) *StandaloneProvider {
	return &StandaloneProvider{repo: repo, authCfg: authCfg}
}

// Login はメールアドレスとパスワードを検証してSessionを返す。
func (p *StandaloneProvider) Login(ctx context.Context, email, password string) (*domain.Session, error) {
	user, err := p.repo.FindUserByEmail(ctx, email)
	if err != nil {
		// ユーザーが存在しない場合も同じエラーメッセージにしてユーザー列挙を防ぐ
		return nil, fmt.Errorf("メールアドレスまたはパスワードが正しくありません")
	}

	if !user.IsActive {
		return nil, fmt.Errorf("アカウントが無効です")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("メールアドレスまたはパスワードが正しくありません")
	}

	session := &domain.Session{
		User: domain.UserClaims{
			Sub:   user.ID,
			Email: user.Email,
			Name:  user.DisplayName,
		},
		Role:      user.Role,
		ExpiresAt: time.Now().Add(time.Duration(p.authCfg.Session.TTLMinutes) * time.Minute),
	}

	return session, nil
}

// HashPassword はパスワードを bcrypt でハッシュ化して返す。
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("パスワードハッシュ生成失敗: %w", err)
	}
	return string(hash), nil
}
