package trading

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics tracks trade execution latency
type Metrics struct {
	// Latency samples (in milliseconds)
	samples   []int64
	sampleIdx int
	mu        sync.Mutex

	// Counters
	totalTrades   atomic.Int64
	successTrades atomic.Int64
	failedTrades  atomic.Int64

	// Component breakdown (last trade)
	lastParseMs   atomic.Int64
	lastResolveMs atomic.Int64
	lastQuoteMs   atomic.Int64
	lastSignMs    atomic.Int64
	lastSendMs    atomic.Int64
	lastTotalMs   atomic.Int64
}

// NewMetrics creates a new metrics tracker
func NewMetrics() *Metrics {
	return &Metrics{
		samples: make([]int64, 100), // Keep last 100 samples
	}
}

// RecordTrade records a trade execution with component breakdown
func (m *Metrics) RecordTrade(success bool, parseMs, resolveMs, quoteMs, signMs, sendMs int64) {
	totalMs := parseMs + resolveMs + quoteMs + signMs + sendMs

	m.mu.Lock()
	m.samples[m.sampleIdx%len(m.samples)] = totalMs
	m.sampleIdx++
	m.mu.Unlock()

	m.totalTrades.Add(1)
	if success {
		m.successTrades.Add(1)
	} else {
		m.failedTrades.Add(1)
	}

	m.lastParseMs.Store(parseMs)
	m.lastResolveMs.Store(resolveMs)
	m.lastQuoteMs.Store(quoteMs)
	m.lastSignMs.Store(signMs)
	m.lastSendMs.Store(sendMs)
	m.lastTotalMs.Store(totalMs)
}

// RecordLatency records just total latency
func (m *Metrics) RecordLatency(latencyMs int64) {
	m.mu.Lock()
	m.samples[m.sampleIdx%len(m.samples)] = latencyMs
	m.sampleIdx++
	m.mu.Unlock()
}

// P50 returns the 50th percentile latency
func (m *Metrics) P50() int64 {
	return m.percentile(50)
}

// P95 returns the 95th percentile latency
func (m *Metrics) P95() int64 {
	return m.percentile(95)
}

// P99 returns the 99th percentile latency
func (m *Metrics) P99() int64 {
	return m.percentile(99)
}

// Avg returns the average latency
func (m *Metrics) Avg() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := m.sampleIdx
	if count > len(m.samples) {
		count = len(m.samples)
	}
	if count == 0 {
		return 0
	}

	var sum int64
	for i := 0; i < count; i++ {
		sum += m.samples[i]
	}
	return sum / int64(count)
}

func (m *Metrics) percentile(p int) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := m.sampleIdx
	if count > len(m.samples) {
		count = len(m.samples)
	}
	if count == 0 {
		return 0
	}

	// Copy and sort
	sorted := make([]int64, count)
	copy(sorted, m.samples[:count])
	
	// Simple bubble sort for small arrays
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			if sorted[j] > sorted[j+1] {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	idx := (p * count) / 100
	if idx >= count {
		idx = count - 1
	}
	return sorted[idx]
}

// LastBreakdown returns last trade's component latency breakdown
func (m *Metrics) LastBreakdown() (parse, resolve, quote, sign, send, total int64) {
	return m.lastParseMs.Load(),
		m.lastResolveMs.Load(),
		m.lastQuoteMs.Load(),
		m.lastSignMs.Load(),
		m.lastSendMs.Load(),
		m.lastTotalMs.Load()
}

// Stats returns aggregate stats
func (m *Metrics) Stats() (total, success, failed int64, successRate float64) {
	total = m.totalTrades.Load()
	success = m.successTrades.Load()
	failed = m.failedTrades.Load()
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
	}
	return
}

// TradeTimer helps time individual trade components
type TradeTimer struct {
	start     time.Time
	parseEnd  time.Time
	resolveEnd time.Time
	quoteEnd  time.Time
	signEnd   time.Time
	sendEnd   time.Time
}

// NewTradeTimer starts timing a trade
func NewTradeTimer() *TradeTimer {
	return &TradeTimer{start: time.Now()}
}

// MarkParseDone marks signal parsing complete
func (t *TradeTimer) MarkParseDone() {
	t.parseEnd = time.Now()
}

// MarkResolveDone marks token resolution complete
func (t *TradeTimer) MarkResolveDone() {
	t.resolveEnd = time.Now()
}

// MarkQuoteDone marks Jupiter quote complete
func (t *TradeTimer) MarkQuoteDone() {
	t.quoteEnd = time.Now()
}

// MarkSignDone marks TX signing complete
func (t *TradeTimer) MarkSignDone() {
	t.signEnd = time.Now()
}

// MarkSendDone marks TX send complete
func (t *TradeTimer) MarkSendDone() {
	t.sendEnd = time.Now()
}

// GetBreakdown returns milliseconds for each phase
func (t *TradeTimer) GetBreakdown() (parse, resolve, quote, sign, send int64) {
	if !t.parseEnd.IsZero() {
		parse = t.parseEnd.Sub(t.start).Milliseconds()
	}
	if !t.resolveEnd.IsZero() {
		resolve = t.resolveEnd.Sub(t.parseEnd).Milliseconds()
	}
	if !t.quoteEnd.IsZero() {
		quote = t.quoteEnd.Sub(t.resolveEnd).Milliseconds()
	}
	if !t.signEnd.IsZero() {
		sign = t.signEnd.Sub(t.quoteEnd).Milliseconds()
	}
	if !t.sendEnd.IsZero() {
		send = t.sendEnd.Sub(t.signEnd).Milliseconds()
	}
	return
}

// TotalMs returns total elapsed time in milliseconds
func (t *TradeTimer) TotalMs() int64 {
	return time.Since(t.start).Milliseconds()
}
