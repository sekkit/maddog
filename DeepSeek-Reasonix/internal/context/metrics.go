package context

import "sync"

type CompressionMetrics struct {
	mu              sync.Mutex
	items           int
	compressedItems int
	originalBytes   int
	compressedBytes int
	savedBytes      int
	byStrategy      map[string]int
}

type CompressionMetricsSnapshot struct {
	Items           int            `json:"items"`
	CompressedItems int            `json:"compressedItems"`
	OriginalBytes   int            `json:"originalBytes"`
	CompressedBytes int            `json:"compressedBytes"`
	SavedBytes      int            `json:"savedBytes"`
	SavingsRatio    float64        `json:"savingsRatio"`
	ByStrategy      map[string]int `json:"byStrategy,omitempty"`
}

func (m *CompressionMetrics) Record(r CompressionResult) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byStrategy == nil {
		m.byStrategy = map[string]int{}
	}
	m.items++
	m.originalBytes += r.OriginalBytes
	if r.CompressedBytes > 0 {
		m.compressedBytes += r.CompressedBytes
	} else {
		m.compressedBytes += r.OriginalBytes
	}
	if r.Compressed {
		m.compressedItems++
		if r.Strategy != "" {
			m.byStrategy[r.Strategy]++
		}
	}
	m.savedBytes += r.SavedBytes
}

func (m *CompressionMetrics) Snapshot() CompressionMetricsSnapshot {
	if m == nil {
		return CompressionMetricsSnapshot{ByStrategy: map[string]int{}}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	byStrategy := make(map[string]int, len(m.byStrategy))
	for k, v := range m.byStrategy {
		byStrategy[k] = v
	}
	ratio := 0.0
	if m.originalBytes > 0 {
		ratio = float64(m.savedBytes) / float64(m.originalBytes)
	}
	return CompressionMetricsSnapshot{
		Items:           m.items,
		CompressedItems: m.compressedItems,
		OriginalBytes:   m.originalBytes,
		CompressedBytes: m.compressedBytes,
		SavedBytes:      m.savedBytes,
		SavingsRatio:    ratio,
		ByStrategy:      byStrategy,
	}
}
