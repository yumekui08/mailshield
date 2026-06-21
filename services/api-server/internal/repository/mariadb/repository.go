// Package mariadb は Repository インターフェースの MariaDB 実装を提供する。
package mariadb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql" // MariaDB/MySQL ドライバー

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// Config はDB接続プールの設定を保持する。
type Config struct {
	MaxOpenConns           int
	MaxIdleConns           int
	ConnMaxLifetimeMinutes int
}

// Repository は MariaDB を使った Repository 実装である。
type Repository struct {
	db *sql.DB
}

// New はMariaDB接続を確立してRepositoryを返す。
func New(dsn string, cfg Config) (*Repository, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("MariaDB 接続オープン失敗: %w", err)
	}

	maxOpen := cfg.MaxOpenConns
	if maxOpen == 0 {
		maxOpen = 10
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle == 0 {
		maxIdle = 5
	}
	maxLifetime := cfg.ConnMaxLifetimeMinutes
	if maxLifetime == 0 {
		maxLifetime = 5
	}

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(time.Duration(maxLifetime) * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("MariaDB 疎通確認失敗: %w", err)
	}

	return &Repository{db: db}, nil
}

// Close はDB接続を閉じる。
func (r *Repository) Close() error {
	return r.db.Close()
}

// DB は生の *sql.DB を返す。セッション/OTP ストア等の MariaDB 実装に渡すために使用する。
func (r *Repository) DB() *sql.DB {
	return r.db
}

// ListMessages はクエリパラメータに従ってメッセージ一覧を返す。
func (r *Repository) ListMessages(ctx context.Context, q domain.ListQuery) ([]domain.Message, int, error) {
	where, args := buildWhereClause(q)

	// 総件数取得
	countQuery := "SELECT COUNT(*) FROM mail_messages " + where
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("メッセージ件数取得失敗: %w", err)
	}

	// データ取得
	sort := sanitizeSort(q.Sort)
	order := sanitizeOrder(q.Order)
	offset := (q.Page - 1) * q.PerPage

	dataQuery := fmt.Sprintf(`
		SELECT id, eml_path, from_address, to_addresses, subject,
		       size_bytes, has_attachment, rspamd_score,
		       spf_result, dkim_result, dmarc_result,
		       status, processed_eml_path, received_at, updated_at
		FROM mail_messages
		%s
		ORDER BY %s %s
		LIMIT ? OFFSET ?`, where, sort, order)

	rows, err := r.db.QueryContext(ctx, dataQuery, append(args, q.PerPage, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("メッセージ一覧取得失敗: %w", err)
	}
	defer rows.Close()

	messages, err := scanMessages(rows)
	if err != nil {
		return nil, 0, err
	}

	return messages, total, nil
}

// GetMessage はメッセージの詳細情報（検査結果を含む）を返す。
func (r *Repository) GetMessage(ctx context.Context, id string) (*domain.MessageDetail, error) {
	msg, err := r.getMessageByID(ctx, id)
	if err != nil {
		return nil, err
	}

	results, err := r.getInspectResults(ctx, id)
	if err != nil {
		return nil, err
	}

	return &domain.MessageDetail{
		Message:        *msg,
		InspectResults: results,
	}, nil
}

// ListQuarantine はstatus=quarantined固定でメッセージ一覧を返す。
func (r *Repository) ListQuarantine(ctx context.Context, q domain.ListQuery) ([]domain.Message, int, error) {
	q.Status = string(domain.StatusQuarantined)
	return r.ListMessages(ctx, q)
}

// GetQuarantine は隔離メッセージの詳細情報を返す。
// statusがquarantinedでない場合はエラーを返す。
func (r *Repository) GetQuarantine(ctx context.Context, id string) (*domain.MessageDetail, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, eml_path, from_address, to_addresses, subject,
		       size_bytes, has_attachment, rspamd_score,
		       spf_result, dkim_result, dmarc_result,
		       status, processed_eml_path, received_at, updated_at
		FROM mail_messages
		WHERE id = ? AND status = ?`,
		id, string(domain.StatusQuarantined),
	)

	msg, err := scanMessage(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("隔離メッセージが見つかりません (id=%s)", id)
		}
		return nil, fmt.Errorf("隔離メッセージ取得失敗 (id=%s): %w", id, err)
	}

	results, err := r.getInspectResults(ctx, id)
	if err != nil {
		return nil, err
	}

	return &domain.MessageDetail{
		Message:        *msg,
		InspectResults: results,
	}, nil
}

// UpdateMessageStatus はメッセージの処理状態を更新する。
func (r *Repository) UpdateMessageStatus(ctx context.Context, id string, status domain.MessageStatus) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE mail_messages SET status = ? WHERE id = ?`,
		string(status), id,
	)
	if err != nil {
		return fmt.Errorf("status 更新失敗 (id=%s): %w", id, err)
	}
	return nil
}

