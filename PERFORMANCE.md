# PocWhisp Performance Optimization Guide

This comprehensive guide covers performance optimization, monitoring, and tuning for the PocWhisp API in production environments.

## Table of Contents

1. [Overview](#overview)
2. [Performance Architecture](#performance-architecture)
3. [Monitoring & Metrics](#monitoring--metrics)
4. [Optimization Strategies](#optimization-strategies)
5. [Performance Testing](#performance-testing)
6. [Production Tuning](#production-tuning)
7. [Troubleshooting](#troubleshooting)
8. [Best Practices](#best-practices)

## Overview

The PocWhisp API includes comprehensive performance optimization features designed for high-throughput, low-latency production environments. The performance system provides:

- **Real-time Performance Monitoring**: CPU, memory, network, and application metrics
- **Automatic Optimization**: Garbage collection tuning, connection pooling, request optimization
- **Resource Management**: Object pooling, buffer management, and memory optimization
- **Intelligent Caching**: Multi-level caching with hit rate optimization
- **Load Balancing**: Request distribution and circuit breaker patterns
- **Performance Profiling**: Detailed analysis and bottleneck identification

## Performance Architecture

### Core Components

```
┌─────────────────────────────────────────────────────────────┐
│                    Performance Layer                        │
├─────────────────────┬─────────────────────┬─────────────────┤
│  Performance        │  Optimization       │  Monitoring     │
│  Profiler          │  Engine             │  Dashboard      │
├─────────────────────┼─────────────────────┼─────────────────┤
│  • Metrics Collection│  • GC Optimization  │  • Real-time    │
│  • System Analysis  │  • Pool Management  │    Metrics      │
│  • Trend Detection  │  • Cache Tuning     │  • Alerting     │
│  • Reporting        │  • Circuit Breakers │  • Reporting    │
└─────────────────────┴─────────────────────┴─────────────────┘
```

### Performance Middleware Stack

1. **Request Optimization**: Connection keep-alive, compression, buffering
2. **Cache Layer**: Response caching, ETag validation, CDN integration
3. **Circuit Breakers**: Failure protection and graceful degradation
4. **Rate Limiting**: Token bucket algorithm with burst capacity
5. **Resource Pooling**: Buffer pools, connection pools, object pools
6. **Metrics Collection**: Prometheus integration and custom metrics

## Monitoring & Metrics

### System Metrics

The performance system tracks comprehensive metrics across multiple layers:

```yaml
# CPU Metrics
cpu_usage_percent: 65.2
goroutine_count: 1247
thread_count: 16

# Memory Metrics
heap_alloc_bytes: 134217728      # 128MB
heap_sys_bytes: 268435456        # 256MB
heap_inuse_bytes: 142606336      # 136MB
gc_count: 145
gc_pause_avg_ns: 2500000         # 2.5ms

# Request Metrics
active_requests: 23
request_latency_p95_ms: 45.2
request_latency_p99_ms: 89.7
throughput_rps: 1250

# Database Metrics
db_connections: 8
db_idle_connections: 2
db_queries_count: 15420
db_slow_queries_count: 3

# Cache Metrics
cache_hit_rate: 0.87
cache_size_bytes: 67108864       # 64MB
```

### Performance Endpoints

#### Get Current Metrics
```bash
GET /api/v1/performance/metrics
Authorization: Bearer <token>
```

#### System Health Check
```bash
GET /api/v1/performance/health
Authorization: Bearer <token>
```

#### Performance Report
```bash
GET /api/v1/performance/report?period=24h&format=json
Authorization: Bearer <token>
```

#### Trigger Optimization
```bash
POST /api/v1/performance/optimize?type=all
Authorization: Bearer <token>
```

### Using the Performance CLI

```bash
# Analyze current performance
./scripts/optimize_performance.py --mode analyze

# Apply optimizations automatically
./scripts/optimize_performance.py --mode optimize --auto-apply

# Monitor performance for 10 minutes
./scripts/optimize_performance.py --mode monitor --duration 600

# Generate comprehensive report
./scripts/optimize_performance.py --mode report --period 24h --output report.json
```

## Optimization Strategies

### 1. Memory Optimization

#### Garbage Collection Tuning
```go
// Automatic GC optimization
profiler := utils.NewPerformanceProfiler()
profiler.OptimizeGC()

// Manual GC tuning
debug.SetGCPercent(200)  // More relaxed GC for high memory systems
debug.SetMemoryLimit(8 * 1024 * 1024 * 1024)  // 8GB limit
```

#### Object Pooling
```go
// Use pooled buffers for request processing
poolManager := utils.NewPoolManager()
buffer := poolManager.GetBuffer()
defer poolManager.PutBuffer(buffer)

// Pooled request contexts
ctx := poolManager.GetRequestContext()
defer poolManager.PutRequestContext(ctx)
```

### 2. Request Optimization

#### Connection Pool Tuning
```go
config := &services.PerformanceConfig{
    MaxOpenConns:    100,
    MaxIdleConns:    10,
    ConnMaxLifetime: time.Hour,
    ConnMaxIdleTime: 10 * time.Minute,
}
```

#### Request Processing Pool
```go
// Submit requests to optimized processing pool
err := performanceService.SubmitRequest(ctx, "transcription", 1, func(ctx context.Context) error {
    // Request processing logic
    return processTranscriptionRequest(ctx)
})
```

### 3. Caching Optimization

#### Multi-Level Cache Configuration
```go
cacheConfig := &CacheConfig{
    HitRateTarget:       0.85,
    WarmupSchedule:      5 * time.Minute,
    CompressionEnabled:  true,
    TTLOptimization:     true,
}
```

#### Cache Warming
```go
cacheWarmer := utils.NewCacheWarmer(5 * time.Minute)
cacheWarmer.AddWarmupFunction(func(ctx context.Context) error {
    // Preload frequently accessed data
    return warmupFrequentlyAccessedData(ctx)
})
```

### 4. Circuit Breaker Pattern

```go
circuitBreaker := utils.NewCircuitBreaker(5, 30*time.Second)

err := circuitBreaker.Execute(func() error {
    // Call external service
    return callExternalService()
})

if err != nil {
    // Handle circuit breaker protection
    return handleCircuitBreakerError(err)
}
```

### 5. Response Optimization

#### Compression Middleware
```go
app.Use(middleware.PerformanceMiddleware(middleware.PerformanceConfig{
    CompressionEnabled: true,
    CompressionLevel:   6,
    CompressionMinSize: 1024,
    CompressionTypes:   []string{"application/json", "text/html"},
}))
```

## Performance Testing

### Load Testing with Locust

#### Basic Load Test
```bash
locust -f tests/load_test.py --host=http://localhost:8080 \
       --users 50 --spawn-rate 5 --run-time 300s --headless
```

#### Stress Testing
```bash
locust -f tests/load_test.py --host=http://localhost:8080 \
       --users 200 --spawn-rate 20 --run-time 120s --headless
```

#### Performance Benchmarking
```bash
# Run Go benchmarks
go test -bench=. -benchmem ./...

# Profile memory usage
go test -bench=BenchmarkProcessAudio -memprofile=mem.prof
go tool pprof mem.prof

# Profile CPU usage
go test -bench=BenchmarkProcessAudio -cpuprofile=cpu.prof
go tool pprof cpu.prof
```

### Performance Targets

| Metric | Target | Warning | Critical |
|--------|--------|---------|----------|
| Response Time (P95) | <100ms | >500ms | >1000ms |
| Throughput | >1000 RPS | <500 RPS | <100 RPS |
| CPU Usage | <70% | >80% | >90% |
| Memory Usage | <80% | >90% | >95% |
| Cache Hit Rate | >85% | <70% | <50% |
| Error Rate | <1% | >2% | >5% |

## Production Tuning

### Environment Configuration

#### Development
```yaml
optimization:
  gc:
    target_percentage: 50    # More aggressive GC
  database:
    max_open_conns: 25      # Lower connection count
  monitoring:
    metrics_update_interval: "10s"  # Frequent updates
```

#### Production
```yaml
optimization:
  gc:
    target_percentage: 200   # Relaxed GC
  database:
    max_open_conns: 100     # Higher connection count
  monitoring:
    metrics_update_interval: "30s"  # Balanced updates
```

### Container Resource Limits

```yaml
# Kubernetes deployment
resources:
  requests:
    cpu: "500m"
    memory: "512Mi"
  limits:
    cpu: "2000m"
    memory: "2Gi"

# Environment variables
env:
  - name: GOGC
    value: "200"
  - name: GOMEMLIMIT
    value: "1800MiB"  # 90% of memory limit
```

### Database Optimization

```sql
-- Index optimization for frequent queries
CREATE INDEX CONCURRENTLY idx_audio_sessions_created_at 
ON audio_sessions(created_at);

CREATE INDEX CONCURRENTLY idx_transcript_segments_session_id 
ON transcript_segment_dbs(session_id);

-- Query optimization
ANALYZE audio_sessions;
ANALYZE transcript_segment_dbs;
ANALYZE summary_dbs;
```

## Troubleshooting

### Common Performance Issues

#### High Memory Usage
```bash
# Check for memory leaks
go tool pprof http://localhost:8080/debug/pprof/heap

# Force garbage collection
curl -X POST http://localhost:8080/api/v1/performance/optimize?type=gc
```

#### High CPU Usage
```bash
# Profile CPU usage
go tool pprof http://localhost:8080/debug/pprof/profile

# Check goroutine count
go tool pprof http://localhost:8080/debug/pprof/goroutine
```

#### Slow Response Times
```bash
# Analyze request traces
curl -H "X-Trace: true" http://localhost:8080/api/v1/transcribe

# Check database query performance
./scripts/optimize_performance.py --mode analyze | grep -A 5 "Database"
```

#### Cache Performance Issues
```bash
# Check cache hit rate
curl http://localhost:8080/api/v1/cache/stats

# Warm up cache
curl -X POST http://localhost:8080/api/v1/cache/warmup
```

### Performance Debugging

#### Enable Profiling
```go
import _ "net/http/pprof"

go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

#### Access Profiling Endpoints
```bash
# CPU profile
go tool pprof http://localhost:6060/debug/pprof/profile

# Memory profile
go tool pprof http://localhost:6060/debug/pprof/heap

# Goroutine profile
go tool pprof http://localhost:6060/debug/pprof/goroutine

# Block profile
go tool pprof http://localhost:6060/debug/pprof/block

# Mutex profile
go tool pprof http://localhost:6060/debug/pprof/mutex
```

## Best Practices

### 1. Memory Management
- Use object pools for frequently allocated objects
- Minimize allocations in hot paths
- Set appropriate garbage collection targets
- Monitor heap usage and growth patterns
- Use memory limits to prevent OOM conditions

### 2. Concurrency
- Limit goroutine creation with worker pools
- Use channels for communication, not shared memory
- Implement proper context cancellation
- Monitor goroutine count and lifecycle
- Use sync.Pool for expensive object creation

### 3. Database Performance
- Use connection pooling with appropriate limits
- Implement query timeouts and cancellation
- Monitor slow queries and optimize indexes
- Use prepared statements for repeated queries
- Implement proper transaction management

### 4. Caching Strategy
- Cache at multiple levels (L1, L2, CDN)
- Implement cache warming for critical data
- Use appropriate TTL values
- Monitor cache hit rates and size
- Implement cache invalidation strategies

### 5. Monitoring & Alerting
- Set up comprehensive metrics collection
- Implement proactive alerting thresholds
- Use distributed tracing for request flow
- Monitor business metrics alongside technical metrics
- Implement automated performance testing

### 6. Capacity Planning
- Establish performance baselines
- Plan for traffic growth patterns
- Implement horizontal scaling capabilities
- Monitor resource utilization trends
- Test disaster recovery scenarios

## Performance Optimization Checklist

### Pre-deployment
- [ ] Run comprehensive load tests
- [ ] Profile memory and CPU usage
- [ ] Optimize database queries and indexes
- [ ] Configure appropriate resource limits
- [ ] Set up monitoring and alerting
- [ ] Test circuit breaker functionality
- [ ] Validate cache performance
- [ ] Review garbage collection settings

### Post-deployment
- [ ] Monitor real-world performance metrics
- [ ] Analyze user traffic patterns
- [ ] Optimize based on production data
- [ ] Scale resources as needed
- [ ] Update performance baselines
- [ ] Review and adjust alert thresholds
- [ ] Conduct regular performance reviews
- [ ] Plan capacity for future growth

---

For questions about performance optimization or to report performance issues, please refer to the project documentation or create an issue in the repository.
