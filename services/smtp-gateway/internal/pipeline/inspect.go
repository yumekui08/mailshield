// Package pipeline は検査パイプライン（並列）と変換パイプライン（直列）を実装する。
package pipeline

import (
	"context"
	"log/slog"
	"sync"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// InspectPipeline は全検査ワーカーを並列実行して結果を返す。
type InspectPipeline struct {
	workers []domain.InspectWorker
}

// NewInspectPipeline は InspectPipeline を構築する。
func NewInspectPipeline(workers []domain.InspectWorker) *InspectPipeline {
	return &InspectPipeline{workers: workers}
}

// Run は全検査ワーカーを並列実行し、成功したワーカーの結果を返す。
// 個別ワーカーのエラーはログに記録してスキップする（他のワーカーの結果は継続）。
func (p *InspectPipeline) Run(ctx context.Context, mail *domain.Mail) ([]*domain.InspectResult, error) {
	if len(p.workers) == 0 {
		return nil, nil
	}

	results := make([]*domain.InspectResult, len(p.workers))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, w := range p.workers {
		wg.Add(1)
		go func(idx int, worker domain.InspectWorker) {
			defer wg.Done()
			result, err := worker.Inspect(ctx, mail)
			if err != nil {
				slog.Warn("検査ワーカーエラー（スキップ）",
					"worker", worker.Name(),
					"message_id", mail.MessageID,
					"error", err)
				return
			}
			mu.Lock()
			results[idx] = result
			mu.Unlock()
		}(i, w)
	}
	wg.Wait()

	// nil を除いた結果を返す
	var valid []*domain.InspectResult
	for _, r := range results {
		if r != nil {
			valid = append(valid, r)
		}
	}
	return valid, nil
}
