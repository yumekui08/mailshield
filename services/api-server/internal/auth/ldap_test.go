package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	ldapsync "github.com/koizumib/mailshield/services/api-server/internal/directory/ldap"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// fakeLDAPConn はテスト用の ldapsync.Searcher 実装。
type fakeLDAPConn struct {
	entries   []ldapsync.Entry
	searchErr error
}

func (f *fakeLDAPConn) SearchUsers(_, _ string, _ []string) ([]ldapsync.Entry, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.entries, nil
}

func (f *fakeLDAPConn) Close() error { return nil }

// fakeDialer はサービスアカウント bind（検索用）とユーザー bind（パスワード検証用）を
// BindDN/BindPassword の組み合わせで判定するフェイク dialer を構築する。
func fakeDialer(t *testing.T, serviceBindDN, servicePassword string, entries []ldapsync.Entry, userDN, correctPassword string, searchErr error) dialer {
	t.Helper()
	return func(cfg ldapsync.ConnConfig) (ldapsync.Searcher, error) {
		if cfg.BindDN == serviceBindDN && cfg.BindPassword == servicePassword {
			return &fakeLDAPConn{entries: entries, searchErr: searchErr}, nil
		}
		// ユーザー本人としての bind 検証
		if cfg.BindDN == userDN && cfg.BindPassword == correctPassword {
			return &fakeLDAPConn{}, nil
		}
		return nil, errors.New("LDAP: invalid credentials")
	}
}

// fakeProvisionerRepo は directory.Provisioner が呼ぶ UpsertFederatedUser を記録するフェイク。
type fakeProvisionerRepo struct {
	gotRole domain.Role
	user    *repository.User
	err     error
}

func (f *fakeProvisionerRepo) UpsertFederatedUser(_ context.Context, email, displayName string, role domain.Role, source domain.ProvisionedBy) (*repository.User, error) {
	f.gotRole = role
	if f.err != nil {
		return nil, f.err
	}
	if f.user != nil {
		return f.user, nil
	}
	return &repository.User{ID: "u-" + email, Email: email, DisplayName: displayName, Role: role, IsActive: true, ProvisionedBy: source}, nil
}

func testLDAPBindProvider(t *testing.T, dial dialer, repo *fakeProvisionerRepo) *LDAPBindProvider {
	t.Helper()
	return &LDAPBindProvider{
		dial:    dial,
		connCfg: ldapsync.ConnConfig{Host: "ldap.corp.local", Port: 389, BindDN: "cn=svc,dc=corp,dc=local", BindPassword: "svc-pass"},
		syncCfg: ldapsync.SyncConfig{
			BaseDN:     "ou=Users,dc=corp,dc=local",
			UserFilter: "(objectClass=person)",
			EmailAttr:  "mail",
			NameAttr:   "displayName",
			GroupsAttr: "memberOf",
			RoleMapper: directory.GroupRoleMapper{AdminGroup: "cn=Admins,dc=corp,dc=local"},
		},
		provisioner: directory.NewProvisioner(repo),
		sessionCfg:  &config.SessionConfig{TTLMinutes: 60},
	}
}

// fakeMailboxAssignmentSyncer は SyncMailboxAssignmentsForUser 呼び出しを記録するフェイク。
type fakeMailboxAssignmentSyncer struct {
	calls     []mailboxSyncCall
	mailboxes []repository.Mailbox // ListMailboxes が返す一覧（fixed 方式テスト用）
}

type mailboxSyncCall struct {
	userID  string
	source  domain.ProvisionedBy
	desired []repository.MailboxAssignmentRequest
}

func (f *fakeMailboxAssignmentSyncer) SyncMailboxAssignmentsForUser(_ context.Context, userID string, source domain.ProvisionedBy, desired []repository.MailboxAssignmentRequest) error {
	f.calls = append(f.calls, mailboxSyncCall{userID: userID, source: source, desired: desired})
	return nil
}

func (f *fakeMailboxAssignmentSyncer) ListMailboxes(_ context.Context) ([]repository.Mailbox, error) {
	return f.mailboxes, nil
}

