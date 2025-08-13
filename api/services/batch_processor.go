package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pocwhisp/models"
	"pocwhisp/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BatchProcessor handles batch processing of audio files
type BatchProcessor struct {
	db             *gorm.DB
	aiClient       *AIClient
	queueManager   *QueueManager
	cache          *MultiLevelCache
	degradationMgr *DegradationManager

	// Processing state
	activeJobs        map[string]*BatchJobContext
	maxConcurrentJobs int
	mutex             sync.RWMutex

	// Configuration
	defaultConfig    models.BatchJobConfig
	storageBasePath  string
	maxFileSize      int64
	supportedFormats []string

	// Monitoring
	metrics *MetricsCollector
	logger  *utils.Logger

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// BatchJobContext holds the context for a running batch job
type BatchJobContext struct {
	Job            *models.BatchJob
	Files          []*models.BatchJobFile
	ProcessingSem  chan struct{} // Semaphore for concurrent file processing
	CancelFunc     context.CancelFunc
	StartTime      time.Time
	LastUpdateTime time.Time
	CurrentFile    string
	Mutex          sync.RWMutex
}

// BatchProcessorConfig holds configuration for the batch processor
type BatchProcessorConfig struct {
	MaxConcurrentJobs int
	StorageBasePath   string
	MaxFileSize       int64
	SupportedFormats  []string
	DefaultConfig     models.BatchJobConfig
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(
	db *gorm.DB,
	aiClient *AIClient,
	queueManager *QueueManager,
	config BatchProcessorConfig,
) *BatchProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	if config.MaxConcurrentJobs <= 0 {
		config.MaxConcurrentJobs = 3
	}

	if config.MaxFileSize <= 0 {
		config.MaxFileSize = 500 * 1024 * 1024 // 500MB
	}

	if len(config.SupportedFormats) == 0 {
		config.SupportedFormats = []string{"wav", "mp3", "flac", "m4a"}
	}

	if config.StorageBasePath == "" {
		config.StorageBasePath = "/tmp/batch_processing"
	}

	// Ensure storage path exists
	os.MkdirAll(config.StorageBasePath, 0755)

	return &BatchProcessor{
		db:                db,
		aiClient:          aiClient,
		queueManager:      queueManager,
		cache:             GetCache(),
		degradationMgr:    GetDegradationManager(),
		activeJobs:        make(map[string]*BatchJobContext),
		maxConcurrentJobs: config.MaxConcurrentJobs,
		defaultConfig:     config.DefaultConfig,
		storageBasePath:   config.StorageBasePath,
		maxFileSize:       config.MaxFileSize,
		supportedFormats:  config.SupportedFormats,
		metrics:           GetMetrics(),
		logger:            utils.GetLogger(),
		ctx:               ctx,
		cancel:            cancel,
	}
}

// CreateBatchJob creates a new batch job
func (bp *BatchProcessor) CreateBatchJob(
	name, description string,
	jobType models.BatchJobType,
	priority models.BatchJobPriority,
	filePaths []string,
	config *models.BatchJobConfig,
	userID string,
) (*models.BatchJob, error) {
	// Use default config if none provided
	if config == nil {
		config = &bp.defaultConfig
	}

	// Validate files
	validFiles, err := bp.validateFiles(filePaths)
	if err != nil {
		return nil, fmt.Errorf("file validation failed: %w", err)
	}

	if len(validFiles) == 0 {
		return nil, fmt.Errorf("no valid files found")
	}

	// Create batch job
	job := &models.BatchJob{
		Name:        name,
		Description: description,
		Type:        jobType,
		Status:      models.BatchJobStatusPending,
		Priority:    priority,
		Config:      *config,
		TotalFiles:  len(validFiles),
		UserID:      userID,
		MaxRetries:  3,
	}

	// Start transaction
	tx := bp.db.Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", tx.Error)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Save job
	if err := tx.Create(job).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create batch job: %w", err)
	}

	// Create batch job files
	for i, filePath := range validFiles {
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to get file info for %s: %w", filePath, err)
		}

		// Get file duration (if possible)
		duration, _ := bp.getFileDuration(filePath)

		file := &models.BatchJobFile{
			BatchJobID:      job.ID,
			OriginalPath:    filePath,
			FileName:        filepath.Base(filePath),
			FileSize:        fileInfo.Size(),
			FileDuration:    duration,
			FileFormat:      strings.ToLower(filepath.Ext(filePath)[1:]),
			Status:          models.BatchFileStatusPending,
			ProcessingOrder: i + 1,
		}

		if err := tx.Create(file).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to create batch job file: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Log job creation
	if bp.logger != nil {
		bp.logger.LogAIService("batch_processor", "job_created", len(validFiles), 0, true, fmt.Sprintf("Job %s created with %d files", job.ID, len(validFiles)))
	}

	return job, nil
}

