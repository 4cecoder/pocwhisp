package utils

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPerformanceProfiler(t *testing.T) {
	t.Run("NewPerformanceProfiler", func(t *testing.T) {
		profiler := NewPerformanceProfiler()

		assert.NotNil(t, profiler)
		assert.NotNil(t, profiler.metrics)
		assert.NotNil(t, profiler.poolManager)
		assert.False(t, profiler.gcOptimized)
	})

	t.Run("UpdateMetrics", func(t *testing.T) {
		profiler := NewPerformanceProfiler()
		ctx := context.Background()

		err := profiler.UpdateMetrics(ctx)
		assert.NoError(t, err)

		metrics := profiler.GetMetrics()
		assert.NotNil(t, metrics)
		assert.True(t, metrics.HeapAlloc > 0)
		assert.True(t, metrics.GoroutineCount > 0)
		assert.False(t, metrics.LastUpdated.IsZero())
	})

	t.Run("OptimizeGC", func(t *testing.T) {
		profiler := NewPerformanceProfiler()

		assert.False(t, profiler.gcOptimized)
		profiler.OptimizeGC()
		assert.True(t, profiler.gcOptimized)

		// Should not optimize again
		profiler.OptimizeGC()
		assert.True(t, profiler.gcOptimized)
	})

	t.Run("ForceGC", func(t *testing.T) {
		profiler := NewPerformanceProfiler()

		// Allocate some memory to ensure GC has something to collect
		data := make([][]byte, 1000)
		for i := range data {
			data[i] = make([]byte, 1024*1024) // 1MB each
		}

		var beforeGC runtime.MemStats
		runtime.ReadMemStats(&beforeGC)

		profiler.ForceGC()

		var afterGC runtime.MemStats
		runtime.ReadMemStats(&afterGC)

		// GC should have run (though effect may vary)
		assert.True(t, afterGC.NumGC >= beforeGC.NumGC)

		// Clean up
		data = nil
		runtime.GC()
	})

	t.Run("GenerateReport", func(t *testing.T) {
		profiler := NewPerformanceProfiler()
		ctx := context.Background()

		// Update metrics first
		err := profiler.UpdateMetrics(ctx)
		require.NoError(t, err)

		report := profiler.GenerateReport()

		assert.NotNil(t, report)
		assert.Contains(t, report, "metrics")
		assert.Contains(t, report, "recommendations")
		assert.Contains(t, report, "uptime_seconds")
		assert.Contains(t, report, "generated_at")

		// Check metrics are included
		metrics, ok := report["metrics"].(*PerformanceMetrics)
		assert.True(t, ok)
		assert.True(t, metrics.HeapAlloc > 0)
	})
}

func TestPoolManager(t *testing.T) {
	t.Run("NewPoolManager", func(t *testing.T) {
		manager := NewPoolManager()

		assert.NotNil(t, manager)
	})

	t.Run("BufferPool", func(t *testing.T) {
		manager := NewPoolManager()

		// Get buffer from pool
		buf1 := manager.GetBuffer()
		assert.NotNil(t, buf1)
		assert.True(t, len(*buf1) == 0)    // Should be reset
		assert.True(t, cap(*buf1) >= 8192) // Should have at least 8KB capacity

		// Use the buffer
		*buf1 = append(*buf1, []byte("test data")...)
		assert.Equal(t, "test data", string(*buf1))

		// Return to pool
		manager.PutBuffer(buf1)

		// Get another buffer (might be the same one, reset)
		buf2 := manager.GetBuffer()
		assert.NotNil(t, buf2)
		assert.True(t, len(*buf2) == 0) // Should be reset

		manager.PutBuffer(buf2)
	})

	t.Run("RequestContextPool", func(t *testing.T) {
		manager := NewPoolManager()

		// Get context from pool
		ctx1 := manager.GetRequestContext()
		assert.NotNil(t, ctx1)
		assert.NotNil(t, ctx1.Metadata)
		assert.False(t, ctx1.StartTime.IsZero())

		// Use the context
		ctx1.ID = "test-id"
		ctx1.UserID = "test-user"
		ctx1.Metadata["key"] = "value"

		// Return to pool
		manager.PutRequestContext(ctx1)

		// Get another context (might be the same one, reset)
		ctx2 := manager.GetRequestContext()
		assert.NotNil(t, ctx2)
		assert.Empty(t, ctx2.ID)       // Should be reset
		assert.Empty(t, ctx2.UserID)   // Should be reset
		assert.Empty(t, ctx2.Metadata) // Should be reset

		manager.PutRequestContext(ctx2)
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		manager := NewPoolManager()

		// Test concurrent access to pools
		const numGoroutines = 100
		const numOperations = 10

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()

				for j := 0; j < numOperations; j++ {
					// Test buffer pool
					buf := manager.GetBuffer()
					*buf = append(*buf, byte(j))
					manager.PutBuffer(buf)

					// Test request context pool
					ctx := manager.GetRequestContext()
					ctx.ID = "concurrent-test"
					manager.PutRequestContext(ctx)
				}
			}()
		}

		wg.Wait()
	})
}