// TestLDAPBindProvider_Login_Success は search+bind の一連の流れとロール解決を確認する。
func TestLDAPBindProvider_Login_Success(t *testing.T) {
	entries := []ldapsync.Entry{
		{DN: "cn=Alice,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{
			"mail": {"alice@corp.local"}, "displayName": {"Alice"},
			"memberOf": {"cn=Admins,dc=corp,dc=local"},
		}},
	}
	dial := fakeDialer(t, "cn=svc,dc=corp,dc=local", "svc-pass", entries, "cn=Alice,ou=Users,dc=corp,dc=local", "correct-password", nil)
	repo := &fakeProvisionerRepo{}
	p := testLDAPBindProvider(t, dial, repo)

	session, err := p.Login(context.Background(), "alice@corp.local", "correct-password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if session.User.Sub != "u-alice@corp.local" {
		t.Errorf("session.User.Sub = %q", session.User.Sub)
	}
	if session.Role != domain.RoleAdmin {
		t.Errorf("session.Role = %q, want admin", session.Role)
	}
	if repo.gotRole != domain.RoleAdmin {
		t.Errorf("provisioner に渡された role = %q, want admin", repo.gotRole)
	}
}

// TestLDAPBindProvider_Login_WrongPassword はパスワード誤りが統一エラーになることを確認する。
func TestLDAPBindProvider_Login_WrongPassword(t *testing.T) {
	entries := []ldapsync.Entry{
		{DN: "cn=Alice,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{"mail": {"alice@corp.local"}}},
	}
	dial := fakeDialer(t, "cn=svc,dc=corp,dc=local", "svc-pass", entries, "cn=Alice,ou=Users,dc=corp,dc=local", "correct-password", nil)
	p := testLDAPBindProvider(t, dial, &fakeProvisionerRepo{})

	_, err := p.Login(context.Background(), "alice@corp.local", "wrong-password")
	if !errors.Is(err, errInvalidCredentials) {
		t.Fatalf("err = %v, want errInvalidCredentials", err)
	}
}

// TestLDAPBindProvider_Login_UserNotFound は検索結果 0 件が統一エラーになることを確認する。
func TestLDAPBindProvider_Login_UserNotFound(t *testing.T) {
	dial := fakeDialer(t, "cn=svc,dc=corp,dc=local", "svc-pass", nil, "", "", nil)
	p := testLDAPBindProvider(t, dial, &fakeProvisionerRepo{})

	_, err := p.Login(context.Background(), "nobody@corp.local", "any-password")
	if !errors.Is(err, errInvalidCredentials) {
		t.Fatalf("err = %v, want errInvalidCredentials（ユーザー列挙を防ぐため notfound もパスワード誤りと同じメッセージにするべき）", err)
	}
}

// TestLDAPBindProvider_Login_MultipleMatches は検索結果が複数件のときも
// 統一エラーになることを確認する（誤設定でフィルタが緩い場合の安全策）。
func TestLDAPBindProvider_Login_MultipleMatches(t *testing.T) {
	entries := []ldapsync.Entry{
		{DN: "cn=Alice1,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{"mail": {"alice@corp.local"}}},
		{DN: "cn=Alice2,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{"mail": {"alice@corp.local"}}},
	}
	dial := fakeDialer(t, "cn=svc,dc=corp,dc=local", "svc-pass", entries, "", "", nil)
	p := testLDAPBindProvider(t, dial, &fakeProvisionerRepo{})

	_, err := p.Login(context.Background(), "alice@corp.local", "any-password")
	if !errors.Is(err, errInvalidCredentials) {
		t.Fatalf("err = %v, want errInvalidCredentials", err)
	}
}

// TestLDAPBindProvider_Login_SearchConnFailure はサービスアカウント接続失敗が
// エラーとして伝播することを確認する。
func TestLDAPBindProvider_Login_SearchConnFailure(t *testing.T) {
	dial := func(ldapsync.ConnConfig) (ldapsync.Searcher, error) {
		return nil, errors.New("connection refused")
	}
	p := testLDAPBindProvider(t, dial, &fakeProvisionerRepo{})

	_, err := p.Login(context.Background(), "alice@corp.local", "any-password")
	if err == nil {
		t.Fatal("接続失敗はエラーになるべきです")
	}
}

// TestLDAPBindProvider_Login_InactiveUser は無効化ユーザーのログインが拒否されることを確認する。
func TestLDAPBindProvider_Login_InactiveUser(t *testing.T) {
	entries := []ldapsync.Entry{
		{DN: "cn=Bob,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{"mail": {"bob@corp.local"}}},
	}
	dial := fakeDialer(t, "cn=svc,dc=corp,dc=local", "svc-pass", entries, "cn=Bob,ou=Users,dc=corp,dc=local", "correct-password", nil)
	repo := &fakeProvisionerRepo{user: &repository.User{ID: "u-bob", Email: "bob@corp.local", IsActive: false}}
	p := testLDAPBindProvider(t, dial, repo)

	_, err := p.Login(context.Background(), "bob@corp.local", "correct-password")
	if err == nil {
		t.Fatal("無効化ユーザーのログインは拒否されるべきです")
	}
}

// TestLDAPBindProvider_Login_ManualRoleProtected は provisioned_by=manual のユーザーの
// role が LDAP ログインで上書きされないことを確認する（権威順位は Provisioner/UpsertFederatedUser
// 側の責務だが、ここでは Provisioner が返した値をそのまま session に使うことを確認する）。
func TestLDAPBindProvider_Login_UsesProvisionerResult(t *testing.T) {
	entries := []ldapsync.Entry{
		{DN: "cn=Admin,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{
			"mail": {"admin@corp.local"}, "memberOf": {}, // LDAP 側は無所属
		}},
	}
	dial := fakeDialer(t, "cn=svc,dc=corp,dc=local", "svc-pass", entries, "cn=Admin,ou=Users,dc=corp,dc=local", "correct-password", nil)
	// manual で admin 昇格済みという想定（UpsertFederatedUser が権威順位に基づき admin を返す）。
	repo := &fakeProvisionerRepo{user: &repository.User{ID: "u-admin", Email: "admin@corp.local", Role: domain.RoleAdmin, IsActive: true, ProvisionedBy: domain.ProvisionedByManual}}
	p := testLDAPBindProvider(t, dial, repo)

	session, err := p.Login(context.Background(), "admin@corp.local", "correct-password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if session.Role != domain.RoleAdmin {
		t.Errorf("session.Role = %q, want admin（Provisioner の結果を尊重するべき）", session.Role)
	}
}

// TestLDAPBindProvider_Login_EmptyCredentials は空の email/password を拒否することを確認する。
func TestLDAPBindProvider_Login_EmptyCredentials(t *testing.T) {
	p := testLDAPBindProvider(t, nil, &fakeProvisionerRepo{})
	if _, err := p.Login(context.Background(), "", "pass"); !errors.Is(err, errInvalidCredentials) {
		t.Errorf("email 空のとき errInvalidCredentials を期待: %v", err)
	}
	if _, err := p.Login(context.Background(), "a@b.com", ""); !errors.Is(err, errInvalidCredentials) {
		t.Errorf("password 空のとき errInvalidCredentials を期待: %v", err)
	}
}

// TestLDAPBindProvider_Login_SyncsMailboxAssignments はログイン時に
// mailboxSyncer が本人1人分のメールボックス割り当てで呼ばれることを確認する
// （user_attribute 方式・dereference 無し。memberOf の CN を正規表現で抽出しドメイン補完）。
func TestLDAPBindProvider_Login_SyncsMailboxAssignments(t *testing.T) {
	entries := []ldapsync.Entry{
		{DN: "cn=Alice,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{
			"mail": {"alice@corp.local"},
			"memberOf": {
				"cn=mbx-sales,ou=Groups,dc=corp,dc=local",
				"cn=unrelated,ou=Groups,dc=corp,dc=local",
			},
		}},
	}
	dial := fakeDialer(t, "cn=svc,dc=corp,dc=local", "svc-pass", entries, "cn=Alice,ou=Users,dc=corp,dc=local", "correct-password", nil)
	repo := &fakeProvisionerRepo{}
	p := testLDAPBindProvider(t, dial, repo)
	memberRule, err := ldapsync.CompileChainRule(domain.AssignmentRoleMember,
		[]map[string]any{
			{"self": "memberOf"},
			{"regex": `^cn=mbx-(?P<value>[\w-]+),ou=Groups`},
			{"to_mailbox": map[string]any{"domain": "example.com"}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	p.syncCfg.MailboxResolution = &ldapsync.MailboxResolution{Roles: []ldapsync.RoleResolution{memberRule}}
	mboxSyncer := &fakeMailboxAssignmentSyncer{}
	p.mailboxSyncer = mboxSyncer

	session, err := p.Login(context.Background(), "alice@corp.local", "correct-password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if len(mboxSyncer.calls) != 1 {
		t.Fatalf("SyncMailboxAssignmentsForUser 呼び出し = %d 回, want 1", len(mboxSyncer.calls))
	}
	call := mboxSyncer.calls[0]
	if call.userID != session.User.Sub {
		t.Errorf("userID = %q, want %q（ログインしたユーザー本人のみが対象であるべき）", call.userID, session.User.Sub)
	}
	if len(call.desired) != 1 || call.desired[0].MailboxEmail != "sales@example.com" {
		t.Errorf("desired = %+v", call.desired)
	}
	if call.source != domain.ProvisionedByLDAP {
		t.Errorf("source = %q, want ldap", call.source)
	}
}

// TestLDAPBindProvider_Login_FixedRole は fixed 方式の対象ユーザーがログインしたとき、
// DB の provisioned_by=ldap メールボックス全件に対してロールが付与されることを確認する。
func TestLDAPBindProvider_Login_FixedRole(t *testing.T) {
	entries := []ldapsync.Entry{
		{DN: "cn=Admin,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{"mail": {"admin@corp.local"}}},
	}
	dial := fakeDialer(t, "cn=svc,dc=corp,dc=local", "svc-pass", entries, "cn=Admin,ou=Users,dc=corp,dc=local", "correct-password", nil)
	p := testLDAPBindProvider(t, dial, &fakeProvisionerRepo{})
	p.syncCfg.MailboxResolution = &ldapsync.MailboxResolution{Roles: []ldapsync.RoleResolution{
		ldapsync.FixedRule(domain.AssignmentRoleApprover, []string{"admin@corp.local"}),
	}}
	mboxSyncer := &fakeMailboxAssignmentSyncer{
		mailboxes: []repository.Mailbox{
			{EmailAddress: "sales@example.com", IsActive: true, ProvisionedBy: domain.ProvisionedByLDAP},
			{EmailAddress: "manual-box@example.com", IsActive: true, ProvisionedBy: domain.ProvisionedByManual},
		},
	}
	p.mailboxSyncer = mboxSyncer

	if _, err := p.Login(context.Background(), "admin@corp.local", "correct-password"); err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if len(mboxSyncer.calls) != 1 {
		t.Fatalf("呼び出し回数 = %d, want 1", len(mboxSyncer.calls))
	}
	desired := mboxSyncer.calls[0].desired
	if len(desired) != 1 || desired[0].MailboxEmail != "sales@example.com" || desired[0].Role != domain.AssignmentRoleApprover {
		t.Errorf("desired = %+v（ldap のメールボックスにのみ admin が付くべき）", desired)
	}
}

// TestLDAPBindProvider_Login_NilMailboxSyncerSkipped は mailboxSyncer が nil の場合、
// ログイン自体は成功し、割り当て同期は単純にスキップされることを確認する。
func TestLDAPBindProvider_Login_NilMailboxSyncerSkipped(t *testing.T) {
	entries := []ldapsync.Entry{
		{DN: "cn=Alice,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{"mail": {"alice@corp.local"}}},
	}
	dial := fakeDialer(t, "cn=svc,dc=corp,dc=local", "svc-pass", entries, "cn=Alice,ou=Users,dc=corp,dc=local", "correct-password", nil)
	p := testLDAPBindProvider(t, dial, &fakeProvisionerRepo{})
	// mailboxSyncer は設定しない（nil のまま）

	if _, err := p.Login(context.Background(), "alice@corp.local", "correct-password"); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
}
