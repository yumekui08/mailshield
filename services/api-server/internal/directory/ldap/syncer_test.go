package ldap

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// fakeSearcher はテスト用の Searcher 実装。
// searchFunc が設定されていれば base_dn / filter に応じた応答を返せる
// （group_search / dereference のように検索先が複数あるテストで使う）。
type fakeSearcher struct {
	entries    []Entry
	searchErr  error
	closed     bool
	searchFunc func(baseDN, filter string, attrs []string) ([]Entry, error)
}

func (f *fakeSearcher) SearchUsers(baseDN, filter string, attrs []string) ([]Entry, error) {
	if f.searchFunc != nil {
		return f.searchFunc(baseDN, filter, attrs)
	}
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
	calls     []mailboxSyncCall
	err       error
	mailboxes []repository.Mailbox // ListMailboxes が返す一覧（fixed 方式テスト用）
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

func (f *fakeMailboxAssignmentSyncer) ListMailboxes(_ context.Context) ([]repository.Mailbox, error) {
	return f.mailboxes, nil
}

// userAttrResolution はチェーン（memberOf → CN 抽出 + ドメイン補完）の
// テスト用 MailboxResolution を返す。グループ CN "mbx-<name>" を <name>@example.com に解決する。
func userAttrResolution(t *testing.T, role domain.AssignmentRole) *MailboxResolution {
	t.Helper()
	rr, err := CompileChainRule(role,
		[]map[string]any{
			{"self": "memberOf"},
			{"regex": `^cn=mbx-(?P<value>[\w-]+),ou=Groups`},
			{"to_mailbox": map[string]any{"domain": "example.com"}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return &MailboxResolution{Roles: []RoleResolution{rr}}
}

// TestSyncer_Sync_MailboxAssignments_UserAttribute は user_attribute 方式（dereference 無し）で
// メールボックス割り当てが解決され、ユーザーごとに SyncMailboxAssignmentsForUser が
// 呼ばれることを確認する。
func TestSyncer_Sync_MailboxAssignments_UserAttribute(t *testing.T) {
	searcher := &fakeSearcher{entries: []Entry{
		{DN: "cn=Alice,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{
			"mail":     {"alice@corp.local"},
			"memberOf": {"cn=mbx-sales,ou=Groups,dc=corp,dc=local", "cn=unrelated,ou=Groups,dc=corp,dc=local"},
		}},
		{DN: "cn=Bob,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{
			"mail": {"bob@corp.local"}, // メールボックスグループには非所属
		}},
	}}
	repo := &fakeProvisionerRepo{}
	mboxSyncer := &fakeMailboxAssignmentSyncer{}
	cfg := testSyncConfig()
	cfg.MailboxResolution = userAttrResolution(t, domain.AssignmentRoleMember)
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

// TestSyncer_Sync_MailboxAssignments_GroupSearch は group_search 方式の一括検索で
// グループの member 属性からユーザーへの割り当てが解決されることを確認する。
// member DN とユーザー DN の表記ゆれ（大文字小文字）も正規化で吸収される。
func TestSyncer_Sync_MailboxAssignments_GroupSearch(t *testing.T) {
	userEntries := []Entry{
		{DN: "cn=Alice,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{"mail": {"alice@corp.local"}}},
		{DN: "cn=Bob,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{"mail": {"bob@corp.local"}}},
	}
	groupEntries := []Entry{
		{DN: "cn=Sales,ou=Groups,dc=corp,dc=local", Attributes: map[string][]string{
			"mail": {"sales@example.com"},
			// 大文字表記（正規化されて alice の DN と一致するべき）
			"member": {"CN=Alice,OU=Users,DC=corp,DC=local"},
		}},
	}
	searcher := &fakeSearcher{searchFunc: func(baseDN, filter string, _ []string) ([]Entry, error) {
		if baseDN == "ou=Groups,dc=corp,dc=local" {
			// member 絞り込み: Alice の DN を含むフィルタのときだけ sales グループを返す
			if strings.Contains(strings.ToLower(filter), "cn=alice") {
				return groupEntries, nil
			}
			return nil, nil
		}
		return userEntries, nil
	}}

	mboxSyncer := &fakeMailboxAssignmentSyncer{}
	cfg := testSyncConfig()
	// チェーン: 自分がメンバーのグループを検索し、そのグループの mail をメールボックスにする
	ownerRule, err := CompileChainRule(domain.AssignmentRoleOwner,
		[]map[string]any{
			{"self": "dn"},
			{"search": map[string]any{"base_dn": "ou=Groups,dc=corp,dc=local", "filter": "(&(mail=*)(member={value}))"}},
			{"attr": "mail"},
			{"to_mailbox": map[string]any{}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MailboxResolution = &MailboxResolution{Roles: []RoleResolution{ownerRule}}
	s := NewSyncer(directory.NewProvisioner(&fakeProvisionerRepo{}), &fakeDeactivator{}, mboxSyncer, cfg)

	if _, err := s.Sync(context.Background(), searcher); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if len(mboxSyncer.calls) != 2 {
		t.Fatalf("呼び出し回数 = %d, want 2", len(mboxSyncer.calls))
	}
	aliceCall := mboxSyncer.calls[0]
	if len(aliceCall.desired) != 1 || aliceCall.desired[0].MailboxEmail != "sales@example.com" || aliceCall.desired[0].Role != domain.AssignmentRoleOwner {
		t.Errorf("alice の desired = %+v（group_search で owner が付くべき）", aliceCall.desired)
	}
	bobCall := mboxSyncer.calls[1]
	if len(bobCall.desired) != 0 {
		t.Errorf("bob の desired = %+v, want 空", bobCall.desired)
	}
}

// TestSyncer_Sync_MailboxAssignments_Fixed は fixed 方式の対象ユーザーが第2パスで処理され、
// DB の provisioned_by=ldap 全メールボックスに対してロールが付与されることを確認する。
func TestSyncer_Sync_MailboxAssignments_Fixed(t *testing.T) {
	searcher := &fakeSearcher{entries: []Entry{
		{DN: "cn=Admin,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{"mail": {"admin@corp.local"}}},
		{DN: "cn=Alice,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{"mail": {"alice@corp.local"}}},
	}}
	mboxSyncer := &fakeMailboxAssignmentSyncer{
		mailboxes: []repository.Mailbox{
			{EmailAddress: "sales@example.com", IsActive: true, ProvisionedBy: domain.ProvisionedByLDAP},
			{EmailAddress: "hr@example.com", IsActive: true, ProvisionedBy: domain.ProvisionedByLDAP},
			{EmailAddress: "manual-box@example.com", IsActive: true, ProvisionedBy: domain.ProvisionedByManual}, // fixed 対象外
			{EmailAddress: "inactive@example.com", IsActive: false, ProvisionedBy: domain.ProvisionedByLDAP},    // fixed 対象外
		},
	}
	cfg := testSyncConfig()
	cfg.MailboxResolution = &MailboxResolution{Roles: []RoleResolution{
		FixedRule(domain.AssignmentRoleApprover, []string{"Admin@corp.local"}), // 大文字小文字を無視して一致するべき
	}}
	s := NewSyncer(directory.NewProvisioner(&fakeProvisionerRepo{}), &fakeDeactivator{}, mboxSyncer, cfg)

	if _, err := s.Sync(context.Background(), searcher); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// alice（第1パス）→ admin（第2パス）の順で呼ばれる
	if len(mboxSyncer.calls) != 2 {
		t.Fatalf("呼び出し回数 = %d, want 2", len(mboxSyncer.calls))
	}
	if mboxSyncer.calls[0].userID != "u-alice@corp.local" {
		t.Errorf("第1パスは alice のはず: %q", mboxSyncer.calls[0].userID)
	}
	adminCall := mboxSyncer.calls[1]
	if adminCall.userID != "u-admin@corp.local" {
		t.Errorf("第2パスは admin のはず: %q", adminCall.userID)
	}
	if len(adminCall.desired) != 2 {
		t.Fatalf("admin の desired = %d 件, want 2（ldap かつ active のメールボックスのみ）: %+v", len(adminCall.desired), adminCall.desired)
	}
	got := map[string]bool{}
	for _, d := range adminCall.desired {
		if d.Role != domain.AssignmentRoleApprover {
			t.Errorf("role = %q, want admin", d.Role)
		}
		got[d.MailboxEmail] = true
	}
	if !got["sales@example.com"] || !got["hr@example.com"] {
		t.Errorf("desired に sales/hr が含まれるべき: %+v", adminCall.desired)
	}
}

// TestSyncer_Sync_MailboxAssignments_ChainSearchFailureSkipsUser は
// チェーンの search が失敗したユーザーについて、不完全なタプルでの reconcile による
// 誤削除を避けるため、そのユーザーのメールボックス反映をスキップすることを確認する。
func TestSyncer_Sync_MailboxAssignments_ChainSearchFailureSkipsUser(t *testing.T) {
	userEntries := []Entry{
		{DN: "cn=Alice,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{
			"mail": {"alice@corp.local"}, "memberOf": {"cn=sales,ou=Groups,dc=corp,dc=local"}}},
	}
	searcher := &fakeSearcher{searchFunc: func(baseDN, _ string, _ []string) ([]Entry, error) {
		if baseDN == "ou=Groups,dc=corp,dc=local" {
			return nil, errors.New("group search failed")
		}
		return userEntries, nil
	}}
	mboxSyncer := &fakeMailboxAssignmentSyncer{}
	cfg := testSyncConfig()
	rule, err := CompileChainRule(domain.AssignmentRoleOwner,
		[]map[string]any{
			{"self": "memberOf"},
			{"search": map[string]any{"base_dn": "ou=Groups,dc=corp,dc=local", "filter": "(member={value})"}},
			{"attr": "mail"},
			{"to_mailbox": map[string]any{}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MailboxResolution = &MailboxResolution{Roles: []RoleResolution{rule}}
	s := NewSyncer(directory.NewProvisioner(&fakeProvisionerRepo{}), &fakeDeactivator{}, mboxSyncer, cfg)

	result, err := s.Sync(context.Background(), searcher)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if len(mboxSyncer.calls) != 0 {
		t.Errorf("解決失敗時は reconcile を呼ぶべきではない（誤削除防止）: %d 回呼ばれた", len(mboxSyncer.calls))
	}
	if result.Synced != 1 {
		t.Errorf("ユーザー同期自体は続行されるべき: Synced = %d", result.Synced)
	}
	if len(result.Errors) == 0 {
		t.Error("解決失敗は Result.Errors に記録されるべき")
	}
}

// TestSyncer_Sync_MailboxAssignments_NilSyncerSkipped は mailboxSyncer が nil の場合、
// メールボックス割り当ての解決・呼び出しを一切行わないことを確認する。
func TestSyncer_Sync_MailboxAssignments_NilSyncerSkipped(t *testing.T) {
	searcher := &fakeSearcher{entries: []Entry{
		{DN: "cn=Alice,ou=Users,dc=corp,dc=local", Attributes: map[string][]string{"mail": {"alice@corp.local"}}},
	}}
	cfg := testSyncConfig()
	cfg.MailboxResolution = userAttrResolution(t, domain.AssignmentRoleMember)
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
