package mariadb

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

func newMockRepo(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock作成失敗: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &Repository{db: db}, mock
}

func TestBuildWhereClause_Empty(t *testing.T) {
	q := domain.ListQuery{}
	where, args := buildWhereClause(q)
	if where != "" {
		t.Errorf("期待: (空), 実際: %s", where)
	}
	if len(args) != 0 {
		t.Errorf("args 期待: 0件, 実際: %v", args)
	}
}

func TestBuildWhereClause_WithStatus(t *testing.T) {
	q := domain.ListQuery{Status: "delivered"}
	where, args := buildWhereClause(q)
	expected := "WHERE status = ?"
	if where != expected {
		t.Errorf("期待: %s, 実際: %s", expected, where)
	}
	if len(args) != 1 || args[0] != "delivered" {
		t.Errorf("args[0] 期待: delivered, 実際: %v", args)
	}
}

func TestBuildWhereClause_WithFrom(t *testing.T) {
	q := domain.ListQuery{From: "sender@example.com"}
	where, args := buildWhereClause(q)
	expected := "WHERE from_address LIKE ?"
	if where != expected {
		t.Errorf("期待: %s, 実際: %s", expected, where)
	}
	if len(args) != 1 || args[0] != "%sender@example.com%" {
		t.Errorf("args[0] 期待: %%sender@example.com%%, 実際: %v", args)
	}
}

func TestBuildWhereClause_MultipleConditions(t *testing.T) {
	q := domain.ListQuery{
		Status:  "quarantined",
		From:    "bad@",
		Subject: "virus",
	}
	where, args := buildWhereClause(q)
	if len(args) != 3 {
		t.Errorf("args 期待: 3件, 実際: %d件", len(args))
	}
	if args[0] != "quarantined" {
		t.Errorf("args[0] 期待: quarantined, 実際: %v", args[0])
	}
	_ = where
}

func TestBuildWhereClause_SinceAndUntil(t *testing.T) {
	since := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)
	q := domain.ListQuery{
		Since: &since,
		Until: &until,
	}
	where, args := buildWhereClause(q)
	_ = where
	if len(args) != 2 {
		t.Errorf("args 期待: 2件 (since + until), 実際: %d件", len(args))
	}
}

func TestBuildWhereClause_HasAttachmentTrue(t *testing.T) {
	b := true
	q := domain.ListQuery{HasAttachment: &b}
	where, _ := buildWhereClause(q)
	expected := "WHERE has_attachment = 1"
	if where != expected {
		t.Errorf("期待: %s, 実際: %s", expected, where)
	}
}

func TestBuildWhereClause_HasAttachmentFalse(t *testing.T) {
	b := false
	q := domain.ListQuery{HasAttachment: &b}
	where, _ := buildWhereClause(q)
	expected := "WHERE has_attachment = 0"
	if where != expected {
		t.Errorf("期待: %s, 実際: %s", expected, where)
	}
}

func TestSanitizeSort_AllowedColumns(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"received_at", "received_at"},
		{"subject", "subject"},
		{"from_address", "from_address"},
		{"size_bytes", "size_bytes"},
	}
	for _, c := range cases {
		got := sanitizeSort(c.input)
		if got != c.expected {
			t.Errorf("sanitizeSort(%q) 期待: %s, 実際: %s", c.input, c.expected, got)
		}
	}
}

func TestSanitizeSort_InvalidFallsToDefault(t *testing.T) {
	cases := []string{"invalid", "'; DROP TABLE--", "", "ID", "RECEIVED_AT"}
	for _, input := range cases {
		got := sanitizeSort(input)
		if got != "received_at" {
			t.Errorf("sanitizeSort(%q) 期待: received_at (デフォルト), 実際: %s", input, got)
		}
	}
}

func TestSanitizeOrder_Asc(t *testing.T) {
	got := sanitizeOrder("asc")
	if got != "ASC" {
		t.Errorf("sanitizeOrder(\"asc\") 期待: ASC, 実際: %s", got)
	}
}

func TestSanitizeOrder_Desc(t *testing.T) {
	got := sanitizeOrder("desc")
	if got != "DESC" {
		t.Errorf("sanitizeOrder(\"desc\") 期待: DESC, 実際: %s", got)
	}
}

