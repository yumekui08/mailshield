package ldap

import (
	"context"
	"errors"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// fakeSearcher はテスト用の Searcher 実装。
type fakeSearcher struct {
	entries   []Entry
	searchErr error
	closed    bool
}

func (f *fakeSearcher) SearchUsers(_, _ string, _ []string) ([]Entry, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.entries, nil
}

func (f *fakeSearcher) Close() error {
	f.closed = true
	return nil
}

// fakeProvisionerRepo は Provisioner が呼ぶ UpsertFederatedUser を記録するフェイク。
type fakeProvisionerRepo struct {
	calls []directory.ExternalIdentity
	err   error
}

func (f *fakeProvisionerRepo) UpsertFederatedUser(_ context.Context, email, displayName string, role domain.Role, source domain.ProvisionedBy) (*repository.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.calls = append(f.calls, directory.ExternalIdentity{Email: email, DisplayName: displayName, Role: role, Source: source})
	return &repository.User{ID: "u-" + email, Email: email, DisplayName: displayName, Role: role, IsActive: true, ProvisionedBy: source}, nil
}

// fakeDeactivator は DeactivateMissingLDAPUsers 呼び出しを記録するフェイク。
type fakeDeactivator struct {
	gotPresentEmails []string
	called           bool
	returnN          int
	returnErr        error
}

func (f *fakeDeactivator) DeactivateMissingLDAPUsers(_ context.Context, presentEmails []string) (int, error) {
	f.called = true
	f.gotPresentEmails = presentEmails
	return f.returnN, f.returnErr
}

func testSyncConfig() SyncConfig {
	return SyncConfig{
		BaseDN:     "OU=Users,DC=corp,DC=local",
		UserFilter: "(objectClass=person)",
		EmailAttr:  "mail",
		NameAttr:   "displayName",
		GroupsAttr: "memberOf",
		RoleMapper: directory.GroupRoleMapper{
			AdminGroup: "CN=Admins,DC=corp,DC=local",
		},
	}
}

// TestSyncer_Sync_BasicFlow はエントリが Provisioner に正しく渡ることを確認する。
func TestSyncer_Sync_BasicFlow(t *testing.T) {
	searcher := &fakeSearcher{entries: []Entry{
		{DN: "CN=Alice,OU=Users,DC=corp,DC=local", Attributes: map[string][]string{
			"mail": {"alice@corp.local"}, "displayName": {"Alice"},
			"memberOf": {"CN=Admins,DC=corp,DC=local"},
		}},
		{DN: "CN=Bob,OU=Users,DC=corp,DC=local", Attributes: map[string][]string{
			"mail": {"bob@corp.local"}, "displayName": {"Bob"},
		}},
	}}
	repo := &fakeProvisionerRepo{}
	deact := &fakeDeactivator{}
	s := NewSyncer(directory.NewProvisioner(repo), deact, nil, testSyncConfig())

	result, err := s.Sync(context.Background(), searcher)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Synced != 2 {
		t.Errorf("Synced = %d, want 2", result.Synced)
	}
	if len(repo.calls) != 2 {
		t.Fatalf("repo.calls = %d 件, want 2", len(repo.calls))
	}
	if repo.calls[0].Role != domain.RoleAdmin {
		t.Errorf("alice の role = %q, want admin（memberOf に Admins グループを含むため）", repo.calls[0].Role)
	}
	if repo.calls[1].Role != domain.RoleViewer {
		t.Errorf("bob の role = %q, want viewer（フォールバック）", repo.calls[1].Role)
	}
	for _, c := range repo.calls {
		if c.Source != domain.ProvisionedByLDAP {
			t.Errorf("source = %q, want ldap", c.Source)
		}
	}
}

// TestSyncer_Sync_SkipsEmptyEmail は email 属性が空のエントリをスキップすることを確認する。
func TestSyncer_Sync_SkipsEmptyEmail(t *testing.T) {
	searcher := &fakeSearcher{entries: []Entry{
		{DN: "CN=NoMail,OU=Users,DC=corp,DC=local", Attributes: map[string][]string{"displayName": {"NoMail"}}},
	}}
	repo := &fakeProvisionerRepo{}
	s := NewSyncer(directory.NewProvisioner(repo), &fakeDeactivator{}, nil, testSyncConfig())

	result, err := s.Sync(context.Background(), searcher)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Skipped != 1 || result.Synced != 0 {
		t.Errorf("Skipped=%d Synced=%d, want Skipped=1 Synced=0", result.Skipped, result.Synced)
	}
	if len(repo.calls) != 0 {
		t.Errorf("email が空のエントリは Provision されるべきではない: %+v", repo.calls)
	}
}

// TestSyncer_Sync_DeactivateMissing は同期成功後に deactivator が present なメールで呼ばれることを確認する。
func TestSyncer_Sync_DeactivateMissing(t *testing.T) {
	searcher := &fakeSearcher{entries: []Entry{
		{DN: "CN=Alice", Attributes: map[string][]string{"mail": {"alice@corp.local"}}},
	}}
	deact := &fakeDeactivator{returnN: 3}
	cfg := testSyncConfig()
	cfg.DeactivateMissing = true
	s := NewSyncer(directory.NewProvisioner(&fakeProvisionerRepo{}), deact, nil, cfg)

	result, err := s.Sync(context.Background(), searcher)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if !deact.called {
		t.Fatal("DeactivateMissingLDAPUsers が呼ばれていません")
	}
	if len(deact.gotPresentEmails) != 1 || deact.gotPresentEmails[0] != "alice@corp.local" {
		t.Errorf("presentEmails = %v, want [alice@corp.local]", deact.gotPresentEmails)
	}
	if result.Deactivated != 3 {
		t.Errorf("Deactivated = %d, want 3", result.Deactivated)
	}
}

// TestSyncer_Sync_EmptyResultSkipsDeactivation は検索結果が0件のとき、
// 誤って全ユーザーを無効化しないよう deactivator を呼ばないことを確認する。
func TestSyncer_Sync_EmptyResultSkipsDeactivation(t *testing.T) {
	searcher := &fakeSearcher{entries: nil}
	deact := &fakeDeactivator{}
	cfg := testSyncConfig()
	cfg.DeactivateMissing = true
	s := NewSyncer(directory.NewProvisioner(&fakeProvisionerRepo{}), deact, nil, cfg)

	if _, err := s.Sync(context.Background(), searcher); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if deact.called {
		t.Error("検索結果0件では DeactivateMissingLDAPUsers を呼ぶべきではありません（誤検知による全ユーザー無効化を防ぐ）")
	}
}

// TestSyncer_Sync_SearchError は検索失敗がそのままエラーとして返ることを確認する。
func TestSyncer_Sync_SearchError(t *testing.T) {
	wantErr := errors.New("connection reset")
	searcher := &fakeSearcher{searchErr: wantErr}
	s := NewSyncer(directory.NewProvisioner(&fakeProvisionerRepo{}), &fakeDeactivator{}, nil, testSyncConfig())

	_, err := s.Sync(context.Background(), searcher)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapping %v", err, wantErr)
	}
}

