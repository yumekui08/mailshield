package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

type TransformPipeline struct {
	workers []domain.TransformWorker
}

func NewTransformPipeline(workers []domain.TransformWorker) *TransformPipeline {
	return &TransformPipeline{workers: workers}
}

func (p *TransformPipeline) Run(ctx context.Context, mail *domain.Mail) (result *domain.Mail, err error) {
	current := mail
	for _, w := range p.workers {
		result, err = runTransform(ctx, w, current)
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

func runTransform(ctx context.Context, w domain.TransformWorker, mail *domain.Mail) (result *domain.Mail, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("変換ワーカーパニック",
				"worker", w.Name(),
				"message_id", mail.MessageID,
				"panic", fmt.Sprintf("%v", r),
				"stack", string(debug.Stack()))
			err = fmt.Errorf("変換ワーカー %s がパニック: %v", w.Name(), r)
		}
	}()
	return w.Transform(ctx, mail)
}