// StartBatchJob starts processing a batch job
func (bp *BatchProcessor) StartBatchJob(jobID string) error {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()

	// Check if job is already running
	if _, exists := bp.activeJobs[jobID]; exists {
		return fmt.Errorf("job %s is already running", jobID)
	}

	// Check concurrent job limit
	if len(bp.activeJobs) >= bp.maxConcurrentJobs {
		return fmt.Errorf("maximum concurrent jobs (%d) reached", bp.maxConcurrentJobs)
	}

	// Load job from database
	var job models.BatchJob
	if err := bp.db.Preload("Files").First(&job, "id = ?", jobID).Error; err != nil {
		return fmt.Errorf("failed to load job: %w", err)
	}

	// Validate job can be started
	if job.Status != models.BatchJobStatusPending {
		return fmt.Errorf("job status is %s, expected pending", job.Status)
	}

	// Create job context
	ctx, cancel := context.WithCancel(bp.ctx)
	jobContext := &BatchJobContext{
		Job:            &job,
		Files:          convertToPointerSlice(job.Files),
		ProcessingSem:  make(chan struct{}, job.Config.MaxConcurrentFiles),
		CancelFunc:     cancel,
		StartTime:      time.Now(),
		LastUpdateTime: time.Now(),
	}

	// Add to active jobs
	bp.activeJobs[jobID] = jobContext

	// Update job status
	now := time.Now()
	job.Status = models.BatchJobStatusRunning
	job.StartedAt = &now
	if err := bp.db.Save(&job).Error; err != nil {
		delete(bp.activeJobs, jobID)
		cancel()
		return fmt.Errorf("failed to update job status: %w", err)
	}

	// Start processing in background
	bp.wg.Add(1)
	go func() {
		defer bp.wg.Done()
		defer cancel()

		bp.processBatchJob(ctx, jobContext)

		// Remove from active jobs
		bp.mutex.Lock()
		delete(bp.activeJobs, jobID)
		bp.mutex.Unlock()
	}()

	return nil
}

// processBatchJob processes all files in a batch job
func (bp *BatchProcessor) processBatchJob(ctx context.Context, jobCtx *BatchJobContext) {
	job := jobCtx.Job

	defer func() {
		// Final job update
		bp.finalizeJob(job)
	}()

	// Process files with concurrency control
	var wg sync.WaitGroup

	for _, file := range jobCtx.Files {
		// Check for cancellation
		select {
		case <-ctx.Done():
			if bp.logger != nil {
				bp.logger.LogAIService("batch_processor", "job_cancelled", job.TotalFiles, 0, false, "Job cancelled")
			}
			return
		default:
		}

		// Acquire semaphore
		select {
		case jobCtx.ProcessingSem <- struct{}{}:
		case <-ctx.Done():
			return
		}

		wg.Add(1)
		go func(f *models.BatchJobFile) {
			defer wg.Done()
			defer func() { <-jobCtx.ProcessingSem }()

			bp.processFile(ctx, jobCtx, f)
		}(file)
	}

	// Wait for all files to complete
	wg.Wait()
}