func TestSanitizeOrder_InvalidFallsToDesc(t *testing.T) {
	cases := []string{"invalid", "", "DESC", "asc2"}
	for _, input := range cases {
		got := sanitizeOrder(input)
		if got != "DESC" {
			t.Errorf("sanitizeOrder(%q) 期待: DESC (デフォルト), 実際: %s", input, got)
		}
	}
}

func TestSanitizeOrder_CaseInsensitiveAsc(t *testing.T) {
	for _, input := range []string{"asc", "ASC", "Asc"} {
		got := sanitizeOrder(input)
		if got != "ASC" {
			t.Errorf("sanitizeOrder(%q) 期待: ASC, 実際: %s", input, got)
		}
	}
}

var messageColumns = []string{
	"id", "eml_path", "from_address", "to_addresses", "subject",
	"size_bytes", "has_attachment", "rspamd_score",
	"spf_result", "dkim_result", "dmarc_result",
	"status", "processed_eml_path", "received_at", "updated_at",
}

func toAddressesJSON(t *testing.T, addrs []string) []byte {
	t.Helper()
	b, err := json.Marshal(addrs)
	if err != nil {
		t.Fatalf("to_addresses JSON変換失敗: %v", err)
	}
	return b
}

func TestListMessages_WithRows(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	toJSON := toAddressesJSON(t, []string{"to@example.com"})

	mock.ExpectQuery("SELECT COUNT").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery("SELECT id").
		WillReturnRows(sqlmock.NewRows(messageColumns).AddRow(
			"msg-id-1", "/raw/msg.eml",
			"from@example.com", toJSON, "Hello",
			int64(1024), 0, float64(0),
			"pass", "pass", "pass",
			"delivered", nil, now, now,
		))

	q := domain.ListQuery{Page: 1, PerPage: 20}
	messages, total, err := repo.ListMessages(ctx, q)
	if err != nil {
		t.Fatalf("ListMessages 失敗: %v", err)
	}
	if total != 1 {
		t.Errorf("total 期待: 1, 実際: %d", total)
	}
	if len(messages) != 1 {
		t.Fatalf("messages 件数 期待: 1, 実際: %d", len(messages))
	}
	if messages[0].ID != "msg-id-1" {
		t.Errorf("messages[0].ID 期待: msg-id-1, 実際: %s", messages[0].ID)
	}
	if messages[0].FromAddress != "from@example.com" {
		t.Errorf("FromAddress 期待: from@example.com, 実際: %s", messages[0].FromAddress)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}

func TestListMessages_EmptyResult(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	mock.ExpectQuery("SELECT COUNT").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	mock.ExpectQuery("SELECT id").
		WillReturnRows(sqlmock.NewRows(messageColumns))

	q := domain.ListQuery{Page: 1, PerPage: 20}
	messages, total, err := repo.ListMessages(ctx, q)
	if err != nil {
		t.Fatalf("ListMessages 失敗: %v", err)
	}
	if total != 0 {
		t.Errorf("total 期待: 0, 実際: %d", total)
	}
	if len(messages) != 0 {
		t.Errorf("messages 件数 期待: 0, 実際: %d", len(messages))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}

func TestGetMessage_Found(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	toJSON := toAddressesJSON(t, []string{"to@example.com"})

	mock.ExpectQuery("SELECT id").
		WillReturnRows(sqlmock.NewRows(messageColumns).AddRow(
			"msg-id-2", "/raw/msg2.eml",
			"sender@example.com", toJSON, "Subject",
			int64(512), 1, float64(1.5),
			"pass", "fail", "none",
			"quarantined", nil, now, now,
		))

	mock.ExpectQuery("SELECT id, worker_name").
		WillReturnRows(sqlmock.NewRows([]string{"id", "worker_name", "score", "detected", "details", "created_at"}))

	detail, err := repo.GetMessage(ctx, "msg-id-2")
	if err != nil {
		t.Fatalf("GetMessage 失敗: %v", err)
	}
	if detail.ID != "msg-id-2" {
		t.Errorf("ID 期待: msg-id-2, 実際: %s", detail.ID)
	}
	if detail.EMLPath != "/raw/msg2.eml" {
		t.Errorf("EMLPath 期待: /raw/msg2.eml, 実際: %s", detail.EMLPath)
	}
	if detail.HasAttachment != true {
		t.Error("HasAttachment 期待: true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}

func TestGetMessage_NotFound(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	mock.ExpectQuery("SELECT id").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.GetMessage(ctx, "nonexistent-id")
	if err == nil {
		t.Error("存在しない ID の GetMessage はエラーを返すべき")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}

func TestUpdateMessageStatus_Success(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	mock.ExpectExec("UPDATE mail_messages SET status").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateMessageStatus(ctx, "msg-id-3", domain.StatusDelivered)
	if err != nil {
		t.Fatalf("UpdateMessageStatus 失敗: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}

func TestUpsertFederatedUser_NewUser(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	mock.ExpectExec("INSERT INTO users").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, email, display_name").
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "email", "display_name", "password_hash", "role", "is_active", "provisioned_by", "created_at", "updated_at"},
		).AddRow(
			"new-user-id", "new@example.com", "New User", "", "operator", 1, "oidc", now, now,
		))

	u, err := repo.UpsertFederatedUser(ctx, "new@example.com", "New User", domain.RoleOperator, domain.ProvisionedByOIDC)
	if err != nil {
		t.Fatalf("UpsertFederatedUser 失敗: %v", err)
	}
	if u.ID != "new-user-id" || u.Role != domain.RoleOperator || u.ProvisionedBy != domain.ProvisionedByOIDC {
		t.Errorf("結果が期待と異なる: %+v", u)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}

func TestUpsertFederatedUser_ManualRolePreserved(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	mock.ExpectExec("INSERT INTO users").
		WillReturnResult(sqlmock.NewResult(0, 1))
	// SQL の ON DUPLICATE KEY UPDATE ロジック（IF(provisioned_by='manual', role, VALUES(role))）が
	// 実際に適用された結果として、DB は manual のまま admin を返す想定。
	mock.ExpectQuery("SELECT id, email, display_name").
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "email", "display_name", "password_hash", "role", "is_active", "provisioned_by", "created_at", "updated_at"},
		).AddRow(
			"existing-admin-id", "admin@example.com", "Admin", "$2a$hash", "admin", 1, "manual", now, now,
		))

	u, err := repo.UpsertFederatedUser(ctx, "admin@example.com", "Admin (IdP name)", domain.RoleViewer, domain.ProvisionedByOIDC)
	if err != nil {
		t.Fatalf("UpsertFederatedUser 失敗: %v", err)
	}
	if u.Role != domain.RoleAdmin {
		t.Errorf("role = %q, want admin（manual ユーザーは role を保持するべき）", u.Role)
	}
	if u.ProvisionedBy != domain.ProvisionedByManual {
		t.Errorf("provisioned_by = %q, want manual", u.ProvisionedBy)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}

func TestUpsertFederatedUser_LDAPRoleProtectedFromOIDC(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	mock.ExpectExec("INSERT INTO users").
		WillReturnResult(sqlmock.NewResult(0, 1))
	// provisioned_by=ldap の行に対する oidc ログインは role を上書きしない
	// （SQL の CASE 分岐: provisioned_by IN ('ldap','scim') AND VALUES(provisioned_by)='oidc' → 既存値保持）。
	mock.ExpectQuery("SELECT id, email, display_name").
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "email", "display_name", "password_hash", "role", "is_active", "provisioned_by", "created_at", "updated_at"},
		).AddRow(
			"ldap-user-id", "ldapuser@example.com", "LDAP User", "", "operator", 1, "ldap", now, now,
		))

	u, err := repo.UpsertFederatedUser(ctx, "ldapuser@example.com", "LDAP User (IdP name)", domain.RoleViewer, domain.ProvisionedByOIDC)
	if err != nil {
		t.Fatalf("UpsertFederatedUser 失敗: %v", err)
	}
	if u.Role != domain.RoleOperator {
		t.Errorf("role = %q, want operator（ldap 同期済みユーザーの role は oidc から保護されるべき）", u.Role)
	}
	if u.ProvisionedBy != domain.ProvisionedByLDAP {
		t.Errorf("provisioned_by = %q, want ldap", u.ProvisionedBy)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}

func TestUpsertFederatedUser_LDAPCanUpdateOwnSync(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	mock.ExpectExec("INSERT INTO users").
		WillReturnResult(sqlmock.NewResult(0, 1))
	// provisioned_by=ldap の行に対する ldap 側の再同期は role を更新してよい（同格の権威）。
	mock.ExpectQuery("SELECT id, email, display_name").
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "email", "display_name", "password_hash", "role", "is_active", "provisioned_by", "created_at", "updated_at"},
		).AddRow(
			"ldap-user-id", "ldapuser@example.com", "LDAP User", "", "admin", 1, "ldap", now, now,
		))

	u, err := repo.UpsertFederatedUser(ctx, "ldapuser@example.com", "LDAP User", domain.RoleAdmin, domain.ProvisionedByLDAP)
	if err != nil {
		t.Fatalf("UpsertFederatedUser 失敗: %v", err)
	}
	if u.Role != domain.RoleAdmin {
		t.Errorf("role = %q, want admin（ldap 自身による再同期は role を更新できるべき）", u.Role)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}

func TestDeactivateMissingLDAPUsers_Success(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	mock.ExpectExec("UPDATE users SET is_active = 0").
		WillReturnResult(sqlmock.NewResult(0, 2))

	n, err := repo.DeactivateMissingLDAPUsers(ctx, []string{"alice@corp.local", "bob@corp.local"})
	if err != nil {
		t.Fatalf("DeactivateMissingLDAPUsers 失敗: %v", err)
	}
	if n != 2 {
		t.Errorf("n = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}

func TestDeactivateMissingLDAPUsers_EmptyListNoop(t *testing.T) {
	repo, _ := newMockRepo(t)
	ctx := context.Background()

	// presentEmails が空の場合、SQL を実行せずに 0 を返す
	// （LDAP 検索の誤検知で全 ldap ユーザーを無効化することを防ぐ最後の防衛線）。
	n, err := repo.DeactivateMissingLDAPUsers(ctx, nil)
	if err != nil {
		t.Fatalf("DeactivateMissingLDAPUsers 失敗: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

func TestSyncMailboxAssignmentsForUser_CreatesNewMailboxAndAssignment(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO mailboxes").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id FROM mailboxes").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("mb-1"))
	mock.ExpectExec("INSERT INTO mailbox_assignments").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT mailbox_id, role FROM mailbox_assignments").
		WillReturnRows(sqlmock.NewRows([]string{"mailbox_id", "role"}))
	mock.ExpectCommit()

	err := repo.SyncMailboxAssignmentsForUser(ctx, "user-1", domain.ProvisionedByLDAP, []repository.MailboxAssignmentRequest{
		{MailboxEmail: "sales@example.com", MailboxDisplayName: "Sales", Role: domain.AssignmentRoleMember},
	})
	if err != nil {
		t.Fatalf("SyncMailboxAssignmentsForUser 失敗: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}

func TestSyncMailboxAssignmentsForUser_RemovesStaleAssignment(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	mock.ExpectBegin()
	// desired が空なので mailbox upsert ループは実行されない
	mock.ExpectQuery("SELECT mailbox_id, role FROM mailbox_assignments").
		WillReturnRows(sqlmock.NewRows([]string{"mailbox_id", "role"}).AddRow("mb-1", "member"))
	mock.ExpectExec("DELETE FROM mailbox_assignments").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.SyncMailboxAssignmentsForUser(ctx, "user-1", domain.ProvisionedByLDAP, nil)
	if err != nil {
		t.Fatalf("SyncMailboxAssignmentsForUser 失敗: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}

func TestSyncMailboxAssignmentsForUser_KeepsPresentAssignment(t *testing.T) {
	repo, mock := newMockRepo(t)
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO mailboxes").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id FROM mailboxes").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("mb-1"))
	mock.ExpectExec("INSERT INTO mailbox_assignments").WillReturnResult(sqlmock.NewResult(0, 1))
	// 既存の割り当てが desired と一致するので DELETE は呼ばれない
	mock.ExpectQuery("SELECT mailbox_id, role FROM mailbox_assignments").
		WillReturnRows(sqlmock.NewRows([]string{"mailbox_id", "role"}).AddRow("mb-1", "member"))
	mock.ExpectCommit()

	err := repo.SyncMailboxAssignmentsForUser(ctx, "user-1", domain.ProvisionedByLDAP, []repository.MailboxAssignmentRequest{
		{MailboxEmail: "sales@example.com", Role: domain.AssignmentRoleMember},
	})
	if err != nil {
		t.Fatalf("SyncMailboxAssignmentsForUser 失敗: %v", err)
	}
	// DELETE が期待されていないので、もし実行されていれば ExpectationsWereMet がエラーにならない
	// (sqlmock は未定義のクエリが実行されるとその時点でエラーを返す)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("未消化の期待値あり: %v", err)
	}
}
