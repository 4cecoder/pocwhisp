package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// JobPriority defines job priority levels
type JobPriority int

const (
	PriorityLow JobPriority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

// JobStatus represents the current status of a job
type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusProcessing JobStatus = "processing"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
	JobStatusRetrying   JobStatus = "retrying"
	JobStatusCancelled  JobStatus = "cancelled"
)

// JobType defines different types of jobs
type JobType string

const (
	JobTypeTranscription     JobType = "transcription"
	JobTypeSummarization     JobType = "summarization"
	JobTypeBatchProcessing   JobType = "batch_processing"
	JobTypeModelLoading      JobType = "model_loading"
	JobTypeChannelSeparation JobType = "channel_separation"
	JobTypeHealthCheck       JobType = "health_check"
)

// Job represents a job in the queue
type Job struct {
	ID          string                 `json:"id"`
	Type        JobType                `json:"type"`
	Priority    JobPriority            `json:"priority"`
	Status      JobStatus              `json:"status"`
	Payload     map[string]interface{} `json:"payload"`
	Result      map[string]interface{} `json:"result,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Attempts    int                    `json:"attempts"`
	MaxAttempts int                    `json:"max_attempts"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	WorkerID    string                 `json:"worker_id,omitempty"`
	Timeout     time.Duration          `json:"timeout"`
	RetryDelay  time.Duration          `json:"retry_delay"`
}

// JobProcessor defines the interface for processing jobs
type JobProcessor interface {
	ProcessJob(ctx context.Context, job *Job) error
	GetJobTypes() []JobType
}

// QueueManager manages Redis-based job queues
type QueueManager struct {
	redis   *redis.Client
	workers map[string]*Worker
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	config  *QueueConfig
}

// QueueConfig holds configuration for the queue manager
type QueueConfig struct {
	RedisURL           string
	MaxWorkers         int
	DefaultTimeout     time.Duration
	DefaultRetryDelay  time.Duration
	DefaultMaxAttempts int
	CleanupInterval    time.Duration
	JobTTL             time.Duration
	MetricsEnabled     bool
}

// Worker represents a job worker
type Worker struct {
	ID        string
	Queue     string
	Processor JobProcessor
	manager   *QueueManager
	ctx       context.Context
	cancel    context.CancelFunc
	running   bool
	mu        sync.Mutex
}

// QueueStats holds queue statistics
type QueueStats struct {
	TotalJobs      int64                 `json:"total_jobs"`
	PendingJobs    int64                 `json:"pending_jobs"`
	ProcessingJobs int64                 `json:"processing_jobs"`
	CompletedJobs  int64                 `json:"completed_jobs"`
	FailedJobs     int64                 `json:"failed_jobs"`
	ActiveWorkers  int                   `json:"active_workers"`
	JobsByType     map[JobType]int64     `json:"jobs_by_type"`
	JobsByPriority map[JobPriority]int64 `json:"jobs_by_priority"`
	Throughput     float64               `json:"throughput_per_minute"`
	AvgProcessTime float64               `json:"avg_process_time_seconds"`
}

// Redis key patterns
const (
	KeyJobQueue      = "pocwhisp:queue:{priority}:{type}"
	KeyJobData       = "pocwhisp:job:{id}"
	KeyJobStatus     = "pocwhisp:job_status:{id}"
	KeyWorkerActive  = "pocwhisp:worker:active:{id}"
	KeyQueueStats    = "pocwhisp:queue:stats"
	KeyProcessingSet = "pocwhisp:processing"
	KeyCompletedSet  = "pocwhisp:completed"
	KeyFailedSet     = "pocwhisp:failed"
)

// NewQueueManager creates a new queue manager
func NewQueueManager(config *QueueConfig) (*QueueManager, error) {
	if config == nil {
		config = DefaultQueueConfig()
	}

	// Parse Redis URL
	opt, err := redis.ParseURL(config.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid Redis URL: %w", err)
	}

	client := redis.NewClient(opt)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	ctx, cancel = context.WithCancel(context.Background())

	qm := &QueueManager{
		redis:   client,
		workers: make(map[string]*Worker),
		ctx:     ctx,
		cancel:  cancel,
		config:  config,
	}

	// Start background tasks
	qm.wg.Add(1)
	go qm.cleanupExpiredJobs()

	if config.MetricsEnabled {
		qm.wg.Add(1)
		go qm.updateMetrics()
	}

	log.Printf("Queue manager initialized with Redis at %s", config.RedisURL)
	return qm, nil
}

