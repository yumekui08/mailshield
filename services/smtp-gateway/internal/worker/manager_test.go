package worker_test

import (
	"context"
	"testing"
	"time"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker"
)

// ─── スタブ ─────────────────────────────────────────────────────

type stubInspect struct{ name string }

func (s *stubInspect) Name() string { return s.name }
func (s *stubInspect) Inspect(_ context.Context, mail *domain.Mail) (*domain.InspectResult, error) {
	return &domain.InspectResult{WorkerName: s.name}, nil
}

type stubTransform struct{ name string }

func (s *stubTransform) Name() string { return s.name }
func (s *stubTransform) Transform(_ context.Context, mail *domain.Mail) (*domain.Mail, error) {
	return mail, nil
}

// emptyCfg は Lua ワーカーを持たないテスト用設定を返す。
// worker.New に渡す workersDir には存在しないパスを使うと loader は空マップを返す。
func emptyCfg(inspect []config.InspectWorkerConfig, transform []config.TransformWorkerConfig) *config.WorkersConfig {
	return &config.WorkersConfig{
		Inspect:   inspect,
		Transform: transform,
	}
}

const testWorkersDir = "/nonexistent/workers"
const testConfigDir = "/nonexistent/conf"

// ─── テスト ─────────────────────────────────────────────────────

func TestManager_NoWorkers(t *testing.T) {
	m, err := worker.New(testWorkersDir, testConfigDir, emptyCfg(nil, nil), nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if len(m.InspectWorkers()) != 0 {
		t.Errorf("InspectWorkers() len = %d, want 0", len(m.InspectWorkers()))
	}
	if len(m.TransformWorkers()) != 0 {
		t.Errorf("TransformWorkers() len = %d, want 0", len(m.TransformWorkers()))
	}
}

func TestManager_OnlyEnabledInspectWorkers(t *testing.T) {
	cfg := emptyCfg(
		[]config.InspectWorkerConfig{
			{Name: "av-worker", Enabled: true, TimeoutSeconds: 30},
			{Name: "dlp-worker", Enabled: false, TimeoutSeconds: 60},
		},
		nil,
	)
	builtins := []domain.InspectWorker{
		&stubInspect{name: "av-worker"},
		&stubInspect{name: "dlp-worker"},
	}

	m, err := worker.New(testWorkersDir, testConfigDir, cfg, builtins, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got := m.InspectWorkers()
	if len(got) != 1 {
		t.Fatalf("InspectWorkers() len = %d, want 1", len(got))
	}
	if got[0].Name() != "av-worker" {
		t.Errorf("InspectWorkers()[0].Name() = %q, want av-worker", got[0].Name())
	}
}

func TestManager_InspectEntries_TimeoutSet(t *testing.T) {
	cfg := emptyCfg(
		[]config.InspectWorkerConfig{
			{Name: "av-worker", Enabled: true, TimeoutSeconds: 30},
		},
		nil,
	)
	builtins := []domain.InspectWorker{&stubInspect{name: "av-worker"}}

	m, err := worker.New(testWorkersDir, testConfigDir, cfg, builtins, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	entries := m.InspectEntries()
	if len(entries) != 1 {
		t.Fatalf("InspectEntries() len = %d, want 1", len(entries))
	}
	want := 30 * time.Second
	if entries[0].Timeout != want {
		t.Errorf("entries[0].Timeout = %v, want %v", entries[0].Timeout, want)
	}
}

func TestManager_TransformWorkersSortedByOrder(t *testing.T) {
	cfg := emptyCfg(
		nil,
		[]config.TransformWorkerConfig{
			{Name: "filesep", Enabled: true, Order: 3},
			{Name: "sanitize", Enabled: true, Order: 1},
			{Name: "urlrewrite", Enabled: true, Order: 2},
		},
	)
	builtins := []domain.TransformWorker{
		&stubTransform{name: "filesep"},
		&stubTransform{name: "sanitize"},
		&stubTransform{name: "urlrewrite"},
	}

	m, err := worker.New(testWorkersDir, testConfigDir, cfg, nil, builtins)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got := m.TransformWorkers()
	if len(got) != 3 {
		t.Fatalf("TransformWorkers() len = %d, want 3", len(got))
	}
	wantOrder := []string{"sanitize", "urlrewrite", "filesep"}
	for i, w := range got {
		if w.Name() != wantOrder[i] {
			t.Errorf("TransformWorkers()[%d].Name() = %q, want %q", i, w.Name(), wantOrder[i])
		}
	}
}

func TestManager_UnknownWorkerSkipped(t *testing.T) {
	cfg := emptyCfg(
		[]config.InspectWorkerConfig{
			{Name: "av-worker", Enabled: true},
			{Name: "nonexistent-worker", Enabled: true},
		},
		nil,
	)
	builtins := []domain.InspectWorker{
		&stubInspect{name: "av-worker"},
	}

	m, err := worker.New(testWorkersDir, testConfigDir, cfg, builtins, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got := m.InspectWorkers()
	if len(got) != 1 {
		t.Fatalf("存在しないワーカーはスキップされるべき: len = %d, want 1", len(got))
	}
	if got[0].Name() != "av-worker" {
		t.Errorf("got[0].Name() = %q, want av-worker", got[0].Name())
	}
}

func TestManager_BuiltinOverridesLua(t *testing.T) {
	// WorkersDir に実際の Lua ファイルなしで、同名の組み込みワーカーが登録される。
	// Lua マップには何もないが、組み込みは直接 registry を上書くため組み込みが使われる。
	cfg := emptyCfg(
		[]config.InspectWorkerConfig{
			{Name: "av-worker", Enabled: true},
		},
		nil,
	)
	builtinAV := &stubInspect{name: "av-worker"}
	m, err := worker.New(testWorkersDir, testConfigDir, cfg, []domain.InspectWorker{builtinAV}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got := m.InspectWorkers()
	if len(got) != 1 {
		t.Fatalf("InspectWorkers() len = %d, want 1", len(got))
	}
	// 組み込みスタブが使われているか確認（同じポインタ）
	if got[0] != domain.InspectWorker(builtinAV) {
		t.Errorf("組み込みワーカーが使われるべき")
	}
}

func TestManager_DisabledTransformExcluded(t *testing.T) {
	cfg := emptyCfg(
		nil,
		[]config.TransformWorkerConfig{
			{Name: "sanitize", Enabled: true, Order: 1},
			{Name: "filesep", Enabled: false, Order: 2},
		},
	)
	builtins := []domain.TransformWorker{
		&stubTransform{name: "sanitize"},
		&stubTransform{name: "filesep"},
	}

	m, err := worker.New(testWorkersDir, testConfigDir, cfg, nil, builtins)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got := m.TransformWorkers()
	if len(got) != 1 || got[0].Name() != "sanitize" {
		t.Errorf("無効な変換ワーカーは除外されるべき: got %v", got)
	}
}