// processFile processes a single file in a batch job
func (bp *BatchProcessor) processFile(ctx context.Context, jobCtx *BatchJobContext, file *models.BatchJobFile) {
	startTime := time.Now()

	// Update file status
	file.Status = models.BatchFileStatusProcessing
	file.StartedAt = &startTime
	bp.db.Save(file)

	// Update current file in job context
	jobCtx.Mutex.Lock()
	jobCtx.CurrentFile = file.FileName
	jobCtx.Mutex.Unlock()

	defer func() {
		// Update processing stats
		processingTime := time.Since(startTime)
		file.ProcessingTime = processingTime
		completedAt := time.Now()
		file.CompletedAt = &completedAt

		// Update job progress
		bp.updateJobProgress(jobCtx.Job)

		// Save file
		bp.db.Save(file)
	}()

	// Process based on job type
	var err error
	switch jobCtx.Job.Type {
	case models.BatchJobTypeTranscription:
		err = bp.processTranscription(ctx, file)
	case models.BatchJobTypeSummarization:
		err = bp.processSummarization(ctx, file)
	case models.BatchJobTypeFullProcessing:
		err = bp.processFullProcessing(ctx, file)
	case models.BatchJobTypeReprocessing:
		err = bp.processReprocessing(ctx, file)
	default:
		err = fmt.Errorf("unsupported job type: %s", jobCtx.Job.Type)
	}

	// Update file status based on result
	if err != nil {
		file.Status = models.BatchFileStatusFailed
		file.ErrorMessage = err.Error()

		// Update job stats
		jobCtx.Job.FailedFiles++

		if bp.logger != nil {
			bp.logger.LogError(err, "batch_file_processing", utils.LogContext{
				RequestID: jobCtx.Job.ID,
				Component: "batch_processor",
				Operation: "process_file",
			}, map[string]interface{}{
				"file_id":   file.ID,
				"file_name": file.FileName,
				"job_type":  jobCtx.Job.Type.String(),
			})
		}
	} else {
		file.Status = models.BatchFileStatusCompleted
		jobCtx.Job.SuccessfulFiles++

		if bp.logger != nil {
			bp.logger.LogAIService("batch_processor", "file_completed", 1, time.Since(startTime), true, file.FileName)
		}
	}

	// Update processed count
	jobCtx.Job.ProcessedFiles++

	// Record metrics
	if bp.metrics != nil {
		bp.metrics.RecordAudioProcessing("batch", file.Status.String(), time.Since(startTime))
	}
}

// processTranscription processes transcription for a file
func (bp *BatchProcessor) processTranscription(ctx context.Context, file *models.BatchJobFile) error {

	// Call AI service with retry
	retryConfig := AIServiceRetryConfig()
	retrier := NewRetrier(retryConfig)

	result, retryResult := retrier.ExecuteWithResultAndContext(ctx, func(ctx context.Context) (interface{}, error) {
		return bp.aiClient.TranscribeAudio(file.OriginalPath)
	})

	if retryResult.LastError != nil {
		return fmt.Errorf("transcription failed after %d attempts: %w", retryResult.Attempts, retryResult.LastError)
	}

	transcriptResponse, ok := result.(*models.TranscriptionResponse)
	if !ok {
		return fmt.Errorf("invalid transcription response type")
	}

	// Save transcription to database
	sessionID, err := bp.saveTranscriptionResult(file, transcriptResponse)
	if err != nil {
		return fmt.Errorf("failed to save transcription: %w", err)
	}

	file.SessionID = sessionID
	return nil
}

