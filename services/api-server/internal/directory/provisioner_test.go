package directory

import (
	"context"
	"errors"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

type stubUserUpserter struct {
	gotEmail       string
	gotDisplayName string
	gotRole        domain.Role
	gotSource      domain.ProvisionedBy
	returnUser     *repository.User
	returnErr      error
}

func (s *stubUserUpserter) UpsertFederatedUser(_ context.Context, email, displayName string, role domain.Role, source domain.ProvisionedBy) (*repository.User, error) {
	s.gotEmail, s.gotDisplayName, s.gotRole, s.gotSource = email, displayName, role, source
	return s.returnUser, s.returnErr
}

// TestProvisioner_Provision は identity の各フィールドが repo にそのまま渡され、
// 結果のユーザーがそのまま返ることを確認する。
func TestProvisioner_Provision(t *testing.T) {
	stub := &stubUserUpserter{returnUser: &repository.User{ID: "u1", Email: "a@example.com", Role: domain.RoleOperator}}
	p := NewProvisioner(stub)

	identity := ExternalIdentity{
		Email: "a@example.com", DisplayName: "A", Role: domain.RoleOperator, Source: domain.ProvisionedByLDAP,
	}
	u, err := p.Provision(context.Background(), identity)
	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}
	if u.ID != "u1" {
		t.Errorf("u.ID = %q, want u1", u.ID)
	}
	if stub.gotEmail != identity.Email || stub.gotDisplayName != identity.DisplayName ||
		stub.gotRole != identity.Role || stub.gotSource != identity.Source {
		t.Errorf("repo に渡された引数が identity と一致しない: %+v", stub)
	}
}

// TestProvisioner_Provision_Error はリポジトリのエラーがラップされて返ることを確認する。
func TestProvisioner_Provision_Error(t *testing.T) {
	wantErr := errors.New("db error")
	stub := &stubUserUpserter{returnErr: wantErr}
	p := NewProvisioner(stub)

	_, err := p.Provision(context.Background(), ExternalIdentity{Email: "x@example.com", Source: domain.ProvisionedBySCIM})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapping %v", err, wantErr)
	}
}