// BulkUpdateMessageStatus は複数メッセージの処理状態を一括更新する。
// 対象は status=quarantined のメッセージのみ（それ以外はスキップされる）。
func (r *Repository) BulkUpdateMessageStatus(ctx context.Context, ids []string, status domain.MessageStatus) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(ids)+1)
	args = append(args, string(status))
	for _, id := range ids {
		args = append(args, id)
	}
	query := fmt.Sprintf(
		`UPDATE mail_messages SET status = ? WHERE id IN (%s) AND status = 'quarantined'`,
		placeholders,
	)
	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("一括 status 更新失敗: %w", err)
	}
	return nil
}

// getMessageByID はIDでメッセージを取得する内部ヘルパー。
func (r *Repository) getMessageByID(ctx context.Context, id string) (*domain.Message, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, eml_path, from_address, to_addresses, subject,
		       size_bytes, has_attachment, rspamd_score,
		       spf_result, dkim_result, dmarc_result,
		       status, processed_eml_path, received_at, updated_at
		FROM mail_messages
		WHERE id = ?`,
		id,
	)

	msg, err := scanMessage(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("メッセージが見つかりません (id=%s)", id)
		}
		return nil, fmt.Errorf("メッセージ取得失敗 (id=%s): %w", id, err)
	}

	return msg, nil
}

// getInspectResults はメッセージの検査結果一覧を取得する内部ヘルパー。
func (r *Repository) getInspectResults(ctx context.Context, messageID string) ([]domain.InspectResult, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, worker_name, score, detected, details, created_at
		FROM inspect_results
		WHERE message_id = ?
		ORDER BY created_at ASC`,
		messageID,
	)
	if err != nil {
		return nil, fmt.Errorf("検査結果取得失敗 (message_id=%s): %w", messageID, err)
	}
	defer rows.Close()

	var results []domain.InspectResult
	for rows.Next() {
		var ir domain.InspectResult
		var detectedInt int
		var detailsJSON []byte

		if err := rows.Scan(
			&ir.ID,
			&ir.WorkerName,
			&ir.Score,
			&detectedInt,
			&detailsJSON,
			&ir.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("検査結果スキャン失敗: %w", err)
		}

		ir.Detected = detectedInt == 1

		if err := json.Unmarshal(detailsJSON, &ir.Details); err != nil {
			ir.Details = map[string]any{}
		}

		results = append(results, ir)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("検査結果イテレーション失敗: %w", err)
	}

	if results == nil {
		results = []domain.InspectResult{}
	}

	return results, nil
}

// buildWhereClause はListQueryからWHERE句と引数を構築する。
func buildWhereClause(q domain.ListQuery) (string, []any) {
	var conditions []string
	var args []any

	if q.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, q.Status)
	}

	if q.From != "" {
		conditions = append(conditions, "from_address LIKE ?")
		args = append(args, "%"+q.From+"%")
	}

	if q.To != "" {
		conditions = append(conditions, "to_addresses LIKE ?")
		args = append(args, "%"+q.To+"%")
	}

	if q.Subject != "" {
		conditions = append(conditions, "subject LIKE ?")
		args = append(args, "%"+q.Subject+"%")
	}

	if q.Since != nil {
		conditions = append(conditions, "received_at >= ?")
		args = append(args, q.Since.UTC())
	}

	if q.Until != nil {
		conditions = append(conditions, "received_at <= ?")
		args = append(args, q.Until.UTC())
	}

	if q.HasAttachment != nil {
		if *q.HasAttachment {
			conditions = append(conditions, "has_attachment = 1")
		} else {
			conditions = append(conditions, "has_attachment = 0")
		}
	}

	// viewer 向けメールボックス可視性フィルター
	if q.VisibilityFilter != nil {
		clause, visArgs := buildVisibilityClause(q.VisibilityFilter)
		// clause は " AND (...)" または " AND 1 = 0" の形式
		// WHERE 句の conditions に組み込むため先頭の " AND " を除去してアンラップする
		if strings.HasPrefix(clause, " AND (") {
			conditions = append(conditions, clause[5:]) // " AND " を除いた "(...)" 部分
			args = append(args, visArgs...)
		} else {
			conditions = append(conditions, "1 = 0")
		}
	}

	if len(conditions) == 0 {
		return "", args
	}

	return "WHERE " + strings.Join(conditions, " AND "), args
}

