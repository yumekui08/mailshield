package directory

import (
	"context"
	"fmt"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// UserUpserter は Provisioner が必要とする repository.Repository のサブセット。
// コンシューマー側（本パッケージ）で狭く定義する。
type UserUpserter interface {
	UpsertFederatedUser(ctx context.Context, email, displayName string, role domain.Role, source domain.ProvisionedBy) (*repository.User, error)
}

// Provisioner は ExternalIdentity を受け取り users テーブルへ反映する、
// OIDC JIT・LDAP 同期・SCIM push に共通の入口である。
// role の権威順位（manual > ldap/scim > oidc）の解決は UpsertFederatedUser 側に委譲する。
type Provisioner struct {
	repo UserUpserter
}

// NewProvisioner は Provisioner を返す。
func NewProvisioner(repo UserUpserter) *Provisioner {
	return &Provisioner{repo: repo}
}

// Provision は identity を users テーブルへ反映し、確定したユーザー行を返す。
func (p *Provisioner) Provision(ctx context.Context, identity ExternalIdentity) (*repository.User, error) {
	u, err := p.repo.UpsertFederatedUser(ctx, identity.Email, identity.DisplayName, identity.Role, identity.Source)
	if err != nil {
		return nil, fmt.Errorf("プロビジョニング失敗 (email=%s, source=%s): %w", identity.Email, identity.Source, err)
	}
	return u, nil
}
