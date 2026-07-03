package domain

import "errors"

// ErrMailRejected はポリシールールによる恒久的な拒否を表す。
// SMTP 層はこのエラーを受け取ったとき 550 5.7.1 を返す。
var ErrMailRejected = errors.New("メール拒否")

// ErrNoRuleMatched はポリシールールにひとつもマッチしなかったことを表す設定エラー。
// SMTP 層はこのエラーを受け取ったとき 550 5.7.0 を返す。
var ErrNoRuleMatched = errors.New("マッチするポリシールールがありません")

// ErrNoRouteMatched は routes.d のどのルートにもマッチしなかったことを表す設定エラー。
// SMTP 層はこのエラーを受け取ったとき 550 5.7.0 を返す。
var ErrNoRouteMatched = errors.New("マッチするルートがありません")

// ctxDryRunKey はコンテキストに dry-run フラグを埋め込むための非公開キー型。
type ctxDryRunKey struct{}

// CtxDryRun はシミュレーション実行時に context に付与するキー。
// この値が存在するとき、副作用を持つワーカー（filesep 等）は実際の保存を省略する。
var CtxDryRun = ctxDryRunKey{}