// sanitizeSort は許可されたソートカラムのみを受け付ける。
func sanitizeSort(sort string) string {
	allowed := map[string]string{
		"received_at":  "received_at",
		"subject":      "subject",
		"from_address": "from_address",
		"size_bytes":   "size_bytes",
	}
	if v, ok := allowed[sort]; ok {
		return v
	}
	return "received_at"
}

// sanitizeOrder は許可されたソート順のみを受け付ける。
func sanitizeOrder(order string) string {
	if strings.ToLower(order) == "asc" {
		return "ASC"
	}
	return "DESC"
}

// scanMessage は*sql.Rowから1件のMessageをスキャンする。
func scanMessage(row *sql.Row) (*domain.Message, error) {
	var msg domain.Message
	var hasAttachmentInt int
	var toAddressesJSON []byte

	err := row.Scan(
		&msg.ID,
		&msg.EMLPath,
		&msg.FromAddress,
		&toAddressesJSON,
		&msg.Subject,
		&msg.SizeBytes,
		&hasAttachmentInt,
		&msg.RspamdScore,
		&msg.SPFResult,
		&msg.DKIMResult,
		&msg.DMARCResult,
		&msg.Status,
		&msg.ProcessedEMLPath,
		&msg.ReceivedAt,
		&msg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	msg.HasAttachment = hasAttachmentInt == 1

	if err := json.Unmarshal(toAddressesJSON, &msg.ToAddresses); err != nil {
		msg.ToAddresses = []string{}
	}

	return &msg, nil
}

// scanMessages は*sql.Rowsから複数のMessageをスキャンする。
func scanMessages(rows *sql.Rows) ([]domain.Message, error) {
	var messages []domain.Message

	for rows.Next() {
		var msg domain.Message
		var hasAttachmentInt int
		var toAddressesJSON []byte

		if err := rows.Scan(
			&msg.ID,
			&msg.EMLPath,
			&msg.FromAddress,
			&toAddressesJSON,
			&msg.Subject,
			&msg.SizeBytes,
			&hasAttachmentInt,
			&msg.RspamdScore,
			&msg.SPFResult,
			&msg.DKIMResult,
			&msg.DMARCResult,
			&msg.Status,
			&msg.ProcessedEMLPath,
			&msg.ReceivedAt,
			&msg.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("メッセージスキャン失敗: %w", err)
		}

		msg.HasAttachment = hasAttachmentInt == 1

		if err := json.Unmarshal(toAddressesJSON, &msg.ToAddresses); err != nil {
			msg.ToAddresses = []string{}
		}

		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("メッセージイテレーション失敗: %w", err)
	}

	if messages == nil {
		messages = []domain.Message{}
	}

	return messages, nil
}

// FindUserByEmail はメールアドレスでユーザーを検索する。
func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*repository.User, error) {
	const q = `
		SELECT id, email, display_name, password_hash, role, is_active, created_at, updated_at
		FROM users
		WHERE email = ? AND is_active = 1
		LIMIT 1`
	row := r.db.QueryRowContext(ctx, q, email)
	return scanUser(row)
}

// CreateUser はユーザーを登録する。
func (r *Repository) CreateUser(ctx context.Context, u *repository.User) error {
	const q = `
		INSERT INTO users (id, email, display_name, password_hash, role, is_active)
		VALUES (?, ?, ?, ?, ?, 1)`
	_, err := r.db.ExecContext(ctx, q,
		u.ID, u.Email, u.DisplayName, u.PasswordHash, string(u.Role),
	)
	if err != nil {
		return fmt.Errorf("ユーザー作成失敗 (email=%s): %w", u.Email, err)
	}
	return nil
}

// CountUsers はユーザー数を返す。
func (r *Repository) CountUsers(ctx context.Context) (int, error) {
	const q = `SELECT COUNT(*) FROM users WHERE is_active = 1`
	var count int
	if err := r.db.QueryRowContext(ctx, q).Scan(&count); err != nil {
		return 0, fmt.Errorf("ユーザー数取得失敗: %w", err)
	}
	return count, nil
}