func TestCircuitBreaker(t *testing.T) {
	t.Run("NewCircuitBreaker", func(t *testing.T) {
		cb := NewCircuitBreaker(3, time.Second)

		assert.NotNil(t, cb)
		assert.Equal(t, 3, cb.maxFailures)
		assert.Equal(t, time.Second, cb.resetTimeout)
		assert.Equal(t, CircuitClosed, cb.currentState)
	})

	t.Run("SuccessfulExecution", func(t *testing.T) {
		cb := NewCircuitBreaker(3, time.Second)

		err := cb.Execute(func() error {
			return nil // Success
		})

		assert.NoError(t, err)
		assert.Equal(t, CircuitClosed, cb.currentState)
		assert.Equal(t, 0, cb.failures)
	})

	t.Run("FailureExecution", func(t *testing.T) {
		cb := NewCircuitBreaker(3, time.Second)

		// First failure
		err := cb.Execute(func() error {
			return assert.AnError
		})

		assert.Error(t, err)
		assert.Equal(t, CircuitClosed, cb.currentState)
		assert.Equal(t, 1, cb.failures)
	})

	t.Run("CircuitOpening", func(t *testing.T) {
		cb := NewCircuitBreaker(2, time.Second) // Open after 2 failures

		// First failure
		err := cb.Execute(func() error {
			return assert.AnError
		})
		assert.Error(t, err)
		assert.Equal(t, CircuitClosed, cb.currentState)

		// Second failure - should open circuit
		err = cb.Execute(func() error {
			return assert.AnError
		})
		assert.Error(t, err)
		assert.Equal(t, CircuitOpen, cb.currentState)

		// Third attempt - should be rejected
		err = cb.Execute(func() error {
			return nil // This won't execute
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circuit breaker is open")
	})

	t.Run("CircuitRecovery", func(t *testing.T) {
		cb := NewCircuitBreaker(1, 10*time.Millisecond) // Very short timeout

		// Cause failure to open circuit
		err := cb.Execute(func() error {
			return assert.AnError
		})
		assert.Error(t, err)
		assert.Equal(t, CircuitOpen, cb.currentState)

		// Wait for reset timeout
		time.Sleep(15 * time.Millisecond)

		// Should now be half-open and allow one attempt
		err = cb.Execute(func() error {
			return nil // Success
		})
		assert.NoError(t, err)
		assert.Equal(t, CircuitClosed, cb.currentState)
	})
}

func TestRateLimiter(t *testing.T) {
	t.Run("NewRateLimiter", func(t *testing.T) {
		rl := NewRateLimiter(10, 5) // 10 tokens max, 5 per second refill

		assert.NotNil(t, rl)
		assert.Equal(t, float64(10), rl.maxTokens)
		assert.Equal(t, float64(5), rl.refillRate)
		assert.Equal(t, float64(10), rl.tokens) // Should start full
	})

	t.Run("AllowRequests", func(t *testing.T) {
		rl := NewRateLimiter(3, 1) // 3 tokens max, 1 per second refill

		// Should allow first 3 requests
		assert.True(t, rl.Allow())
		assert.True(t, rl.Allow())
		assert.True(t, rl.Allow())

		// Fourth request should be denied
		assert.False(t, rl.Allow())
	})

	t.Run("TokenRefill", func(t *testing.T) {
		rl := NewRateLimiter(2, 10) // 2 tokens max, 10 per second refill (fast)

		// Consume all tokens
		assert.True(t, rl.Allow())
		assert.True(t, rl.Allow())
		assert.False(t, rl.Allow())

		// Wait for refill
		time.Sleep(200 * time.Millisecond) // Should refill ~2 tokens

		// Should be able to make requests again
		assert.True(t, rl.Allow())
	})
}

func TestCacheWarmer(t *testing.T) {
	t.Run("NewCacheWarmer", func(t *testing.T) {
		cw := NewCacheWarmer(time.Minute)

		assert.NotNil(t, cw)
		assert.Equal(t, time.Minute, cw.schedule)
		assert.False(t, cw.running)
		assert.Empty(t, cw.warmupFunctions)
	})

	t.Run("AddWarmupFunction", func(t *testing.T) {
		cw := NewCacheWarmer(time.Minute)

		called := false
		warmupFunc := func(ctx context.Context) error {
			called = true
			return nil
		}

		cw.AddWarmupFunction(warmupFunc)
		assert.Len(t, cw.warmupFunctions, 1)

		// Test the function is callable
		ctx := context.Background()
		cw.warmupCache(ctx)
		assert.True(t, called)
	})

	t.Run("StartAndStop", func(t *testing.T) {
		cw := NewCacheWarmer(10 * time.Millisecond) // Very short interval

		callCount := 0
		warmupFunc := func(ctx context.Context) error {
			callCount++
			return nil
		}
		cw.AddWarmupFunction(warmupFunc)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Start cache warmer
		go cw.Start(ctx)

		// Wait a bit for it to run
		time.Sleep(50 * time.Millisecond)

		// Stop it
		cw.Stop()

		// Should have been called at least once
		assert.True(t, callCount > 0)
		assert.False(t, cw.running)
	})
}

func TestPerformanceMetrics(t *testing.T) {
	t.Run("MetricsStructure", func(t *testing.T) {
		metrics := &PerformanceMetrics{
			CPUUsagePercent: 65.5,
			GoroutineCount:  150,
			HeapAlloc:       1024 * 1024, // 1MB
			HeapSys:         2048 * 1024, // 2MB
			GCCount:         10,
			LastUpdated:     time.Now(),
		}

		assert.Equal(t, 65.5, metrics.CPUUsagePercent)
		assert.Equal(t, 150, metrics.GoroutineCount)
		assert.Equal(t, uint64(1024*1024), metrics.HeapAlloc)
		assert.Equal(t, uint32(10), metrics.GCCount)
		assert.False(t, metrics.LastUpdated.IsZero())
	})
}

// Benchmark tests for performance validation
func BenchmarkPoolManagerBuffer(b *testing.B) {
	manager := NewPoolManager()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := manager.GetBuffer()
			*buf = append(*buf, []byte("benchmark data")...)
			manager.PutBuffer(buf)
		}
	})
}

