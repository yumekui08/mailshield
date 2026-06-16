# Go 組み込みワーカー開発ガイド

ClamAV・外部 HTTP API 連携・バイナリ処理など、Lua では難しい処理を Go で実装できます。
組み込みワーカーはビルド時にバイナリに含まれます。

---

## インターフェース

```go
// internal/domain/worker.go

type InspectWorker interface {
    Name() string
    Inspect(ctx context.Context, mail *Mail) (*InspectResult, error)
}

type TransformWorker interface {
    Name() string
    Transform(ctx context.Context, mail *Mail) (*Mail, error)
}

type InspectResult struct {
    WorkerName string
    Score      int            // 0-100
    Detected   bool
    Details    map[string]any
}
```

---

## ファイルの配置

```
services/smtp-gateway/internal/worker/builtin/
└── myworker/
    ├── worker.go    ← InspectWorker / TransformWorker を実装
    └── config.go    ← 設定構造体（任意）
```

---

## 検査ワーカーの実装例

```go
// internal/worker/builtin/myworker/worker.go
package myworker

import (
    "context"
    "strings"

    "github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

type Worker struct {
    threshold int
}

type Config struct {
    Threshold int      `yaml:"threshold"`
    Keywords  []string `yaml:"keywords"`
}

func New(cfg Config) *Worker {
    return &Worker{threshold: cfg.Threshold}
}

func (w *Worker) Name() string { return "my-worker" }

func (w *Worker) Inspect(ctx context.Context, mail *domain.Mail) (*domain.InspectResult, error) {
    subject := strings.ToLower(mail.Subject)

    for _, kw := range []string{"urgent", "confidential"} {
        if strings.Contains(subject, kw) {
            return &domain.InspectResult{
                WorkerName: w.Name(),
                Score:      80,
                Detected:   true,
                Details:    map[string]any{"reason": "keyword: " + kw},
            }, nil
        }
    }

    return &domain.InspectResult{
        WorkerName: w.Name(),
        Score:      0,
        Detected:   false,
        Details:    map[string]any{},
    }, nil
}
```

---

## `main.go` への登録

`cmd/server/main.go` の `builtinInspect` / `builtinTransform` スライスに追加します。

```go
// cmd/server/main.go
import (
    "github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/myworker"
)

// ...

builtinInspect := []domain.InspectWorker{
    // ... 既存ワーカー ...
    myworker.New(myworker.Config{Threshold: 60}),  // ← 追加
}
```

---

## 設定ファイルの読み込み

ワーカー設定を `config/workers/conf/{name}.yaml` から読み込む場合は、
マネージャーが渡す `rawConfig map[string]any` を自前でデシリアライズします。

```go
import "gopkg.in/yaml.v3"

func NewFromRaw(raw map[string]any) (*Worker, error) {
    b, _ := yaml.Marshal(raw)
    var cfg Config
    if err := yaml.Unmarshal(b, &cfg); err != nil {
        return nil, err
    }
    return New(cfg), nil
}
```

---

## ガイドライン

- `Inspect` / `Transform` の中で panic しない。エラーは `error` で返す
- `ctx` のキャンセル・デッドラインを尊重する（外部 HTTP/TCP には `ctx` を渡す）
- 外部サービスへの接続は起動時に初期化し、リクエストごとに再接続しない
- `Name()` は `mailshield.yaml` のワーカー名と完全一致させる

インターフェース定義（`internal/domain/worker.go`）を変更する場合は、
事前に既存の全ワーカーへの影響を確認してください。
