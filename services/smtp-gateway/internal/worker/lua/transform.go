package lua

import (
	"context"
	"fmt"

	glua "github.com/yuin/gopher-lua"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

type transformWorker struct {
	name   string
	source string
	config map[string]any // ワーカー設定（.yaml から読み込んだもの）
}

func (w *transformWorker) Name() string { return w.name }

// Transform はスクリプト内の transform(mail, config) 関数を呼び出し、変更後の Mail を返す。
// subject フィールドが変更された場合は RawEML の Subject ヘッダーも書き換える。
// ctx がキャンセルまたはタイムアウトした場合はエラーを返す。
func (w *transformWorker) Transform(ctx context.Context, mail *domain.Mail) (*domain.Mail, error) {
	type result struct {
		r   *domain.Mail
		err error
	}
	ch := make(chan result, 1)
	go func() {
		r, err := w.runTransform(mail)
		ch <- result{r, err}
	}()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("Luaワーカー %q タイムアウト: %w", w.name, ctx.Err())
	case res := <-ch:
		return res.r, res.err
	}
}

// runTransform は Lua の transform(mail, config) を同期的に実行する。
// LState はこの呼び出しごとに新規作成するため goroutine 安全。
func (w *transformWorker) runTransform(mail *domain.Mail) (*domain.Mail, error) {
	L := glua.NewState()
	defer L.Close()

	module, err := loadModule(L, w.source, w.name)
	if err != nil {
		return nil, err
	}

	transformFn := L.GetField(module, "transform")
	if transformFn == glua.LNil {
		return nil, fmt.Errorf("Luaワーカーに transform 関数がありません (%s)", w.name)
	}

	L.Push(transformFn)
	L.Push(mailToTable(L, mail))
	L.Push(configToTable(L, w.config))
	if err := L.PCall(2, 1, nil); err != nil {
		return nil, fmt.Errorf("transform() 呼び出し失敗 (%s): %w", w.name, err)
	}

	resultTable, ok := L.Get(-1).(*glua.LTable)
	L.Pop(1)
	if !ok {
		return nil, fmt.Errorf("transform() がテーブルを返しませんでした (%s)", w.name)
	}

	return applyTransformResult(mail, resultTable), nil
}