// DefaultQueueConfig returns default configuration
func DefaultQueueConfig() *QueueConfig {
	return &QueueConfig{
		RedisURL:           "redis://localhost:6379",
		MaxWorkers:         10,
		DefaultTimeout:     5 * time.Minute,
		DefaultRetryDelay:  30 * time.Second,
		DefaultMaxAttempts: 3,
		CleanupInterval:    10 * time.Minute,
		JobTTL:             24 * time.Hour,
		MetricsEnabled:     true,
	}
}

// EnqueueJob adds a job to the queue
func (qm *QueueManager) EnqueueJob(jobType JobType, priority JobPriority, payload map[string]interface{}) (*Job, error) {
	job := &Job{
		ID:          uuid.New().String(),
		Type:        jobType,
		Priority:    priority,
		Status:      JobStatusPending,
		Payload:     payload,
		Attempts:    0,
		MaxAttempts: qm.config.DefaultMaxAttempts,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Timeout:     qm.config.DefaultTimeout,
		RetryDelay:  qm.config.DefaultRetryDelay,
	}

	// Set custom parameters from payload
	if maxAttempts, ok := payload["max_attempts"].(int); ok {
		job.MaxAttempts = maxAttempts
	}

	if timeout, ok := payload["timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			job.Timeout = d
		}
	}

	// Store job data
	jobData, err := json.Marshal(job)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job: %w", err)
	}

	pipe := qm.redis.Pipeline()

	// Store job data with TTL
	jobKey := fmt.Sprintf(KeyJobData, job.ID)
	pipe.Set(qm.ctx, jobKey, jobData, qm.config.JobTTL)

	// Add to priority queue
	queueKey := fmt.Sprintf(KeyJobQueue, int(priority), string(jobType))
	score := float64(time.Now().Unix())
	pipe.ZAdd(qm.ctx, queueKey, &redis.Z{Score: score, Member: job.ID})

	// Update stats
	pipe.HIncrBy(qm.ctx, KeyQueueStats, "total_jobs", 1)
	pipe.HIncrBy(qm.ctx, KeyQueueStats, "pending_jobs", 1)
	pipe.HIncrBy(qm.ctx, KeyQueueStats, fmt.Sprintf("jobs_by_type:%s", jobType), 1)
	pipe.HIncrBy(qm.ctx, KeyQueueStats, fmt.Sprintf("jobs_by_priority:%d", priority), 1)

	if _, err := pipe.Exec(qm.ctx); err != nil {
		return nil, fmt.Errorf("failed to enqueue job: %w", err)
	}

	log.Printf("Job %s (%s, priority %d) enqueued", job.ID, job.Type, job.Priority)
	return job, nil
}

// GetJob retrieves a job by ID
func (qm *QueueManager) GetJob(jobID string) (*Job, error) {
	jobKey := fmt.Sprintf(KeyJobData, jobID)
	data, err := qm.redis.Get(qm.ctx, jobKey).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	var job Job
	if err := json.Unmarshal([]byte(data), &job); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job: %w", err)
	}

	return &job, nil
}

// UpdateJobStatus updates the status of a job
func (qm *QueueManager) UpdateJobStatus(jobID string, status JobStatus, result map[string]interface{}, errorMsg string) error {
	job, err := qm.GetJob(jobID)
	if err != nil {
		return err
	}

	job.Status = status
	job.UpdatedAt = time.Now()

	if result != nil {
		job.Result = result
	}

	if errorMsg != "" {
		job.Error = errorMsg
	}

	switch status {
	case JobStatusProcessing:
		now := time.Now()
		job.StartedAt = &now
	case JobStatusCompleted, JobStatusFailed, JobStatusCancelled:
		now := time.Now()
		job.CompletedAt = &now
	}

	// Store updated job
	jobData, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	pipe := qm.redis.Pipeline()

	jobKey := fmt.Sprintf(KeyJobData, jobID)
	pipe.Set(qm.ctx, jobKey, jobData, qm.config.JobTTL)

	// Update status counters
	switch status {
	case JobStatusProcessing:
		pipe.HIncrBy(qm.ctx, KeyQueueStats, "pending_jobs", -1)
		pipe.HIncrBy(qm.ctx, KeyQueueStats, "processing_jobs", 1)
		pipe.SAdd(qm.ctx, KeyProcessingSet, jobID)
	case JobStatusCompleted:
		pipe.HIncrBy(qm.ctx, KeyQueueStats, "processing_jobs", -1)
		pipe.HIncrBy(qm.ctx, KeyQueueStats, "completed_jobs", 1)
		pipe.SRem(qm.ctx, KeyProcessingSet, jobID)
		pipe.SAdd(qm.ctx, KeyCompletedSet, jobID)
	case JobStatusFailed:
		pipe.HIncrBy(qm.ctx, KeyQueueStats, "processing_jobs", -1)
		pipe.HIncrBy(qm.ctx, KeyQueueStats, "failed_jobs", 1)
		pipe.SRem(qm.ctx, KeyProcessingSet, jobID)
		pipe.SAdd(qm.ctx, KeyFailedSet, jobID)
	}

	if _, err := pipe.Exec(qm.ctx); err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	log.Printf("Job %s status updated to %s", jobID, status)
	return nil
}

