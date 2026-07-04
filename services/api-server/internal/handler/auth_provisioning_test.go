package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// TestProvisionFederatedUser_NewUser は初回 OIDC ログインで users 行が作成され、
// session.User.Sub が内部 user_id に差し替わることを確認する。
func TestProvisionFederatedUser_NewUser(t *testing.T) {
	repo := &mockRepository{
		upsertFederatedUserFunc: func(_ context.Context, email, displayName string, role domain.Role, source domain.ProvisionedBy) (*repository.User, error) {
			if source != domain.ProvisionedByOIDC {
				t.Errorf("source = %q, want oidc", source)
			}
			return &repository.User{
				ID: "db-user-1", Email: email, DisplayName: displayName,
				Role: role, IsActive: true, ProvisionedBy: source,
			}, nil
		},
	}
	h := &AuthHandler{provisioner: directory.NewProvisioner(repo)}
	session := &domain.Session{
		User: domain.UserClaims{Sub: "oidc-sub-xyz", Email: "user@example.com", Name: "Test User"},
		Role: domain.RoleOperator,
	}

	if err := h.provisionFederatedUser(context.Background(), session, domain.ProvisionedByOIDC); err != nil {
		t.Fatalf("provisionFederatedUser() error = %v", err)
	}
	if session.User.Sub != "db-user-1" {
		t.Errorf("session.User.Sub = %q, want db-user-1（内部 user_id に差し替わるべき）", session.User.Sub)
	}
	if session.Role != domain.RoleOperator {
		t.Errorf("session.Role = %q, want operator", session.Role)
	}
}

// TestProvisionFederatedUser_ManualRoleWins は provisioned_by=manual の既存ユーザーに対し、
// OIDC 側の role が上書きされないことを確認する（DB 側が権威となる想定を反映するモック）。
func TestProvisionFederatedUser_ManualRoleWins(t *testing.T) {
	repo := &mockRepository{
		upsertFederatedUserFunc: func(_ context.Context, email, _ string, _ domain.Role, _ domain.ProvisionedBy) (*repository.User, error) {
			// 手動作成の admin ユーザーとしてすでに存在する想定。
			// UpsertFederatedUser の SQL 側ロジック（IF(provisioned_by='manual', role, VALUES(role))）が
			// 上書きしなかった結果を模して、DB 由来の role をそのまま返す。
			return &repository.User{
				ID: "db-user-2", Email: email, Role: domain.RoleAdmin,
				IsActive: true, ProvisionedBy: domain.ProvisionedByManual,
			}, nil
		},
	}
	h := &AuthHandler{provisioner: directory.NewProvisioner(repo)}
	// OIDC グループからは viewer と判定されたとする。
	session := &domain.Session{
		User: domain.UserClaims{Sub: "oidc-sub-abc", Email: "admin@example.com"},
		Role: domain.RoleViewer,
	}

	if err := h.provisionFederatedUser(context.Background(), session, domain.ProvisionedByOIDC); err != nil {
		t.Fatalf("provisionFederatedUser() error = %v", err)
	}
	if session.Role != domain.RoleAdmin {
		t.Errorf("session.Role = %q, want admin（manual 設定が優先されるべき）", session.Role)
	}
}

// TestProvisionFederatedUser_InactiveRejected は無効化されたユーザーのログインが
// errFederatedUserInactive を返すことを確認する。
func TestProvisionFederatedUser_InactiveRejected(t *testing.T) {
	repo := &mockRepository{
		upsertFederatedUserFunc: func(_ context.Context, email, _ string, _ domain.Role, _ domain.ProvisionedBy) (*repository.User, error) {
			return &repository.User{ID: "db-user-3", Email: email, IsActive: false}, nil
		},
	}
	h := &AuthHandler{provisioner: directory.NewProvisioner(repo)}
	session := &domain.Session{User: domain.UserClaims{Email: "disabled@example.com"}, Role: domain.RoleViewer}

	err := h.provisionFederatedUser(context.Background(), session, domain.ProvisionedByOIDC)
	if !errors.Is(err, errFederatedUserInactive) {
		t.Fatalf("err = %v, want errFederatedUserInactive", err)
	}
}

// TestProvisionFederatedUser_RepoError はリポジトリエラーがそのまま伝播することを確認する。
func TestProvisionFederatedUser_RepoError(t *testing.T) {
	wantErr := errors.New("db down")
	repo := &mockRepository{
		upsertFederatedUserFunc: func(context.Context, string, string, domain.Role, domain.ProvisionedBy) (*repository.User, error) {
			return nil, wantErr
		},
	}
	h := &AuthHandler{provisioner: directory.NewProvisioner(repo)}
	session := &domain.Session{User: domain.UserClaims{Email: "x@example.com"}}

	err := h.provisionFederatedUser(context.Background(), session, domain.ProvisionedByOIDC)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}
