// Package metrics は Prometheus テキスト形式（text exposition format）の
// メトリクスを外部ライブラリなしで提供する。
//
// smtp-gateway は依存を最小に保つ方針のため、prometheus/client_golang を
// 導入せず、必要なカウンター・ヒストグラムのみを自前で実装する。
// 出力形式は Prometheus 0.0.4 テキストフォーマットに準拠しており、
// Prometheus / VictoriaMetrics / Grafana Agent 等からそのままスクレイプできる。
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// 処理時間ヒストグラムのバケット上限（秒）。
// メール1通の処理は通常サブ秒〜数秒、ワーカータイムアウト時に数十秒となるため
// その範囲を粗くカバーする。
var durationBuckets = []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60}

// Metrics は smtp-gateway のランタイムメトリクスを保持する。
// 全メソッドは goroutine セーフである。
type Metrics struct {
	mu sync.Mutex

	version string

	// mailshield_mail_received_total{route}
	received map[string]uint64
	// mailshield_mail_action_total{route,action}
	actions map[[2]string]uint64
	// mailshield_mail_unrouted_total
	unrouted uint64
	// mailshield_mail_errors_total{stage}
	errors map[string]uint64
	// mailshield_inspect_detected_total{route,worker}
	detected map[[2]string]uint64

	// mailshield_mail_processing_seconds ヒストグラム
	durBucketCounts []uint64 // durationBuckets と同じ長さ（+Inf は durCount で表す）
	durSum          float64
	durCount        uint64
}

// New は初期化済みの Metrics を返す。version は build_info として公開される。
func New(version string) *Metrics {
	return &Metrics{
		version:         version,
		received:        make(map[string]uint64),
		actions:         make(map[[2]string]uint64),
		errors:          make(map[string]uint64),
		detected:        make(map[[2]string]uint64),
		durBucketCounts: make([]uint64, len(durationBuckets)),
	}
}

// IncReceived はルート解決に成功したメール受信を1カウントする。
func (m *Metrics) IncReceived(route string) {
	m.mu.Lock()
	m.received[route]++
	m.mu.Unlock()
}

// IncUnrouted はマッチするルートがなく拒否したメールを1カウントする。
func (m *Metrics) IncUnrouted() {
	m.mu.Lock()
	m.unrouted++
	m.mu.Unlock()
}

// IncAction はポリシー評価の結果実行されたアクションを1カウントする。
// 変換パイプライン失敗時の強制隔離も action="quarantine" として数える。
func (m *Metrics) IncAction(route, action string) {
	m.mu.Lock()
	m.actions[[2]string{route, action}]++
	m.mu.Unlock()
}

// IncError は処理段階（stage）ごとの失敗を1カウントする。
// stage の例: "storage_save", "policy", "no_rule"。
func (m *Metrics) IncError(stage string) {
	m.mu.Lock()
	m.errors[stage]++
	m.mu.Unlock()
}

// IncDetected は検査ワーカーが detected=true を返した回数を1カウントする。
func (m *Metrics) IncDetected(route, worker string) {
	m.mu.Lock()
	m.detected[[2]string{route, worker}]++
	m.mu.Unlock()
}

// ObserveProcessing はメール1通の処理時間（秒）をヒストグラムに記録する。
func (m *Metrics) ObserveProcessing(seconds float64) {
	m.mu.Lock()
	for i, upper := range durationBuckets {
		if seconds <= upper {
			m.durBucketCounts[i]++
		}
	}
	m.durSum += seconds
	m.durCount++
	m.mu.Unlock()
}

// Handler は GET /metrics 用の http.Handler を返す。
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprint(w, m.Render())
	})
}

// Render は Prometheus テキスト形式で全メトリクスを文字列化する。
func (m *Metrics) Render() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var b strings.Builder

	b.WriteString("# HELP mailshield_build_info ビルド情報（値は常に1）\n")
	b.WriteString("# TYPE mailshield_build_info gauge\n")
	fmt.Fprintf(&b, "mailshield_build_info{version=%q} 1\n", m.version)

	b.WriteString("# HELP mailshield_mail_received_total ルート解決に成功した受信メール数\n")
	b.WriteString("# TYPE mailshield_mail_received_total counter\n")
	for _, route := range sortedKeys1(m.received) {
		fmt.Fprintf(&b, "mailshield_mail_received_total{route=%q} %d\n", route, m.received[route])
	}

	b.WriteString("# HELP mailshield_mail_unrouted_total マッチするルートがなく拒否したメール数\n")
	b.WriteString("# TYPE mailshield_mail_unrouted_total counter\n")
	fmt.Fprintf(&b, "mailshield_mail_unrouted_total %d\n", m.unrouted)

	b.WriteString("# HELP mailshield_mail_action_total ポリシーアクション実行数\n")
	b.WriteString("# TYPE mailshield_mail_action_total counter\n")
	for _, k := range sortedKeys2(m.actions) {
		fmt.Fprintf(&b, "mailshield_mail_action_total{route=%q,action=%q} %d\n", k[0], k[1], m.actions[k])
	}

	b.WriteString("# HELP mailshield_mail_errors_total 処理段階ごとの失敗数\n")
	b.WriteString("# TYPE mailshield_mail_errors_total counter\n")
	for _, stage := range sortedKeys1(m.errors) {
		fmt.Fprintf(&b, "mailshield_mail_errors_total{stage=%q} %d\n", stage, m.errors[stage])
	}

	b.WriteString("# HELP mailshield_inspect_detected_total 検査ワーカーが detected=true を返した回数\n")
	b.WriteString("# TYPE mailshield_inspect_detected_total counter\n")
	for _, k := range sortedKeys2(m.detected) {
		fmt.Fprintf(&b, "mailshield_inspect_detected_total{route=%q,worker=%q} %d\n", k[0], k[1], m.detected[k])
	}

	b.WriteString("# HELP mailshield_mail_processing_seconds メール1通の処理時間（受信〜アクション実行）\n")
	b.WriteString("# TYPE mailshield_mail_processing_seconds histogram\n")
	for i, upper := range durationBuckets {
		fmt.Fprintf(&b, "mailshield_mail_processing_seconds_bucket{le=\"%g\"} %d\n", upper, m.durBucketCounts[i])
	}
	fmt.Fprintf(&b, "mailshield_mail_processing_seconds_bucket{le=\"+Inf\"} %d\n", m.durCount)
	fmt.Fprintf(&b, "mailshield_mail_processing_seconds_sum %g\n", m.durSum)
	fmt.Fprintf(&b, "mailshield_mail_processing_seconds_count %d\n", m.durCount)

	return b.String()
}

func sortedKeys1(m map[string]uint64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeys2(m map[[2]string]uint64) [][2]string {
	keys := make([][2]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	return keys
}