// TestSyncer_Sync_ProvisionErrorContinues は1件のプロビジョニング失敗が
// 他のエントリの処理を止めないことを確認する。
func TestSyncer_Sync_ProvisionErrorContinues(t *testing.T) {
	searcher := &fakeSearcher{entries: []Entry{
		{DN: "CN=Alice", Attributes: map[string][]string{"mail": {"alice@corp.local"}}},
	}}
	repo := &fakeProvisionerRepo{err: errors.New("db down")}
	s := NewSyncer(directory.NewProvisioner(repo), &fakeDeactivator{}, nil, testSyncConfig())

	result, err := s.Sync(context.Background(), searcher)
	if err != nil {
		t.Fatalf("Sync() 全体はエラーを返すべきではない（個々のエラーは Result.Errors に集約）: %v", err)
	}
	if len(result.Errors) != 1 {
		t.Errorf("Errors = %d 件, want 1", len(result.Errors))
	}
	if result.Synced != 0 {
		t.Errorf("Synced = %d, want 0", result.Synced)
	}
}

// fakeMailboxAssignmentSyncer は SyncMailboxAssignmentsForUser 呼び出しを記録するフェイク。
type fakeMailboxAssignmentSyncer struct {
	calls []mailboxSyncCall
	err   error
}

type mailboxSyncCall struct {
	userID  string
	source  domain.ProvisionedBy
	desired []repository.MailboxAssignmentRequest
}

func (f *fakeMailboxAssignmentSyncer) SyncMailboxAssignmentsForUser(_ context.Context, userID string, source domain.ProvisionedBy, desired []repository.MailboxAssignmentRequest) error {
	f.calls = append(f.calls, mailboxSyncCall{userID: userID, source: source, desired: desired})
	return f.err
}