// ListUsers はユーザー一覧を返す。
func (r *Repository) ListUsers(ctx context.Context) ([]repository.User, error) {
	const q = `
		SELECT id, email, display_name, password_hash, role, is_active, created_at, updated_at
		FROM users
		WHERE is_active = 1
		ORDER BY created_at ASC`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("ユーザー一覧取得失敗: %w", err)
	}
	defer rows.Close()

	var users []repository.User
	for rows.Next() {
		var u repository.User
		var isActiveInt int
		if err := rows.Scan(
			&u.ID, &u.Email, &u.DisplayName,
			&u.PasswordHash, &u.Role, &isActiveInt, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("ユーザースキャン失敗: %w", err)
		}
		u.IsActive = isActiveInt == 1
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ユーザーイテレーション失敗: %w", err)
	}
	if users == nil {
		users = []repository.User{}
	}
	return users, nil
}

// UpdateUserPassword はユーザーのパスワードハッシュを更新する。
func (r *Repository) UpdateUserPassword(ctx context.Context, userID, passwordHash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ? WHERE id = ?`,
		passwordHash, userID,
	)
	if err != nil {
		return fmt.Errorf("パスワード更新失敗 (user_id=%s): %w", userID, err)
	}
	return nil
}

// UpdateUserRole はユーザーのロールを更新する。
func (r *Repository) UpdateUserRole(ctx context.Context, userID string, role domain.Role) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET role = ? WHERE id = ?`,
		string(role), userID,
	)
	if err != nil {
		return fmt.Errorf("ロール更新失敗 (user_id=%s): %w", userID, err)
	}
	return nil
}

// DeleteUser はユーザーを論理削除する（is_active = 0）。
func (r *Repository) DeleteUser(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET is_active = 0 WHERE id = ?`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("ユーザー削除失敗 (user_id=%s): %w", userID, err)
	}
	return nil
}

// CreateMailbox はメールボックスを登録する。
func (r *Repository) CreateMailbox(ctx context.Context, m *repository.Mailbox) error {
	const q = `
		INSERT INTO mailboxes (id, email_address, display_name, is_active)
		VALUES (?, ?, ?, 1)`
	_, err := r.db.ExecContext(ctx, q, m.ID, m.EmailAddress, m.DisplayName)
	if err != nil {
		return fmt.Errorf("メールボックス作成失敗 (email=%s): %w", m.EmailAddress, err)
	}
	return nil
}

// ListMailboxes はメールボックス一覧を返す。
func (r *Repository) ListMailboxes(ctx context.Context) ([]repository.Mailbox, error) {
	const q = `
		SELECT id, email_address, display_name, is_active, created_at, updated_at
		FROM mailboxes
		ORDER BY email_address ASC`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("メールボックス一覧取得失敗: %w", err)
	}
	defer rows.Close()

	var mailboxes []repository.Mailbox
	for rows.Next() {
		var m repository.Mailbox
		var isActiveInt int
		if err := rows.Scan(
			&m.ID, &m.EmailAddress, &m.DisplayName,
			&isActiveInt, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("メールボックススキャン失敗: %w", err)
		}
		m.IsActive = isActiveInt == 1
		mailboxes = append(mailboxes, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("メールボックスイテレーション失敗: %w", err)
	}
	if mailboxes == nil {
		mailboxes = []repository.Mailbox{}
	}
	return mailboxes, nil
}

// GetMailbox は指定メールボックスを返す。見つからない場合は nil, nil を返す。
func (r *Repository) GetMailbox(ctx context.Context, id string) (*repository.Mailbox, error) {
	const q = `
		SELECT id, email_address, display_name, is_active, created_at, updated_at
		FROM mailboxes
		WHERE id = ?`
	row := r.db.QueryRowContext(ctx, q, id)
	var m repository.Mailbox
	var isActiveInt int
	if err := row.Scan(
		&m.ID, &m.EmailAddress, &m.DisplayName,
		&isActiveInt, &m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("メールボックス取得失敗 (id=%s): %w", id, err)
	}
	m.IsActive = isActiveInt == 1
	return &m, nil
}

// UpdateMailbox はメールボックスの表示名と有効フラグを更新する。
func (r *Repository) UpdateMailbox(ctx context.Context, id, displayName string, isActive bool) error {
	isActiveInt := 0
	if isActive {
		isActiveInt = 1
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE mailboxes SET display_name = ?, is_active = ? WHERE id = ?`,
		displayName, isActiveInt, id,
	)
	if err != nil {
		return fmt.Errorf("メールボックス更新失敗 (id=%s): %w", id, err)
	}
	return nil
}

