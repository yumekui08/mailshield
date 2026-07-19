package mariadb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
	"github.com/koizumib/mailshield/services/api-server/internal/repository"
)

// ─── ワーカーインスタンス ─────────────────────────────────────────────

func (r *Repository) ListWorkerInstances(ctx context.Context) ([]domain.WorkerInstance, error) {
	const q = `
		SELECT id, alias, display_name, worker_type, kind, config_json,
		       default_timeout_seconds, is_enabled, created_at, updated_at
		FROM worker_instances
		ORDER BY kind ASC, alias ASC`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("ワーカーインスタンス一覧取得失敗: %w", err)
	}
	defer rows.Close()

	list := []domain.WorkerInstance{}
	for rows.Next() {
		w, err := scanWorkerInstance(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *w)
	}
	return list, rows.Err()
}

func (r *Repository) GetWorkerInstance(ctx context.Context, id string) (*domain.WorkerInstance, error) {
	const q = `
		SELECT id, alias, display_name, worker_type, kind, config_json,
		       default_timeout_seconds, is_enabled, created_at, updated_at
		FROM worker_instances WHERE id = ?`
	w, err := scanWorkerInstance(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ワーカーインスタンス取得失敗: %w", err)
	}
	return w, nil
}

func (r *Repository) CreateWorkerInstance(ctx context.Context, w *domain.WorkerInstance) error {
	if w.ID == "" {
		w.ID = uuid.NewString()
	}
	cfg, err := marshalConfig(w.Config)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO worker_instances
		    (id, alias, display_name, worker_type, kind, config_json, default_timeout_seconds, is_enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = r.db.ExecContext(ctx, q, w.ID, w.Alias, w.DisplayName, w.WorkerType,
		string(w.Kind), cfg, w.DefaultTimeoutSeconds, boolToInt(w.IsEnabled))
	if err != nil {
		return fmt.Errorf("ワーカーインスタンス作成失敗: %w", err)
	}
	return nil
}

func (r *Repository) UpdateWorkerInstance(ctx context.Context, w *domain.WorkerInstance) error {
	cfg, err := marshalConfig(w.Config)
	if err != nil {
		return err
	}
	const q = `
		UPDATE worker_instances
		   SET alias = ?, display_name = ?, worker_type = ?, kind = ?, config_json = ?,
		       default_timeout_seconds = ?, is_enabled = ?
		 WHERE id = ?`
	_, err = r.db.ExecContext(ctx, q, w.Alias, w.DisplayName, w.WorkerType, string(w.Kind),
		cfg, w.DefaultTimeoutSeconds, boolToInt(w.IsEnabled), w.ID)
	if err != nil {
		return fmt.Errorf("ワーカーインスタンス更新失敗: %w", err)
	}
	return nil
}

func (r *Repository) DeleteWorkerInstance(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM worker_instances WHERE id = ?`, id); err != nil {
		return fmt.Errorf("ワーカーインスタンス削除失敗: %w", err)
	}
	return nil
}

// rowScanner は *sql.Row と *sql.Rows の共通スキャン用。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanWorkerInstance(s rowScanner) (*domain.WorkerInstance, error) {
	var w domain.WorkerInstance
	var kind string
	var cfg []byte
	var enabledInt int
	if err := s.Scan(&w.ID, &w.Alias, &w.DisplayName, &w.WorkerType, &kind, &cfg,
		&w.DefaultTimeoutSeconds, &enabledInt, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return nil, err
	}
	w.Kind = domain.WorkerKind(kind)
	w.IsEnabled = enabledInt == 1
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &w.Config); err != nil {
			return nil, fmt.Errorf("ワーカー設定 JSON パース失敗 (id=%s): %w", w.ID, err)
		}
	}
	if w.Config == nil {
		w.Config = map[string]any{}
	}
	return &w, nil
}

func marshalConfig(cfg map[string]any) (string, error) {
	if cfg == nil {
		return "{}", nil
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("ワーカー設定 JSON 変換失敗: %w", err)
	}
	return string(b), nil
}

// ─── 設定変数 ─────────────────────────────────────────────────────────

func (r *Repository) ListConfigVariables(ctx context.Context) ([]domain.ConfigVariable, error) {
	const q = `SELECT id, var_key, value, description, created_at, updated_at
	           FROM config_variables ORDER BY var_key ASC`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("設定変数一覧取得失敗: %w", err)
	}
	defer rows.Close()

	list := []domain.ConfigVariable{}
	for rows.Next() {
		var v domain.ConfigVariable
		if err := rows.Scan(&v.ID, &v.Key, &v.Value, &v.Description, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, fmt.Errorf("設定変数スキャン失敗: %w", err)
		}
		list = append(list, v)
	}
	return list, rows.Err()
}

func (r *Repository) GetConfigVariable(ctx context.Context, id string) (*domain.ConfigVariable, error) {
	const q = `SELECT id, var_key, value, description, created_at, updated_at
	           FROM config_variables WHERE id = ?`
	var v domain.ConfigVariable
	err := r.db.QueryRowContext(ctx, q, id).Scan(
		&v.ID, &v.Key, &v.Value, &v.Description, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("設定変数取得失敗: %w", err)
	}
	return &v, nil
}

func (r *Repository) CreateConfigVariable(ctx context.Context, v *domain.ConfigVariable) error {
	if v.ID == "" {
		v.ID = uuid.NewString()
	}
	const q = `INSERT INTO config_variables (id, var_key, value, description) VALUES (?, ?, ?, ?)`
	if _, err := r.db.ExecContext(ctx, q, v.ID, v.Key, v.Value, v.Description); err != nil {
		return fmt.Errorf("設定変数作成失敗: %w", err)
	}
	return nil
}

func (r *Repository) UpdateConfigVariable(ctx context.Context, v *domain.ConfigVariable) error {
	const q = `UPDATE config_variables SET var_key = ?, value = ?, description = ? WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, q, v.Key, v.Value, v.Description, v.ID); err != nil {
		return fmt.Errorf("設定変数更新失敗: %w", err)
	}
	return nil
}

