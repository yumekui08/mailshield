package pipeline_test

import (
	"context"
	"errors"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/pipeline"
)

// stubInspectWorker はテスト用スタブ。
type stubInspectWorker struct {
	name     string
	result   *domain.InspectResult
	err      error
}

func (w *stubInspectWorker) Name() string { return w.name }
func (w *stubInspectWorker) Inspect(_ context.Context, _ *domain.Mail) (*domain.InspectResult, error) {
	return w.result, w.err
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
	workers := []domain.InspectWorker{
		&stubInspectWorker{
			name:   "worker-a",
			result: &domain.InspectResult{WorkerName: "worker-a", Score: 100, Detected: true},
		},
		&stubInspectWorker{
			name:   "worker-b",
			result: &domain.InspectResult{WorkerName: "worker-b", Score: 0, Detected: false},
		},
	}

	p := pipeline.NewInspectPipeline(workers)
	results, err := p.Run(context.Background(), &domain.Mail{MessageID: "test"})
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("len(results) = %d, want 2", len(results))
	}
}

func TestInspectPipeline_Run_ErrorWorkerIsSkipped(t *testing.T) {
	workers := []domain.InspectWorker{
		&stubInspectWorker{
			name:   "ok-worker",
			result: &domain.InspectResult{WorkerName: "ok-worker", Score: 50},
		},
		&stubInspectWorker{
			name: "error-worker",
			err:  errors.New("something went wrong"),
		},
	}

	p := pipeline.NewInspectPipeline(workers)
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
