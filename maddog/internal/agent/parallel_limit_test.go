package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunParallelHonorsLimit(t *testing.T) {
	const limit = 2
	var cur, peak, ran int32
	run := func(int) {
		atomic.AddInt32(&ran, 1)
		n := atomic.AddInt32(&cur, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&cur, -1)
	}
	ranUntil := runParallel(context.Background(), limit, 0, 8, run)
	if ranUntil != 8 {
		t.Fatalf("ranUntil = %d, want 8", ranUntil)
	}
	if got := atomic.LoadInt32(&ran); got != 8 {
		t.Fatalf("ran %d calls, want 8", got)
	}
	if p := atomic.LoadInt32(&peak); p > limit {
		t.Errorf("peak concurrency %d exceeds limit %d", p, limit)
	}
}

func TestRunParallelZeroLimitUsesDefault(t *testing.T) {
	var ran int32
	ranUntil := runParallel(context.Background(), 0, 0, 4, func(int) { atomic.AddInt32(&ran, 1) })
	if ranUntil != 4 {
		t.Fatalf("ranUntil = %d, want 4", ranUntil)
	}
	if got := atomic.LoadInt32(&ran); got != 4 {
		t.Fatalf("ran %d calls, want 4", got)
	}
}