// DeleteMailbox はメールボックスを削除する（割り当ては CASCADE 削除される）。
func (r *Repository) DeleteMailbox(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM mailboxes WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("メールボックス削除失敗 (id=%s): %w", id, err)
	}
	return nil
}

// ListAssignments はメールボックスの割り当て一覧を返す（ユーザー情報も JOIN）。
func (r *Repository) ListAssignments(ctx context.Context, mailboxID string) ([]repository.MailboxAssignment, error) {
	const q = `
		SELECT ma.id, ma.mailbox_id, ma.user_id, ma.role, ma.created_at,
		       u.email, u.display_name
		FROM mailbox_assignments ma
		JOIN users u ON u.id = ma.user_id
		WHERE ma.mailbox_id = ?
		ORDER BY ma.role ASC, u.email ASC`
	rows, err := r.db.QueryContext(ctx, q, mailboxID)
	if err != nil {
		return nil, fmt.Errorf("割り当て一覧取得失敗 (mailbox_id=%s): %w", mailboxID, err)
	}
	defer rows.Close()

	var assignments []repository.MailboxAssignment
	for rows.Next() {
		var a repository.MailboxAssignment
		var role string
		if err := rows.Scan(
			&a.ID, &a.MailboxID, &a.UserID, &role, &a.CreatedAt,
			&a.UserEmail, &a.UserDisplayName,
		); err != nil {
			return nil, fmt.Errorf("割り当てスキャン失敗: %w", err)
		}
		a.Role = domain.AssignmentRole(role)
		assignments = append(assignments, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("割り当てイテレーション失敗: %w", err)
	}
	if assignments == nil {
		assignments = []repository.MailboxAssignment{}
	}
	return assignments, nil
}

// AddAssignment はメールボックスにユーザーを割り当てる。重複は無視する。
func (r *Repository) AddAssignment(ctx context.Context, a *repository.MailboxAssignment) error {
	const q = `
		INSERT IGNORE INTO mailbox_assignments (id, mailbox_id, user_id, role)
		VALUES (?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q, a.ID, a.MailboxID, a.UserID, string(a.Role))
	if err != nil {
		return fmt.Errorf("割り当て追加失敗 (mailbox_id=%s, user_id=%s): %w", a.MailboxID, a.UserID, err)
	}
	return nil
}

// RemoveAssignment はメールボックスからユーザーの割り当てを削除する。
func (r *Repository) RemoveAssignment(ctx context.Context, mailboxID, userID string, role domain.AssignmentRole) error {
	const q = `
		DELETE FROM mailbox_assignments
		WHERE mailbox_id = ? AND user_id = ? AND role = ?`
	_, err := r.db.ExecContext(ctx, q, mailboxID, userID, string(role))
	if err != nil {
		return fmt.Errorf("割り当て削除失敗 (mailbox_id=%s, user_id=%s, role=%s): %w", mailboxID, userID, role, err)
	}
	return nil
}

// GetMailboxAddressesForUser は指定ロールを持つユーザーのメールボックスアドレス一覧を返す。
func (r *Repository) GetMailboxAddressesForUser(ctx context.Context, userID string, roles []domain.AssignmentRole) ([]string, error) {
	if len(roles) == 0 {
		return []string{}, nil
	}

	placeholders := strings.Repeat("?,", len(roles))
	placeholders = placeholders[:len(placeholders)-1]

	q := fmt.Sprintf(`
		SELECT DISTINCT mb.email_address
		FROM mailboxes mb
		JOIN mailbox_assignments ma ON ma.mailbox_id = mb.id
		WHERE ma.user_id = ? AND ma.role IN (%s) AND mb.is_active = 1`,
		placeholders,
	)

	args := []any{userID}
	for _, role := range roles {
		args = append(args, string(role))
	}

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("メールボックスアドレス取得失敗 (user_id=%s): %w", userID, err)
	}
	defer rows.Close()

	var addresses []string
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			return nil, fmt.Errorf("アドレススキャン失敗: %w", err)
		}
		addresses = append(addresses, addr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("アドレスイテレーション失敗: %w", err)
	}
	if addresses == nil {
		addresses = []string{}
	}
	return addresses, nil
}

// GetStats はダッシュボード用の集計統計を返す。
func (r *Repository) GetStats(ctx context.Context, filter *domain.MailboxVisibilityFilter) (*domain.Stats, error) {
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	weekStart := todayStart.AddDate(0, 0, -6)

	today, err := r.getStatsPeriod(ctx, todayStart, filter)
	if err != nil {
		return nil, fmt.Errorf("当日統計取得失敗: %w", err)
	}
	week, err := r.getStatsPeriod(ctx, weekStart, filter)
	if err != nil {
		return nil, fmt.Errorf("週間統計取得失敗: %w", err)
	}
	return &domain.Stats{Today: *today, Week: *week}, nil
}

func (r *Repository) getStatsPeriod(ctx context.Context, since time.Time, filter *domain.MailboxVisibilityFilter) (*domain.StatsPeriod, error) {
	baseArgs := []any{since.UTC()}
	visClause, visArgs := buildVisibilityClause(filter)

	q := "SELECT status, COUNT(*) FROM mail_messages WHERE received_at >= ?" + visClause + " GROUP BY status"
	args := append(baseArgs, visArgs...)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("統計クエリ失敗: %w", err)
	}
	defer rows.Close()

	period := &domain.StatsPeriod{}
	for rows.Next() {
		var status domain.MessageStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("統計スキャン失敗: %w", err)
		}
		switch status {
		case domain.StatusDelivered:
			period.Delivered = count
		case domain.StatusQuarantined:
			period.Quarantined = count
		case domain.StatusRejected:
			period.Rejected = count
		}
		period.Total += count
	}
	return period, rows.Err()
}

// buildVisibilityClause は可視性フィルターを SQL 条件句と引数に変換する。
// buildWhereClause と同じロジックだが単独の WHERE 追加句として返す。
func buildVisibilityClause(filter *domain.MailboxVisibilityFilter) (string, []any) {
	if filter == nil {
		return "", nil
	}
	var visConditions []string
	var args []any

	if len(filter.InboundMailboxes) > 0 {
		jsonArr, _ := json.Marshal(filter.InboundMailboxes)
		visConditions = append(visConditions, "JSON_OVERLAPS(to_addresses, ?) = 1")
		args = append(args, string(jsonArr))
	}
	if len(filter.OutboundMailboxes) > 0 {
		placeholders := strings.Repeat("?,", len(filter.OutboundMailboxes))
		placeholders = placeholders[:len(placeholders)-1]
		visConditions = append(visConditions, fmt.Sprintf("from_address IN (%s)", placeholders))
		for _, addr := range filter.OutboundMailboxes {
			args = append(args, addr)
		}
	}
	if len(visConditions) > 0 {
		return " AND (" + strings.Join(visConditions, " OR ") + ")", args
	}
	return " AND 1 = 0", nil
}

// ── 添付ファイル ─────────────────────────────────────────────

// ListAttachmentsByMessage はメッセージに紐づく添付ファイル一覧を返す（削除済み除く）。
func (r *Repository) ListAttachmentsByMessage(ctx context.Context, messageID string) ([]domain.Attachment, error) {
	const q = `
		SELECT id, message_id, download_token, filename, content_type,
		       size_bytes, storage_backend, storage_path, is_disabled, download_mode, created_at
		FROM mail_attachments
		WHERE message_id = ? AND deleted_at IS NULL
		ORDER BY filename`

	rows, err := r.db.QueryContext(ctx, q, messageID)
	if err != nil {
		return nil, fmt.Errorf("添付ファイル一覧取得失敗: %w", err)
	}
	defer rows.Close()

	var result []domain.Attachment
	for rows.Next() {
		var att domain.Attachment
		var isDisabledInt int
		if err := rows.Scan(
			&att.ID, &att.MessageID, &att.DownloadToken,
			&att.Filename, &att.ContentType, &att.SizeBytes,
			&att.StorageBackend, &att.StoragePath, &isDisabledInt, &att.DownloadMode, &att.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("添付ファイルスキャン失敗: %w", err)
		}
		att.IsDisabled = isDisabledInt == 1
		result = append(result, att)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("添付ファイルイテレーション失敗: %w", err)
	}
	if result == nil {
		result = []domain.Attachment{}
	}
	return result, nil
}

// ListAttachmentsByToken は download_token に紐づく添付ファイル一覧を返す（削除済み除く）。
func (r *Repository) ListAttachmentsByToken(ctx context.Context, downloadToken string) ([]domain.Attachment, error) {
	const q = `
		SELECT id, message_id, download_token, filename, content_type,
		       size_bytes, storage_backend, storage_path, is_disabled, download_mode, created_at
		FROM mail_attachments
		WHERE download_token = ? AND deleted_at IS NULL
		ORDER BY filename`

	rows, err := r.db.QueryContext(ctx, q, downloadToken)
	if err != nil {
		return nil, fmt.Errorf("添付ファイル一覧取得失敗: %w", err)
	}
	defer rows.Close()

	var result []domain.Attachment
	for rows.Next() {
		var att domain.Attachment
		var isDisabledInt int
		if err := rows.Scan(
			&att.ID, &att.MessageID, &att.DownloadToken,
			&att.Filename, &att.ContentType, &att.SizeBytes,
			&att.StorageBackend, &att.StoragePath, &isDisabledInt, &att.DownloadMode, &att.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("添付ファイルスキャン失敗: %w", err)
		}
		att.IsDisabled = isDisabledInt == 1
		result = append(result, att)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("添付ファイルイテレーション失敗: %w", err)
	}
	if result == nil {
		result = []domain.Attachment{}
	}
	return result, nil
}

// GetAttachmentByToken は download_token とファイル名で添付ファイルを取得する。
func (r *Repository) GetAttachmentByToken(ctx context.Context, downloadToken, filename string) (*domain.Attachment, error) {
	const q = `
		SELECT id, message_id, download_token, filename, content_type,
		       size_bytes, storage_backend, storage_path, is_disabled, download_mode, created_at
		FROM mail_attachments
		WHERE download_token = ? AND filename = ? AND deleted_at IS NULL
		LIMIT 1`

	row := r.db.QueryRowContext(ctx, q, downloadToken, filename)
	var att domain.Attachment
	var isDisabledInt int
	if err := row.Scan(
		&att.ID, &att.MessageID, &att.DownloadToken,
		&att.Filename, &att.ContentType, &att.SizeBytes,
		&att.StorageBackend, &att.StoragePath, &isDisabledInt, &att.DownloadMode, &att.CreatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("添付ファイル取得失敗: %w", err)
	}
	att.IsDisabled = isDisabledInt == 1
	return &att, nil
}

// ListAttachmentsByTokenPublic は download_token のみで添付ファイル一覧を返す（認証不要）。
func (r *Repository) ListAttachmentsByTokenPublic(ctx context.Context, downloadToken string) ([]domain.Attachment, error) {
	return r.ListAttachmentsByToken(ctx, downloadToken)
}

// GetAttachmentByTokenPublic は download_token とファイル名で添付ファイルを取得する（認証不要）。
func (r *Repository) GetAttachmentByTokenPublic(ctx context.Context, downloadToken, filename string) (*domain.Attachment, error) {
	return r.GetAttachmentByToken(ctx, downloadToken, filename)
}

// GetAttachmentToAddressesByToken は download_token に紐づく元メッセージの to_addresses を返す。
func (r *Repository) GetAttachmentToAddressesByToken(ctx context.Context, downloadToken string) ([]string, error) {
	const q = `
		SELECT m.to_addresses
		FROM mail_messages m
		JOIN mail_attachments a ON a.message_id = m.id
		WHERE a.download_token = ?
		LIMIT 1`

	row := r.db.QueryRowContext(ctx, q, downloadToken)
	var toAddressesJSON string
	if err := row.Scan(&toAddressesJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("to_addresses 取得失敗: %w", err)
	}
	var addrs []string
	if err := json.Unmarshal([]byte(toAddressesJSON), &addrs); err != nil {
		return nil, fmt.Errorf("to_addresses デシリアライズ失敗: %w", err)
	}
	return addrs, nil
}

// DisableAttachment は添付ファイルのダウンロード有効/無効を切り替える。
func (r *Repository) DisableAttachment(ctx context.Context, id string, disabled bool) error {
	v := 0
	if disabled {
		v = 1
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE mail_attachments SET is_disabled = ? WHERE id = ?`,
		v, id,
	)
	if err != nil {
		return fmt.Errorf("添付ファイル無効化失敗 (id=%s): %w", id, err)
	}
	return nil
}

