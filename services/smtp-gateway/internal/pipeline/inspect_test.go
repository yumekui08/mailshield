package pipeline_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/pipeline"
)

// stubInspectWorker はテスト用スタブ。
// fn が設定されている場合は fn を呼ぶ（タイムアウトテスト用）。
type stubInspectWorker struct {
	name   string
	result *domain.InspectResult
	err    error
	fn     func(ctx context.Context) (*domain.InspectResult, error)
}

func (w *stubInspectWorker) Name() string { return w.name }
func (w *stubInspectWorker) Inspect(ctx context.Context, _ *domain.Mail) (*domain.InspectResult, error) {
	if w.fn != nil {
		return w.fn(ctx)
	}
	return w.result, w.err
}

func entry(w domain.InspectWorker, timeoutSec int) domain.InspectEntry {
	return domain.InspectEntry{Worker: w, Timeout: time.Duration(timeoutSec) * time.Second}
}

func TestInspectPipeline_Run_NoWorkers(t *testing.T) {
	p := pipeline.NewInspectPipeline(nil)
	results, err := p.Run(context.Background(), &domain.Mail{})
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Run() returned %d results, want 0", len(results))
	}
}

func TestInspectPipeline_Run_AllSucceed(t *testing.T) {
	entries := []domain.InspectEntry{
		entry(&stubInspectWorker{
			name:   "worker-a",
			result: &domain.InspectResult{WorkerName: "worker-a", Score: 100, Detected: true},
		}, 5),
		entry(&stubInspectWorker{
			name:   "worker-b",
			result: &domain.InspectResult{WorkerName: "worker-b", Score: 0, Detected: false},
		}, 0), // タイムアウトなし
	}

	p := pipeline.NewInspectPipeline(entries)
	results, err := p.Run(context.Background(), &domain.Mail{MessageID: "test"})
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("len(results) = %d, want 2", len(results))
	}
}

func TestInspectPipeline_Run_ErrorWorkerIsSkipped(t *testing.T) {
	entries := []domain.InspectEntry{
		entry(&stubInspectWorker{
			name:   "ok-worker",
			result: &domain.InspectResult{WorkerName: "ok-worker", Score: 50},
		}, 5),
		entry(&stubInspectWorker{
			name: "error-worker",
			err:  errors.New("something went wrong"),
		}, 5),
	}

	p := pipeline.NewInspectPipeline(entries)
	results, err := p.Run(context.Background(), &domain.Mail{MessageID: "test"})
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}
	// エラーワーカーはスキップされ、成功したワーカーの結果のみ返る
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
	if results[0].WorkerName != "ok-worker" {
		t.Errorf("results[0].WorkerName = %q, want %q", results[0].WorkerName, "ok-worker")
	}
}

func TestInspectPipeline_Run_WorkerTimesOut(t *testing.T) {
	slow := &stubInspectWorker{
		name: "slow-worker",
		fn: func(ctx context.Context) (*domain.InspectResult, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return &domain.InspectResult{WorkerName: "slow-worker"}, nil
			}
		},
	}
	entries := []domain.InspectEntry{
		{Worker: slow, Timeout: 50 * time.Millisecond}, // 50ms で強制タイムアウト
	}
	p := pipeline.NewInspectPipeline(entries)
	results, err := p.Run(context.Background(), &domain.Mail{MessageID: "test"})
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}
	// タイムアウトしたワーカーはスキップされるため結果は 0 件
	if len(results) != 0 {
		t.Errorf("タイムアウトしたワーカーはスキップされるべき: len(results) = %d", len(results))
	}
}