// StartWorker starts a new worker for processing jobs
func (qm *QueueManager) StartWorker(queue string, processor JobProcessor) (*Worker, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	workerID := uuid.New().String()
	ctx, cancel := context.WithCancel(qm.ctx)

	worker := &Worker{
		ID:        workerID,
		Queue:     queue,
		Processor: processor,
		manager:   qm,
		ctx:       ctx,
		cancel:    cancel,
		running:   true,
	}

	qm.workers[workerID] = worker

	// Register worker as active
	workerKey := fmt.Sprintf(KeyWorkerActive, workerID)
	qm.redis.Set(qm.ctx, workerKey, time.Now().Unix(), 5*time.Minute)

	// Start worker goroutine
	qm.wg.Add(1)
	go worker.start()

	log.Printf("Worker %s started for queue %s", workerID, queue)
	return worker, nil
}

// StopWorker stops a worker
func (qm *QueueManager) StopWorker(workerID string) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	worker, exists := qm.workers[workerID]
	if !exists {
		return fmt.Errorf("worker not found: %s", workerID)
	}

	worker.stop()
	delete(qm.workers, workerID)

	// Remove worker from active set
	workerKey := fmt.Sprintf(KeyWorkerActive, workerID)
	qm.redis.Del(qm.ctx, workerKey)

	log.Printf("Worker %s stopped", workerID)
	return nil
}

// GetStats returns queue statistics
func (qm *QueueManager) GetStats() (*QueueStats, error) {
	stats := &QueueStats{
		JobsByType:     make(map[JobType]int64),
		JobsByPriority: make(map[JobPriority]int64),
	}

	// Get basic stats
	result, err := qm.redis.HGetAll(qm.ctx, KeyQueueStats).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	for key, value := range result {
		val, _ := strconv.ParseInt(value, 10, 64)
		switch key {
		case "total_jobs":
			stats.TotalJobs = val
		case "pending_jobs":
			stats.PendingJobs = val
		case "processing_jobs":
			stats.ProcessingJobs = val
		case "completed_jobs":
			stats.CompletedJobs = val
		case "failed_jobs":
			stats.FailedJobs = val
		}
	}

	// Get active workers count
	qm.mu.RLock()
	stats.ActiveWorkers = len(qm.workers)
	qm.mu.RUnlock()

	// Calculate throughput (completed jobs in last minute)
	// This is a simplified calculation - in production, you'd use a time series
	if stats.CompletedJobs > 0 {
		stats.Throughput = float64(stats.CompletedJobs) // Simplified
	}

	return stats, nil
}

// Shutdown gracefully shuts down the queue manager
func (qm *QueueManager) Shutdown() error {
	log.Println("Shutting down queue manager...")

	// Stop all workers
	qm.mu.Lock()
	for workerID := range qm.workers {
		qm.StopWorker(workerID)
	}
	qm.mu.Unlock()

	// Cancel context
	qm.cancel()

	// Wait for all goroutines
	qm.wg.Wait()

	// Close Redis connection
	if err := qm.redis.Close(); err != nil {
		return fmt.Errorf("failed to close Redis connection: %w", err)
	}

	log.Println("Queue manager shut down complete")
	return nil
}

// Worker methods

// start begins the worker's job processing loop
func (w *Worker) start() {
	defer w.manager.wg.Done()
	log.Printf("Worker %s started processing", w.ID)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			log.Printf("Worker %s stopping", w.ID)
			return
		case <-ticker.C:
			if err := w.processNextJob(); err != nil {
				log.Printf("Worker %s error: %v", w.ID, err)
				time.Sleep(5 * time.Second) // Backoff on error
			}
		}
	}
}

