package pipeline_test

import (
	"context"
	"errors"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/pipeline"
)

// stubTransformWorker はテスト用スタブ。
type stubTransformWorker struct {
	name   string
	result *domain.Mail
	err    error
}

func (w *stubTransformWorker) Name() string { return w.name }
func (w *stubTransformWorker) Transform(_ context.Context, mail *domain.Mail) (*domain.Mail, error) {
	if w.err != nil {
		return nil, w.err
	}
	if w.result != nil {
		return w.result, nil
	}
	return mail, nil
}

func TestTransformPipeline_Run_NoWorkers(t *testing.T) {
	original := &domain.Mail{MessageID: "test", Subject: "Hello"}
	p := pipeline.NewTransformPipeline(nil)
	result, err := p.Run(context.Background(), original)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}
	if result != original {
		t.Error("Run() with no workers should return the original mail unchanged")
	}
}

func TestTransformPipeline_Run_ChainedTransform(t *testing.T) {
	mail1 := &domain.Mail{MessageID: "test", Subject: "step1"}
	mail2 := &domain.Mail{MessageID: "test", Subject: "step2"}

	workers := []domain.TransformWorker{
		&stubTransformWorker{name: "worker-1", result: mail1},
		&stubTransformWorker{name: "worker-2", result: mail2},
	}

	p := pipeline.NewTransformPipeline(workers)
	result, err := p.Run(context.Background(), &domain.Mail{MessageID: "test", Subject: "original"})
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}
	// 最後のワーカーが返した mail2 が最終結果
	if result != mail2 {
		t.Errorf("Run() final result = %v, want mail2", result)
	}
}

func TestTransformPipeline_Run_ErrorStopsChain(t *testing.T) {
	workers := []domain.TransformWorker{
		&stubTransformWorker{name: "worker-1"},
		&stubTransformWorker{name: "worker-2", err: errors.New("transform failed")},
		&stubTransformWorker{name: "worker-3"}, // 実行されない
	}

	p := pipeline.NewTransformPipeline(workers)
	_, err := p.Run(context.Background(), &domain.Mail{MessageID: "test"})
	if err == nil {
		t.Error("Run() should return error when a worker fails")
	}
}
