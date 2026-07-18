package rulestats

import (
	"sync"
	"testing"
)

func TestCounter_IncAndSnapshot(t *testing.T) {
	c := New()
	c.Inc("inbound", "av_detected")
	c.Inc("inbound", "av_detected")
	c.Inc("inbound", "default")
	c.Inc("outbound", "dlp")
	c.Inc("inbound", "") // 空ルールは無視

	snap := c.Snapshot()
	if snap["inbound"]["av_detected"] != 2 {
		t.Errorf("av_detected = %d, want 2", snap["inbound"]["av_detected"])
	}
	if snap["inbound"]["default"] != 1 {
		t.Errorf("default = %d, want 1", snap["inbound"]["default"])
	}
	if snap["outbound"]["dlp"] != 1 {
		t.Errorf("dlp = %d, want 1", snap["outbound"]["dlp"])
	}
	if _, ok := snap["inbound"][""]; ok {
		t.Error("空ルールが記録された")
	}
}

func TestCounter_Ensure(t *testing.T) {
	c := New()
	c.Ensure("inbound", "never_hit")
	snap := c.Snapshot()
	if v, ok := snap["inbound"]["never_hit"]; !ok || v != 0 {
		t.Errorf("Ensure したルールは 0 件で現れるべき: %v ok=%v", v, ok)
	}
	// Ensure は既存カウントを潰さない
	c.Inc("inbound", "hit")
	c.Ensure("inbound", "hit")
	if c.Snapshot()["inbound"]["hit"] != 1 {
		t.Error("Ensure が既存カウントを 0 に戻した")
	}
}

func TestCounter_SnapshotIsCopy(t *testing.T) {
	c := New()
	c.Inc("r", "x")
	snap := c.Snapshot()
	snap["r"]["x"] = 999
	if c.Snapshot()["r"]["x"] != 1 {
		t.Error("Snapshot が内部状態を共有している")
	}
}

func TestCounter_Concurrent(t *testing.T) {
	c := New()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Inc("r", "x")
		}()
	}
	wg.Wait()
	if c.Snapshot()["r"]["x"] != 100 {
		t.Errorf("並行 Inc で欠損: %d", c.Snapshot()["r"]["x"])
	}
}
