// Package rulestats はポリシールールの発火（ヒット）件数をプロセス内で集計する。
// 管理 UI がルールの棚卸し（当たっていないルール・全部を飲み込むルール）を可視化する用途。
// カウントはプロセス起動時からの累積で、再起動でリセットされる。
package rulestats

import "sync"

// Counter はルート×ルール単位のヒット件数を保持するスレッドセーフなカウンタ。
type Counter struct {
	mu   sync.Mutex
	hits map[string]map[string]int64 // route -> rule -> count
}

// New は空の Counter を返す。
func New() *Counter {
	return &Counter{hits: make(map[string]map[string]int64)}
}

// Inc は指定ルート・ルールのヒット件数を 1 増やす。rule が空の場合は何もしない。
func (c *Counter) Inc(route, rule string) {
	if rule == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.hits[route]
	if m == nil {
		m = make(map[string]int64)
		c.hits[route] = m
	}
	m[rule]++
}

// Ensure は指定ルート・ルールのエントリを（未登録なら 0 で）用意する。
// リロード時に「一度も当たっていないルール」を UI に 0 件として表示するために使う。
func (c *Counter) Ensure(route, rule string) {
	if rule == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.hits[route]
	if m == nil {
		m = make(map[string]int64)
		c.hits[route] = m
	}
	if _, ok := m[rule]; !ok {
		m[rule] = 0
	}
}

// Snapshot は現在のカウントのディープコピーを返す（JSON 公開用）。
func (c *Counter) Snapshot() map[string]map[string]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]map[string]int64, len(c.hits))
	for route, rules := range c.hits {
		m := make(map[string]int64, len(rules))
		for rule, n := range rules {
			m[rule] = n
		}
		out[route] = m
	}
	return out
}