// TestSyncer_Sync_MailboxAssignments_ExplicitMapping はグループの明示マッピングから
// メールボックス割り当てが解決され、ユーザーごとに SyncMailboxAssignmentsForUser が
// 呼ばれることを確認する。CN は DN から抽出される。
func TestSyncer_Sync_MailboxAssignments_ExplicitMapping(t *testing.T) {
	searcher := &fakeSearcher{entries: []Entry{
		{DN: "CN=Alice,OU=Users,DC=corp,DC=local", Attributes: map[string][]string{
			"mail": {"alice@corp.local"},
			"memberOf": {
				"CN=Sales-Team,OU=Groups,DC=corp,DC=local",
			},
		}},
		{DN: "CN=Bob,OU=Users,DC=corp,DC=local", Attributes: map[string][]string{
			"mail": {"bob@corp.local"}, // メールボックスグループには非所属
		}},
	}}
	repo := &fakeProvisionerRepo{}
	mboxSyncer := &fakeMailboxAssignmentSyncer{}
	cfg := testSyncConfig()
	cfg.MailboxMapper = directory.GroupMailboxMapper{
		Mappings: []directory.GroupMailboxMapping{
			{Group: "Sales-Team", MailboxEmail: "sales@example.com", MailboxDisplayName: "Sales", Role: domain.AssignmentRoleMember},
		},
	}
	s := NewSyncer(directory.NewProvisioner(repo), &fakeDeactivator{}, mboxSyncer, cfg)

	if _, err := s.Sync(context.Background(), searcher); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if len(mboxSyncer.calls) != 2 {
		t.Fatalf("SyncMailboxAssignmentsForUser 呼び出し = %d 回, want 2（全ユーザー分呼ばれるべき）", len(mboxSyncer.calls))
	}

	aliceCall := mboxSyncer.calls[0]
	if aliceCall.userID != "u-alice@corp.local" {
		t.Errorf("alice の userID = %q", aliceCall.userID)
	}
	if len(aliceCall.desired) != 1 || aliceCall.desired[0].MailboxEmail != "sales@example.com" {
		t.Errorf("alice の desired = %+v", aliceCall.desired)
	}
	if aliceCall.source != domain.ProvisionedByLDAP {
		t.Errorf("source = %q, want ldap", aliceCall.source)
	}

	bobCall := mboxSyncer.calls[1]
	if len(bobCall.desired) != 0 {
		t.Errorf("bob はどのメールボックスグループにも属さないので desired は空であるべき: %+v", bobCall.desired)
	}
}

// TestSyncer_Sync_MailboxAssignments_EmptyDesiredStillCalled は、あるユーザーの
// 解決結果が0件でも SyncMailboxAssignmentsForUser が呼ばれることを確認する
// （グループから正当に離脱したユーザーの割り当てを剥奪するために必要。
// 「0件ガード」はユーザー個別の粒度では適用しない）。
func TestSyncer_Sync_MailboxAssignments_EmptyDesiredStillCalled(t *testing.T) {
	searcher := &fakeSearcher{entries: []Entry{
		{DN: "CN=Alice,OU=Users,DC=corp,DC=local", Attributes: map[string][]string{"mail": {"alice@corp.local"}}},
	}}
	mboxSyncer := &fakeMailboxAssignmentSyncer{}
	cfg := testSyncConfig()
	cfg.MailboxMapper = directory.GroupMailboxMapper{
		Mappings: []directory.GroupMailboxMapping{
			{Group: "Sales-Team", MailboxEmail: "sales@example.com", Role: domain.AssignmentRoleMember},
		},
	}
	s := NewSyncer(directory.NewProvisioner(&fakeProvisionerRepo{}), &fakeDeactivator{}, mboxSyncer, cfg)

	if _, err := s.Sync(context.Background(), searcher); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if len(mboxSyncer.calls) != 1 {
		t.Fatalf("呼び出し回数 = %d, want 1", len(mboxSyncer.calls))
	}
	if len(mboxSyncer.calls[0].desired) != 0 {
		t.Errorf("desired = %+v, want 空", mboxSyncer.calls[0].desired)
	}
}

// TestSyncer_Sync_MailboxAssignments_NilSyncerSkipped は mailboxSyncer が nil の場合、
// メールボックス割り当ての解決・呼び出しを一切行わないことを確認する。
func TestSyncer_Sync_MailboxAssignments_NilSyncerSkipped(t *testing.T) {
	searcher := &fakeSearcher{entries: []Entry{
		{DN: "CN=Alice,OU=Users,DC=corp,DC=local", Attributes: map[string][]string{"mail": {"alice@corp.local"}}},
	}}
	cfg := testSyncConfig()
	cfg.MailboxMapper = directory.GroupMailboxMapper{
		Mappings: []directory.GroupMailboxMapping{
			{Group: "Sales-Team", MailboxEmail: "sales@example.com", Role: domain.AssignmentRoleMember},
		},
	}
	s := NewSyncer(directory.NewProvisioner(&fakeProvisionerRepo{}), &fakeDeactivator{}, nil, cfg)

	if _, err := s.Sync(context.Background(), searcher); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	// mailboxSyncer が nil でもパニックしないことが本テストの主眼
}

func TestExtractCN(t *testing.T) {
	tests := []struct {
		name string
		dn   string
		want string
	}{
		{"標準的なDN", "CN=Sales-Team,OU=Groups,DC=corp,DC=local", "Sales-Team"},
		{"CNが複数属性の先頭RDNの一部", "CN=Alice Admin,OU=Users,DC=corp,DC=local", "Alice Admin"},
		{"不正なDNはそのまま返す", "12345", "12345"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractCN(tt.dn); got != tt.want {
				t.Errorf("ExtractCN(%q) = %q, want %q", tt.dn, got, tt.want)
			}
		})
	}
}