// processSummarization processes summarization for a file
func (bp *BatchProcessor) processSummarization(ctx context.Context, file *models.BatchJobFile) error {
	// Check if transcription exists
	if file.SessionID == "" {
		return fmt.Errorf("no transcription available for summarization")
	}

	// Get transcription
	var session models.AudioSession
	if err := bp.db.First(&session, "id = ?", file.SessionID).Error; err != nil {
		return fmt.Errorf("failed to load transcription: %w", err)
	}

	// Get transcript segments
	var segments []models.TranscriptSegmentDB
	if err := bp.db.Where("session_id = ?", session.ID).Find(&segments).Error; err != nil {
		return fmt.Errorf("failed to load transcript segments: %w", err)
	}

	// Convert to transcript text
	var transcriptText strings.Builder
	for _, segment := range segments {
		transcriptText.WriteString(fmt.Sprintf("[%s] %s\n", segment.Speaker, segment.Text))
	}

	// Call AI service for summarization
	retryConfig := AIServiceRetryConfig()
	retrier := NewRetrier(retryConfig)

	result, retryResult := retrier.ExecuteWithResultAndContext(ctx, func(ctx context.Context) (interface{}, error) {
		request := &SummarizationRequest{
			Text: transcriptText.String(),
		}
		return bp.aiClient.SummarizeTranscription(request)
	})

	if retryResult.LastError != nil {
		return fmt.Errorf("summarization failed after %d attempts: %w", retryResult.Attempts, retryResult.LastError)
	}

	summaryResponse, ok := result.(*SummaryResponse)
	if !ok {
		return fmt.Errorf("invalid summary response type")
	}

	// Save summary to database
	summaryID, err := bp.saveSummaryResult(file.SessionID, summaryResponse)
	if err != nil {
		return fmt.Errorf("failed to save summary: %w", err)
	}

	file.SummaryID = summaryID
	return nil
}

// processFullProcessing processes both transcription and summarization
func (bp *BatchProcessor) processFullProcessing(ctx context.Context, file *models.BatchJobFile) error {
	// First, process transcription
	if err := bp.processTranscription(ctx, file); err != nil {
		return fmt.Errorf("transcription failed: %w", err)
	}

	// Then, process summarization
	if err := bp.processSummarization(ctx, file); err != nil {
		return fmt.Errorf("summarization failed: %w", err)
	}

	return nil
}

// processReprocessing reprocesses a failed file
func (bp *BatchProcessor) processReprocessing(ctx context.Context, file *models.BatchJobFile) error {
	// Reset file status
	file.RetryCount++
	file.ErrorMessage = ""

	// Process based on what was originally requested
	return bp.processFullProcessing(ctx, file)
}

// Helper methods

// validateFiles validates the provided file paths
func (bp *BatchProcessor) validateFiles(filePaths []string) ([]string, error) {
	var validFiles []string

	for _, path := range filePaths {
		// Check if file exists
		fileInfo, err := os.Stat(path)
		if err != nil {
			if bp.logger != nil {
				bp.logger.LogError(err, "file_validation", utils.LogContext{
					Component: "batch_processor",
					Operation: "validate_files",
				}, map[string]interface{}{
					"file_path": path,
				})
			}
			continue
		}

		// Check file size
		if fileInfo.Size() > bp.maxFileSize {
			if bp.logger != nil {
				bp.logger.LogError(fmt.Errorf("file too large"), "file_validation", utils.LogContext{
					Component: "batch_processor",
					Operation: "validate_files",
				}, map[string]interface{}{
					"file_path": path,
					"file_size": fileInfo.Size(),
					"max_size":  bp.maxFileSize,
				})
			}
			continue
		}

		// Check file format
		ext := strings.ToLower(filepath.Ext(path)[1:])
		if !bp.isFormatSupported(ext) {
			if bp.logger != nil {
				bp.logger.LogError(fmt.Errorf("unsupported format"), "file_validation", utils.LogContext{
					Component: "batch_processor",
					Operation: "validate_files",
				}, map[string]interface{}{
					"file_path": path,
					"format":    ext,
				})
			}
			continue
		}

		validFiles = append(validFiles, path)
	}

	return validFiles, nil
}

// isFormatSupported checks if a file format is supported
func (bp *BatchProcessor) isFormatSupported(format string) bool {
	for _, supported := range bp.supportedFormats {
		if supported == format {
			return true
		}
	}
	return false
}

