package context

import "testing"

func TestCompressionMetricsRecordSnapshot(t *testing.T) {
	var m CompressionMetrics
	m.Record(CompressionResult{Compressed: true, OriginalBytes: 1000, CompressedBytes: 300, SavedBytes: 700, Strategy: "shell_summary"})
	m.Record(CompressionResult{Compressed: false, OriginalBytes: 10, CompressedBytes: 10})

	got := m.Snapshot()
	if got.CompressedItems != 1 || got.OriginalBytes != 1010 || got.CompressedBytes != 310 || got.SavedBytes != 700 {
		t.Fatalf("snapshot = %+v", got)
	}
	if got.SavingsRatio < 0.69 || got.SavingsRatio > 0.70 {
		t.Fatalf("SavingsRatio = %f", got.SavingsRatio)
	}
	if got.ByStrategy["shell_summary"] != 1 {
		t.Fatalf("ByStrategy = %+v", got.ByStrategy)
	}
}