func (r *Repository) DeleteConfigVariable(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM config_variables WHERE id = ?`, id); err != nil {
		return fmt.Errorf("設定変数削除失敗: %w", err)
	}
	return nil
}

// boolToInt は TINYINT(1) 保存用。
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// 静的アサーション: *Repository が ConfigRepository を満たすことを保証する。
var _ repository.ConfigRepository = (*Repository)(nil)

// ─── ルーティング ─────────────────────────────────────────────────────

func (r *Repository) ListRoutings(ctx context.Context) ([]domain.Routing, error) {
	const q = `
		SELECT id, name, priority, match_expr, is_catchall, is_enabled, policy_ref,
		       inspect_json, transform_json, created_at, updated_at
		FROM routings
		ORDER BY priority ASC, created_at ASC`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("ルーティング一覧取得失敗: %w", err)
	}
	defer rows.Close()

	list := []domain.Routing{}
	for rows.Next() {
		rt, err := scanRouting(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *rt)
	}
	return list, rows.Err()
}

func (r *Repository) GetRouting(ctx context.Context, id string) (*domain.Routing, error) {
	const q = `
		SELECT id, name, priority, match_expr, is_catchall, is_enabled, policy_ref,
		       inspect_json, transform_json, created_at, updated_at
		FROM routings WHERE id = ?`
	rt, err := scanRouting(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ルーティング取得失敗: %w", err)
	}
	return rt, nil
}

func (r *Repository) CreateRouting(ctx context.Context, rt *domain.Routing) error {
	if rt.ID == "" {
		rt.ID = uuid.NewString()
	}
	insJSON, transJSON, err := marshalBindings(rt)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO routings
		    (id, name, priority, match_expr, is_catchall, is_enabled, policy_ref, inspect_json, transform_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = r.db.ExecContext(ctx, q, rt.ID, rt.Name, rt.Priority, rt.MatchExpr,
		boolToInt(rt.IsCatchAll), boolToInt(rt.IsEnabled), rt.PolicyRef, insJSON, transJSON)
	if err != nil {
		return fmt.Errorf("ルーティング作成失敗: %w", err)
	}
	return nil
}

func (r *Repository) UpdateRouting(ctx context.Context, rt *domain.Routing) error {
	insJSON, transJSON, err := marshalBindings(rt)
	if err != nil {
		return err
	}
	const q = `
		UPDATE routings
		   SET name = ?, priority = ?, match_expr = ?, is_enabled = ?, policy_ref = ?,
		       inspect_json = ?, transform_json = ?
		 WHERE id = ?`
	// is_catchall は更新しない（システム保証・変更不可）。
	_, err = r.db.ExecContext(ctx, q, rt.Name, rt.Priority, rt.MatchExpr,
		boolToInt(rt.IsEnabled), rt.PolicyRef, insJSON, transJSON, rt.ID)
	if err != nil {
		return fmt.Errorf("ルーティング更新失敗: %w", err)
	}
	return nil
}

func (r *Repository) DeleteRouting(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM routings WHERE id = ?`, id); err != nil {
		return fmt.Errorf("ルーティング削除失敗: %w", err)
	}
	return nil
}

func (r *Repository) CountCatchAllRoutings(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM routings WHERE is_catchall = 1`).Scan(&n); err != nil {
		return 0, fmt.Errorf("catch-all ルーティング数取得失敗: %w", err)
	}
	return n, nil
}

func scanRouting(s rowScanner) (*domain.Routing, error) {
	var rt domain.Routing
	var catchInt, enabledInt int
	var insJSON, transJSON []byte
	if err := s.Scan(&rt.ID, &rt.Name, &rt.Priority, &rt.MatchExpr, &catchInt, &enabledInt,
		&rt.PolicyRef, &insJSON, &transJSON, &rt.CreatedAt, &rt.UpdatedAt); err != nil {
		return nil, err
	}
	rt.IsCatchAll = catchInt == 1
	rt.IsEnabled = enabledInt == 1
	rt.Inspect = []domain.WorkerBinding{}
	rt.Transform = []domain.WorkerBinding{}
	if len(insJSON) > 0 {
		if err := json.Unmarshal(insJSON, &rt.Inspect); err != nil {
			return nil, fmt.Errorf("inspect_json パース失敗 (id=%s): %w", rt.ID, err)
		}
	}
	if len(transJSON) > 0 {
		if err := json.Unmarshal(transJSON, &rt.Transform); err != nil {
			return nil, fmt.Errorf("transform_json パース失敗 (id=%s): %w", rt.ID, err)
		}
	}
	return &rt, nil
}

func marshalBindings(rt *domain.Routing) (ins string, trans string, err error) {
	inspect := rt.Inspect
	if inspect == nil {
		inspect = []domain.WorkerBinding{}
	}
	transform := rt.Transform
	if transform == nil {
		transform = []domain.WorkerBinding{}
	}
	ib, err := json.Marshal(inspect)
	if err != nil {
		return "", "", fmt.Errorf("inspect JSON 変換失敗: %w", err)
	}
	tb, err := json.Marshal(transform)
	if err != nil {
		return "", "", fmt.Errorf("transform JSON 変換失敗: %w", err)
	}
	return string(ib), string(tb), nil
}
