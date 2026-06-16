package lua

import (
	"context"
	"fmt"

	glua "github.com/yuin/gopher-lua"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

type inspectWorker struct {
	name   string
	source string
	config map[string]any // ワーカー設定（.yaml から読み込んだもの）
}

func (w *inspectWorker) Name() string { return w.name }

// Inspect はスクリプト内の inspect(mail, config) 関数を呼び出して結果を返す。
// Lua の実行は goroutine ごとに独立した State で行うため並列安全。
func (w *inspectWorker) Inspect(_ context.Context, mail *domain.Mail) (*domain.InspectResult, error) {
	L := glua.NewState()
	defer L.Close()

	module, err := loadModule(L, w.source, w.name)
	if err != nil {
		return nil, err
	}

	inspectFn := L.GetField(module, "inspect")
	if inspectFn == glua.LNil {
		return nil, fmt.Errorf("Luaワーカーに inspect 関数がありません (%s)", w.name)
	}

	L.Push(inspectFn)
	L.Push(mailToTable(L, mail))
	L.Push(configToTable(L, w.config))
	if err := L.PCall(2, 1, nil); err != nil {
		return nil, fmt.Errorf("inspect() 呼び出し失敗 (%s): %w", w.name, err)
	}

	resultTable, ok := L.Get(-1).(*glua.LTable)
	L.Pop(1)
	if !ok {
		return nil, fmt.Errorf("inspect() がテーブルを返しませんでした (%s)", w.name)
	}

	result := &domain.InspectResult{
		WorkerName: w.name,
		Details:    make(map[string]any),
	}
	if b, ok := resultTable.RawGetString("detected").(glua.LBool); ok {
		result.Detected = bool(b)
	}
	if n, ok := resultTable.RawGetString("score").(glua.LNumber); ok {
		result.Score = int(n)
	}
	if dt, ok := resultTable.RawGetString("details").(*glua.LTable); ok {
		dt.ForEach(func(k, v glua.LValue) {
			result.Details[k.String()] = luaToAny(v)
		})
	}

	return result, nil
}