// DeleteAttachment は添付ファイルをソフトデリートする。
func (r *Repository) DeleteAttachment(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE mail_attachments SET deleted_at = NOW(6) WHERE id = ? AND deleted_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("添付ファイル削除失敗 (id=%s): %w", id, err)
	}
	return nil
}

// CreateAuditLog は監査ログを1件記録する。
func (r *Repository) CreateAuditLog(ctx context.Context, log *domain.AuditLog) error {
	detail, err := json.Marshal(log.Detail)
	if err != nil {
		return fmt.Errorf("監査ログ detail のエンコード失敗: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO audit_logs
		  (id, event_type, actor_id, actor_email, target_type, target_id, detail, ip_address, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW(3))`,
		log.ID, log.EventType,
		log.ActorID, log.ActorEmail,
		log.TargetType, log.TargetID,
		string(detail), log.IPAddress,
	)
	if err != nil {
		return fmt.Errorf("監査ログ記録失敗: %w", err)
	}
	return nil
}

// ListAuditLogs は監査ログを絞り込み・ページネーションして返す。
func (r *Repository) ListAuditLogs(ctx context.Context, q domain.AuditLogQuery) ([]domain.AuditLog, int, error) {
	if q.PerPage <= 0 {
		q.PerPage = 50
	}
	if q.Page <= 0 {
		q.Page = 1
	}

	where := "1=1"
	args := []any{}

	if q.EventType != "" {
		where += " AND event_type LIKE ?"
		args = append(args, q.EventType+"%")
	}
	if q.ActorID != "" {
		where += " AND actor_id = ?"
		args = append(args, q.ActorID)
	}
	if q.FromDate != "" {
		where += " AND created_at >= ?"
		args = append(args, q.FromDate)
	}
	if q.ToDate != "" {
		where += " AND created_at < DATE_ADD(?, INTERVAL 1 DAY)"
		args = append(args, q.ToDate)
	}

	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_logs WHERE "+where, countArgs...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("監査ログ件数取得失敗: %w", err)
	}

	offset := (q.Page - 1) * q.PerPage
	args = append(args, q.PerPage, offset)
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, event_type, actor_id, actor_email, target_type, target_id, detail, ip_address, created_at "+
			"FROM audit_logs WHERE "+where+
			" ORDER BY created_at DESC LIMIT ? OFFSET ?",
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("監査ログ一覧取得失敗: %w", err)
	}
	defer rows.Close()

	var logs []domain.AuditLog
	for rows.Next() {
		var l domain.AuditLog
		var detailJSON []byte
		if err := rows.Scan(
			&l.ID, &l.EventType,
			&l.ActorID, &l.ActorEmail,
			&l.TargetType, &l.TargetID,
			&detailJSON, &l.IPAddress,
			&l.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("監査ログスキャン失敗: %w", err)
		}
		if len(detailJSON) > 0 {
			_ = json.Unmarshal(detailJSON, &l.Detail)
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("監査ログ取得エラー: %w", err)
	}
	return logs, total, nil
}

// scanUser は*sql.Rowから1件のUserをスキャンする。
func scanUser(row *sql.Row) (*repository.User, error) {
	var u repository.User
	var isActiveInt int
	if err := row.Scan(
		&u.ID, &u.Email, &u.DisplayName,
		&u.PasswordHash, &u.Role, &isActiveInt, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("ユーザーが見つかりません")
		}
		return nil, fmt.Errorf("ユーザー取得失敗: %w", err)
	}
	u.IsActive = isActiveInt == 1
	return &u, nil
}

// ─── API キー ────────────────────────────────────────────────────

func (r *Repository) CreateAPIKey(ctx context.Context, key *domain.APIKey, keyHash string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, name, key_hash, role, created_by, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		key.ID, key.Name, keyHash, string(key.Role), key.CreatedBy, key.ExpiresAt, key.CreatedAt,
	)
	return err
}

func (r *Repository) ListAPIKeys(ctx context.Context) ([]domain.APIKey, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, role, created_by, last_used_at, expires_at, revoked_at, created_at
		 FROM api_keys ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []domain.APIKey
	for rows.Next() {
		var k domain.APIKey
		var role string
		if err := rows.Scan(
			&k.ID, &k.Name, &role,
			&k.CreatedBy, &k.LastUsedAt, &k.ExpiresAt, &k.RevokedAt, &k.CreatedAt,
		); err != nil {
			return nil, err
		}
		k.Role = domain.Role(role)
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (r *Repository) FindAPIKeyByHash(ctx context.Context, keyHash string) (*domain.APIKey, error) {
	var k domain.APIKey
	var role string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, role, created_by, last_used_at, expires_at, revoked_at, created_at
		 FROM api_keys WHERE key_hash = ?`,
		keyHash,
	).Scan(&k.ID, &k.Name, &role, &k.CreatedBy, &k.LastUsedAt, &k.ExpiresAt, &k.RevokedAt, &k.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("API キー検索失敗: %w", err)
	}
	k.Role = domain.Role(role)
	return &k, nil
}

func (r *Repository) RevokeAPIKey(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now(), id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("API キーが見つからないか既に失効しています")
	}
	return nil
}

func (r *Repository) UpdateAPIKeyLastUsed(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		time.Now(), id,
	)
	return err
}