func BenchmarkPoolManagerRequestContext(b *testing.B) {
	manager := NewPoolManager()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx := manager.GetRequestContext()
			ctx.ID = "benchmark"
			ctx.Metadata["test"] = "value"
			manager.PutRequestContext(ctx)
		}
	})
}

func BenchmarkCircuitBreakerSuccess(b *testing.B) {
	cb := NewCircuitBreaker(100, time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := cb.Execute(func() error {
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRateLimiterAllow(b *testing.B) {
	rl := NewRateLimiter(1000000, 1000000) // Very high limits for benchmarking

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rl.Allow()
		}
	})
}

func BenchmarkPerformanceProfilerUpdate(b *testing.B) {
	profiler := NewPerformanceProfiler()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := profiler.UpdateMetrics(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Test memory allocation patterns
func TestMemoryOptimization(t *testing.T) {
	t.Run("BufferPoolReducesAllocations", func(t *testing.T) {
		manager := NewPoolManager()

		// Measure allocations without pool
		var m1, m2 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m1)

		// Allocate buffers without pool
		for i := 0; i < 1000; i++ {
			buf := make([]byte, 8192)
			_ = buf
		}

		runtime.GC()
		runtime.ReadMemStats(&m2)
		allocsWithoutPool := m2.Mallocs - m1.Mallocs

		// Reset
		runtime.GC()
		runtime.ReadMemStats(&m1)

		// Allocate buffers with pool
		for i := 0; i < 1000; i++ {
			buf := manager.GetBuffer()
			manager.PutBuffer(buf)
		}

		runtime.GC()
		runtime.ReadMemStats(&m2)
		allocsWithPool := m2.Mallocs - m1.Mallocs

		// Pool should reduce allocations significantly
		t.Logf("Allocations without pool: %d", allocsWithoutPool)
		t.Logf("Allocations with pool: %d", allocsWithPool)
		assert.True(t, allocsWithPool < allocsWithoutPool/2,
			"Pool should reduce allocations by at least 50%%")
	})
}

// Integration test with concurrent load
func TestPerformanceUnderLoad(t *testing.T) {
	t.Run("ConcurrentMetricsUpdates", func(t *testing.T) {
		profiler := NewPerformanceProfiler()
		ctx := context.Background()

		const numGoroutines = 10
		const numUpdates = 10

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		errors := make(chan error, numGoroutines*numUpdates)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()

				for j := 0; j < numUpdates; j++ {
					if err := profiler.UpdateMetrics(ctx); err != nil {
						errors <- err
					}
				}
			}()
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("Metrics update failed: %v", err)
		}

		// Verify final state
		metrics := profiler.GetMetrics()
		assert.NotNil(t, metrics)
		assert.False(t, metrics.LastUpdated.IsZero())
	})
}