// getFileDuration gets the duration of an audio file (simplified)
func (bp *BatchProcessor) getFileDuration(filePath string) (float64, error) {
	// This is a simplified implementation
	// In practice, you'd use a library like ffprobe or similar
	return 0, nil
}

// saveTranscriptionResult saves transcription result to database
func (bp *BatchProcessor) saveTranscriptionResult(file *models.BatchJobFile, response *models.TranscriptionResponse) (string, error) {
	// Create audio session
	processedAt := time.Now()
	session := &models.AudioSession{
		Filename:    file.FileName,
		FileSize:    file.FileSize,
		Duration:    file.FileDuration,
		ProcessedAt: &processedAt,
		Status:      "completed",
	}

	if err := bp.db.Create(session).Error; err != nil {
		return "", err
	}

	// Save transcript segments
	for _, segment := range response.Transcript.Segments {
		dbSegment := &models.TranscriptSegmentDB{
			SessionID: session.ID,
			Speaker:   segment.Speaker,
			StartTime: segment.StartTime,
			EndTime:   segment.EndTime,
			Text:      segment.Text,
		}

		if err := bp.db.Create(dbSegment).Error; err != nil {
			return "", err
		}
	}

	return session.ID.String(), nil
}

// saveSummaryResult saves summary result to database
func (bp *BatchProcessor) saveSummaryResult(sessionID string, response *SummaryResponse) (string, error) {
	// Parse sessionID to UUID
	sessionUUID, err := uuid.Parse(sessionID)
	if err != nil {
		return "", fmt.Errorf("invalid session ID: %w", err)
	}

	summary := &models.SummaryDB{
		SessionID: sessionUUID,
		Summary:   response.Summary,
		KeyPoints: strings.Join(response.KeyPoints, "\n"),
		CreatedAt: time.Now(),
	}

	if err := bp.db.Create(summary).Error; err != nil {
		return "", err
	}

	return summary.ID.String(), nil
}

// updateJobProgress updates the progress of a batch job
func (bp *BatchProcessor) updateJobProgress(job *models.BatchJob) {
	job.UpdateProgress()
	bp.db.Save(job)

	// Update cache with progress
	if bp.cache != nil {
		progress := job.ToProgress()
		cacheKey := fmt.Sprintf("batch_job_progress:%s", job.ID)
		bp.cache.Set(cacheKey, progress, 1*time.Hour)
	}
}

// finalizeJob finalizes a batch job
func (bp *BatchProcessor) finalizeJob(job *models.BatchJob) {
	now := time.Now()
	job.CompletedAt = &now
	job.TotalDuration = time.Since(*job.StartedAt)

	// Determine final status
	if job.FailedFiles == 0 {
		job.Status = models.BatchJobStatusCompleted
	} else if job.SuccessfulFiles == 0 {
		job.Status = models.BatchJobStatusFailed
	} else {
		job.Status = models.BatchJobStatusPartial
	}

	// Final progress update
	job.UpdateProgress()

	// Save to database
	bp.db.Save(job)

	// Log completion
	if bp.logger != nil {
		bp.logger.LogAIService("batch_processor", "job_completed", job.TotalFiles, job.TotalDuration,
			job.Status == models.BatchJobStatusCompleted, fmt.Sprintf("Completed: %d/%d successful", job.SuccessfulFiles, job.TotalFiles))
	}

	// Record final metrics
	if bp.metrics != nil {
		bp.metrics.RecordAudioProcessing("batch", job.Status.String(), job.TotalDuration)
	}
}

// Public API methods

