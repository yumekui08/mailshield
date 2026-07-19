package mariadb

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

func TestCreateWorkerInstance_MarshalsConfig(t *testing.T) {
	repo, mock := newMockRepo(t)
	inst := &domain.WorkerInstance{
		Alias: "fs_internal", DisplayName: "内部向け添付分離", WorkerType: "filesep-worker",
		Kind: domain.WorkerKindTransform, Config: map[string]any{"link_ttl_hours": 72},
		DefaultTimeoutSeconds: 20, IsEnabled: true,
	}
	mock.ExpectExec("INSERT INTO worker_instances").
		WithArgs(sqlmock.AnyArg(), "fs_internal", "内部向け添付分離", "filesep-worker",
			"transform", `{"link_ttl_hours":72}`, 20, 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.CreateWorkerInstance(context.Background(), inst); err != nil {
		t.Fatalf("CreateWorkerInstance() error = %v", err)
	}
	if inst.ID == "" {
		t.Error("ID が採番されていない")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestListWorkerInstances_ParsesConfigJSON(t *testing.T) {
	repo, mock := newMockRepo(t)
	now := time.Now()
	cols := []string{"id", "alias", "display_name", "worker_type", "kind", "config_json",
		"default_timeout_seconds", "is_enabled", "created_at", "updated_at"}
	mock.ExpectQuery("SELECT id, alias, display_name, worker_type, kind, config_json").
		WillReturnRows(sqlmock.NewRows(cols).AddRow(
			"i1", "av_internal", "内部AV", "av-worker", "inspect",
			`{"threshold":50}`, 30, 1, now, now))

	list, err := repo.ListWorkerInstances(context.Background())
	if err != nil {
		t.Fatalf("ListWorkerInstances() error = %v", err)
	}
	if len(list) != 1 || list[0].Alias != "av_internal" || !list[0].IsEnabled {
		t.Fatalf("結果が不正: %+v", list)
	}
	if v, _ := list[0].Config["threshold"].(float64); v != 50 {
		t.Errorf("config_json がパースされていない: %+v", list[0].Config)
	}
}

func TestCreateConfigVariable_Insert(t *testing.T) {
	repo, mock := newMockRepo(t)
	v := &domain.ConfigVariable{Key: "INTERNAL_DOMAIN", Value: "@example.com", Description: "共有ドメイン"}
	mock.ExpectExec("INSERT INTO config_variables").
		WithArgs(sqlmock.AnyArg(), "INTERNAL_DOMAIN", "@example.com", "共有ドメイン").
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := repo.CreateConfigVariable(context.Background(), v); err != nil {
		t.Fatalf("CreateConfigVariable() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
