package domain

import (
	"context"
	"time"
)

// InspectWorker は検査ワーカーのインターフェースである。
// 原本EMLを読むだけでメールを変更しない。全ワーカーが並列実行される。
type InspectWorker interface {
	Name() string
	Inspect(ctx context.Context, mail *Mail) (*InspectResult, error)
}

// TransformWorker は変換ワーカーのインターフェースである。
// EMLの内容を書き換えて新しい Mail を返す。設定順に直列実行される。
type TransformWorker interface {
	Name() string
	Transform(ctx context.Context, mail *Mail) (*Mail, error)
}

// InspectEntry はパイプラインに渡す検査ワーカーとタイムアウトのペアを表す。
// Timeout が 0 の場合は親 context のタイムアウトのみ適用される。
type InspectEntry struct {
	Worker  InspectWorker
	Timeout time.Duration
}

// InspectResult は検査ワーカーの結果を表す。
type InspectResult struct {
	WorkerName string
	Score      int // 0-100
	Detected   bool
	Details    map[string]any
}
