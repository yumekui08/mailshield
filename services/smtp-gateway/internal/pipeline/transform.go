package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

type TransformPipeline struct {
	workers []domain.TransformWorker
}

func NewTransformPipeline(workers []domain.TransformWorker) *TransformPipeline {
	return &TransformPipeline{workers: workers}
}

func (p *TransformPipeline) Run(ctx context.Context, mail *domain.Mail) (*domain.Mail, error) {
	current := mail
	for _, w := range p.workers {
		result, err := w.Transform(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("変換ワーカー %s が失敗: %w", w.Name(), err)
		}
		slog.Info("変換ワーカー完了",
			"worker", w.Name(),
			"message_id", current.MessageID)
		current = result
	}
	return current, nil
}
