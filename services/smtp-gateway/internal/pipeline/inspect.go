package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

type InspectPipeline struct {
	entries []domain.InspectEntry
}

// Timeout > 0 のエントリはそのワーカー専用の context.WithTimeout が作られる
func NewInspectPipeline(entries []domain.InspectEntry) *InspectPipeline {
	return &InspectPipeline{entries: entries}
}

// 個別ワーカーのエラーはログに記録してスキップする（他ワーカーの結果に影響しない）
func (p *InspectPipeline) Run(ctx context.Context, mail *domain.Mail) ([]*domain.InspectResult, error) {
	if len(p.entries) == 0 {
		return nil, nil
	}

	results := make([]*domain.InspectResult, len(p.entries))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, e := range p.entries {
		wg.Add(1)
		go func(idx int, entry domain.InspectEntry) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("検査ワーカーパニック（スキップ）",
						"worker", entry.Worker.Name(),
						"message_id", mail.MessageID,
						"panic", fmt.Sprintf("%v", r),
						"stack", string(debug.Stack()))
				}
			}()

			wCtx := ctx
			if entry.Timeout > 0 {
				var cancel context.CancelFunc
				wCtx, cancel = context.WithTimeout(ctx, entry.Timeout)
				defer cancel()
			}

			result, err := entry.Worker.Inspect(wCtx, mail)
			if err != nil {
				slog.Warn("検査ワーカーエラー（スキップ）",
					"worker", entry.Worker.Name(),
					"message_id", mail.MessageID,
					"error", err)
				return
			}
			mu.Lock()
			results[idx] = result
			mu.Unlock()
		}(i, e)
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