// GetBatchJob gets a batch job by ID
func (bp *BatchProcessor) GetBatchJob(jobID string) (*models.BatchJob, error) {
	var job models.BatchJob
	if err := bp.db.Preload("Files").First(&job, "id = ?", jobID).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

// GetBatchJobProgress gets the current progress of a batch job
func (bp *BatchProcessor) GetBatchJobProgress(jobID string) (*models.BatchJobProgress, error) {
	// Try cache first
	if bp.cache != nil {
		cacheKey := fmt.Sprintf("batch_job_progress:%s", jobID)
		if cached, found := bp.cache.Get(cacheKey); found {
			if progress, ok := cached.(models.BatchJobProgress); ok {
				return &progress, nil
			}
		}
	}

	// Get from database
	job, err := bp.GetBatchJob(jobID)
	if err != nil {
		return nil, err
	}

	progress := job.ToProgress()

	// Update current file if job is running
	bp.mutex.RLock()
	if jobCtx, exists := bp.activeJobs[jobID]; exists {
		jobCtx.Mutex.RLock()
		progress.CurrentFile = jobCtx.CurrentFile
		jobCtx.Mutex.RUnlock()
	}
	bp.mutex.RUnlock()

	return &progress, nil
}

// ListBatchJobs lists batch jobs with optional filtering
func (bp *BatchProcessor) ListBatchJobs(userID string, status models.BatchJobStatus, jobType models.BatchJobType, limit, offset int) ([]models.BatchJobSummary, error) {
	query := bp.db.Model(&models.BatchJob{})

	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if jobType != "" {
		query = query.Where("type = ?", jobType)
	}

	if limit <= 0 {
		limit = 50
	}

	var jobs []models.BatchJob
	if err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&jobs).Error; err != nil {
		return nil, err
	}

	summaries := make([]models.BatchJobSummary, len(jobs))
	for i, job := range jobs {
		summaries[i] = job.ToSummary()
	}

	return summaries, nil
}

// CancelBatchJob cancels a running batch job
func (bp *BatchProcessor) CancelBatchJob(jobID string) error {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()

	// Check if job is running
	jobCtx, exists := bp.activeJobs[jobID]
	if !exists {
		// Job not running, update database status
		return bp.db.Model(&models.BatchJob{}).Where("id = ?", jobID).Update("status", models.BatchJobStatusCancelled).Error
	}

	// Cancel the job context
	jobCtx.CancelFunc()

	// Update job status
	jobCtx.Job.Status = models.BatchJobStatusCancelled
	now := time.Now()
	jobCtx.Job.CompletedAt = &now

	return bp.db.Save(jobCtx.Job).Error
}

// RetryBatchJob retries a failed batch job
func (bp *BatchProcessor) RetryBatchJob(jobID string) error {
	job, err := bp.GetBatchJob(jobID)
	if err != nil {
		return err
	}

	if !job.CanRetry() {
		return fmt.Errorf("job cannot be retried: status=%s, retries=%d/%d",
			job.Status, job.RetryCount, job.MaxRetries)
	}

	// Reset job for retry
	job.Status = models.BatchJobStatusPending
	job.RetryCount++
	job.ProcessedFiles = 0
	job.SuccessfulFiles = 0
	job.FailedFiles = 0
	job.StartedAt = nil
	job.CompletedAt = nil
	job.EstimatedETA = nil
	job.LastError = ""

	// Reset failed files
	if err := bp.db.Model(&models.BatchJobFile{}).
		Where("batch_job_id = ? AND status IN ?", jobID, []string{
			string(models.BatchFileStatusFailed),
			string(models.BatchFileStatusCancelled),
		}).
		Updates(map[string]interface{}{
			"status":        models.BatchFileStatusPending,
			"error_message": "",
			"started_at":    nil,
			"completed_at":  nil,
			"retry_count":   gorm.Expr("retry_count + 1"),
		}).Error; err != nil {
		return err
	}

	// Save job
	if err := bp.db.Save(job).Error; err != nil {
		return err
	}

	// Start the job
	return bp.StartBatchJob(jobID)
}

// Shutdown gracefully shuts down the batch processor
func (bp *BatchProcessor) Shutdown() error {
	bp.cancel()
	bp.wg.Wait()
	return nil
}

// Helper functions

// convertToPointerSlice converts []BatchJobFile to []*BatchJobFile
func convertToPointerSlice(files []models.BatchJobFile) []*models.BatchJobFile {
	result := make([]*models.BatchJobFile, len(files))
	for i := range files {
		result[i] = &files[i]
	}
	return result
}