// processNextJob gets and processes the next job from the queue
func (w *Worker) processNextJob() error {
	// Get supported job types
	jobTypes := w.Processor.GetJobTypes()
	if len(jobTypes) == 0 {
		return fmt.Errorf("no job types supported by processor")
	}

	// Try to get a job from queues (highest priority first)
	var jobID string
	var found bool

	for priority := PriorityCritical; priority >= PriorityLow; priority-- {
		for _, jobType := range jobTypes {
			queueKey := fmt.Sprintf(KeyJobQueue, int(priority), string(jobType))

			// Pop job from queue (atomic operation)
			result, err := w.manager.redis.ZPopMin(w.ctx, queueKey, 1).Result()
			if err == redis.Nil || len(result) == 0 {
				continue // No jobs in this queue
			}
			if err != nil {
				return fmt.Errorf("failed to pop job from queue: %w", err)
			}

			jobID = result[0].Member.(string)
			found = true
			break
		}
		if found {
			break
		}
	}

	if !found {
		return nil // No jobs available
	}

	// Get job details
	job, err := w.manager.GetJob(jobID)
	if err != nil {
		log.Printf("Failed to get job %s: %v", jobID, err)
		return nil
	}

	// Update job status to processing
	job.WorkerID = w.ID
	if err := w.manager.UpdateJobStatus(jobID, JobStatusProcessing, nil, ""); err != nil {
		log.Printf("Failed to update job status for %s: %v", jobID, err)
		return nil
	}

	// Process the job
	log.Printf("Worker %s processing job %s (%s)", w.ID, jobID, job.Type)
	startTime := time.Now()

	// Create timeout context
	processingCtx, cancel := context.WithTimeout(w.ctx, job.Timeout)
	defer cancel()

	err = w.Processor.ProcessJob(processingCtx, job)
	processingTime := time.Since(startTime)

	if err != nil {
		job.Attempts++
		if job.Attempts >= job.MaxAttempts {
			// Max attempts reached, mark as failed
			w.manager.UpdateJobStatus(jobID, JobStatusFailed, nil, err.Error())
			log.Printf("Job %s failed after %d attempts: %v", jobID, job.Attempts, err)
		} else {
			// Retry job
			log.Printf("Job %s failed (attempt %d/%d), retrying: %v", jobID, job.Attempts, job.MaxAttempts, err)

			// Re-enqueue with delay
			go func() {
				time.Sleep(job.RetryDelay)
				queueKey := fmt.Sprintf(KeyJobQueue, int(job.Priority), string(job.Type))
				score := float64(time.Now().Unix())
				w.manager.redis.ZAdd(w.ctx, queueKey, &redis.Z{Score: score, Member: jobID})
				w.manager.UpdateJobStatus(jobID, JobStatusRetrying, nil, err.Error())
			}()
		}
	} else {
		// Job completed successfully
		result := map[string]interface{}{
			"processing_time": processingTime.Seconds(),
			"worker_id":       w.ID,
		}
		w.manager.UpdateJobStatus(jobID, JobStatusCompleted, result, "")
		log.Printf("Job %s completed successfully in %v", jobID, processingTime)
	}

	return nil
}

// stop stops the worker
func (w *Worker) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		w.cancel()
		w.running = false
	}
}

// Background tasks

// cleanupExpiredJobs removes expired jobs from Redis
func (qm *QueueManager) cleanupExpiredJobs() {
	defer qm.wg.Done()

	ticker := time.NewTicker(qm.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-qm.ctx.Done():
			return
		case <-ticker.C:
			qm.performCleanup()
		}
	}
}

// performCleanup performs the actual cleanup
func (qm *QueueManager) performCleanup() {
	// Clean up completed jobs older than TTL
	cutoff := time.Now().Add(-qm.config.JobTTL).Unix()

	// Remove old jobs from completed set
	removed, err := qm.redis.ZRemRangeByScore(qm.ctx, KeyCompletedSet, "0", fmt.Sprintf("%d", cutoff)).Result()
	if err == nil && removed > 0 {
		log.Printf("Cleaned up %d old completed jobs", removed)
	}

	// Remove old jobs from failed set
	removed, err = qm.redis.ZRemRangeByScore(qm.ctx, KeyFailedSet, "0", fmt.Sprintf("%d", cutoff)).Result()
	if err == nil && removed > 0 {
		log.Printf("Cleaned up %d old failed jobs", removed)
	}

	// Clean up inactive workers
	workerKeys, err := qm.redis.Keys(qm.ctx, "pocwhisp:worker:active:*").Result()
	if err == nil {
		for _, key := range workerKeys {
			lastSeen, err := qm.redis.Get(qm.ctx, key).Int64()
			if err == nil && time.Now().Unix()-lastSeen > 300 { // 5 minutes
				qm.redis.Del(qm.ctx, key)
			}
		}
	}
}

// updateMetrics updates queue metrics
func (qm *QueueManager) updateMetrics() {
	defer qm.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-qm.ctx.Done():
			return
		case <-ticker.C:
			// Update worker heartbeat
			qm.mu.RLock()
			for workerID := range qm.workers {
				workerKey := fmt.Sprintf(KeyWorkerActive, workerID)
				qm.redis.Set(qm.ctx, workerKey, time.Now().Unix(), 5*time.Minute)
			}
			qm.mu.RUnlock()
		}
	}
}
