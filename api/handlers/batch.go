package handlers

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pocwhisp/models"
	"pocwhisp/services"
	"pocwhisp/utils"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// BatchHandler handles batch processing endpoints
type BatchHandler struct {
	db             *gorm.DB
	batchProcessor *services.BatchProcessor
	cache          *services.MultiLevelCache
	logger         *utils.Logger
}

// NewBatchHandler creates a new batch handler
func NewBatchHandler(db *gorm.DB, batchProcessor *services.BatchProcessor) *BatchHandler {
	return &BatchHandler{
		db:             db,
		batchProcessor: batchProcessor,
		cache:          services.GetCache(),
		logger:         utils.GetLogger(),
	}
}

// CreateBatchJobRequest represents the request to create a batch job
type CreateBatchJobRequest struct {
	Name        string                  `json:"name" validate:"required,min=1,max=255"`
	Description string                  `json:"description" validate:"max=1000"`
	Type        models.BatchJobType     `json:"type" validate:"required"`
	Priority    models.BatchJobPriority `json:"priority" validate:"min=1,max=10"`
	FilePaths   []string                `json:"file_paths" validate:"required,min=1"`
	Config      *models.BatchJobConfig  `json:"config,omitempty"`
	UserID      string                  `json:"user_id,omitempty"`
	Tags        []string                `json:"tags,omitempty"`
	Metadata    map[string]interface{}  `json:"metadata,omitempty"`
}

// BatchJobResponse represents the response when creating/getting a batch job
type BatchJobResponse struct {
	ID              string                  `json:"id"`
	Name            string                  `json:"name"`
	Description     string                  `json:"description"`
	Type            models.BatchJobType     `json:"type"`
	Status          models.BatchJobStatus   `json:"status"`
	Priority        models.BatchJobPriority `json:"priority"`
	Config          models.BatchJobConfig   `json:"config"`
	Progress        float64                 `json:"progress"`
	TotalFiles      int                     `json:"total_files"`
	ProcessedFiles  int                     `json:"processed_files"`
	SuccessfulFiles int                     `json:"successful_files"`
	FailedFiles     int                     `json:"failed_files"`
	StartedAt       *time.Time              `json:"started_at"`
	CompletedAt     *time.Time              `json:"completed_at"`
	EstimatedETA    *time.Time              `json:"estimated_eta"`
	ThroughputFPS   float64                 `json:"throughput_fps"`
	TotalDuration   time.Duration           `json:"total_duration"`
	LastError       string                  `json:"last_error,omitempty"`
	RetryCount      int                     `json:"retry_count"`
	MaxRetries      int                     `json:"max_retries"`
	UserID          string                  `json:"user_id,omitempty"`
	Tags            []string                `json:"tags,omitempty"`
	Metadata        map[string]interface{}  `json:"metadata,omitempty"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

// CreateBatchJob creates a new batch job
func (bh *BatchHandler) CreateBatchJob(c *fiber.Ctx) error {
	var req CreateBatchJobRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "invalid_request",
			Message:   "Invalid request body",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Validate job type
	if !req.Type.IsValid() {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "invalid_job_type",
			Message:   "Invalid job type",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Validate priority
	if req.Priority < 1 || req.Priority > 10 {
		req.Priority = models.BatchJobPriorityMedium
	}

	// Use default user ID if not provided
	if req.UserID == "" {
		req.UserID = "anonymous"
	}

	// Create batch job
	job, err := bh.batchProcessor.CreateBatchJob(
		req.Name,
		req.Description,
		req.Type,
		req.Priority,
		req.FilePaths,
		req.Config,
		req.UserID,
	)
	if err != nil {
		bh.logError(c, err, "create_batch_job")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "creation_failed",
			Message:   "Failed to create batch job: " + err.Error(),
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Add tags and metadata if provided
	if len(req.Tags) > 0 {
		tagsJSON, _ := json.Marshal(req.Tags)
		job.Tags = string(tagsJSON)
	}

	if len(req.Metadata) > 0 {
		metadataJSON, _ := json.Marshal(req.Metadata)
		job.Metadata = string(metadataJSON)
		bh.db.Save(job)
	}

	// Convert to response
	response := bh.jobToResponse(job, req.Tags, req.Metadata)

	// Log creation
	if bh.logger != nil {
		bh.logger.LogAIService("batch_handler", "job_created", job.TotalFiles, 0, true, fmt.Sprintf("Created job %s", job.ID))
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"status":     "success",
		"message":    "Batch job created successfully",
		"data":       response,
		"request_id": c.Get("X-Request-ID"),
	})
}

// StartBatchJob starts a batch job
func (bh *BatchHandler) StartBatchJob(c *fiber.Ctx) error {
	jobID := c.Params("jobId")
	if jobID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "missing_job_id",
			Message:   "Job ID is required",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Start the job
	if err := bh.batchProcessor.StartBatchJob(jobID); err != nil {
		bh.logError(c, err, "start_batch_job")

		// Determine appropriate status code based on error
		statusCode := fiber.StatusInternalServerError
		errorType := "start_failed"

		if strings.Contains(err.Error(), "already running") {
			statusCode = fiber.StatusConflict
			errorType = "already_running"
		} else if strings.Contains(err.Error(), "maximum concurrent") {
			statusCode = fiber.StatusTooManyRequests
			errorType = "concurrent_limit_reached"
		} else if strings.Contains(err.Error(), "expected pending") {
			statusCode = fiber.StatusBadRequest
			errorType = "invalid_status"
		}

		return c.Status(statusCode).JSON(models.ErrorResponse{
			Error:     errorType,
			Message:   err.Error(),
			Code:      statusCode,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	return c.JSON(fiber.Map{
		"status":     "success",
		"message":    "Batch job started successfully",
		"job_id":     jobID,
		"request_id": c.Get("X-Request-ID"),
	})
}

// GetBatchJob gets a batch job by ID
func (bh *BatchHandler) GetBatchJob(c *fiber.Ctx) error {
	jobID := c.Params("jobId")
	if jobID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "missing_job_id",
			Message:   "Job ID is required",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Try cache first
	cacheKey := "batch_job:" + jobID
	if bh.cache != nil {
		if cached, found := bh.cache.Get(cacheKey); found {
			if response, ok := cached.(BatchJobResponse); ok {
				return c.JSON(fiber.Map{
					"status":     "success",
					"data":       response,
					"cached":     true,
					"request_id": c.Get("X-Request-ID"),
				})
			}
		}
	}

	// Get from database
	job, err := bh.batchProcessor.GetBatchJob(jobID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(models.ErrorResponse{
				Error:     "job_not_found",
				Message:   "Batch job not found",
				Code:      fiber.StatusNotFound,
				RequestID: c.Get("X-Request-ID"),
				Timestamp: time.Now(),
			})
		}

		bh.logError(c, err, "get_batch_job")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "retrieval_failed",
			Message:   "Failed to retrieve batch job",
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Parse tags and metadata
	var tags []string
	var metadata map[string]interface{}

	if job.Tags != "" {
		json.Unmarshal([]byte(job.Tags), &tags)
	}

	if job.Metadata != "" {
		json.Unmarshal([]byte(job.Metadata), &metadata)
	}

	// Convert to response
	response := bh.jobToResponse(job, tags, metadata)

	// Cache the response
	if bh.cache != nil {
		bh.cache.Set(cacheKey, response, 5*time.Minute)
	}

	return c.JSON(fiber.Map{
		"status":     "success",
		"data":       response,
		"request_id": c.Get("X-Request-ID"),
	})
}

// GetBatchJobProgress gets the progress of a batch job
func (bh *BatchHandler) GetBatchJobProgress(c *fiber.Ctx) error {
	jobID := c.Params("jobId")
	if jobID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "missing_job_id",
			Message:   "Job ID is required",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Get progress
	progress, err := bh.batchProcessor.GetBatchJobProgress(jobID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(models.ErrorResponse{
				Error:     "job_not_found",
				Message:   "Batch job not found",
				Code:      fiber.StatusNotFound,
				RequestID: c.Get("X-Request-ID"),
				Timestamp: time.Now(),
			})
		}

		bh.logError(c, err, "get_batch_job_progress")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "progress_retrieval_failed",
			Message:   "Failed to retrieve job progress",
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	return c.JSON(fiber.Map{
		"status":     "success",
		"data":       progress,
		"request_id": c.Get("X-Request-ID"),
	})
}

// ListBatchJobs lists batch jobs with optional filtering
func (bh *BatchHandler) ListBatchJobs(c *fiber.Ctx) error {
	// Parse query parameters
	userID := c.Query("user_id")
	statusStr := c.Query("status")
	typeStr := c.Query("type")
	limitStr := c.Query("limit", "50")
	offsetStr := c.Query("offset", "0")

	// Parse limit and offset
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100 // Maximum limit
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	// Parse status and type
	var status models.BatchJobStatus
	var jobType models.BatchJobType

	if statusStr != "" {
		status = models.BatchJobStatus(statusStr)
		if !status.IsValid() {
			return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
				Error:     "invalid_status",
				Message:   "Invalid status parameter",
				Code:      fiber.StatusBadRequest,
				RequestID: c.Get("X-Request-ID"),
				Timestamp: time.Now(),
			})
		}
	}

	if typeStr != "" {
		jobType = models.BatchJobType(typeStr)
		if !jobType.IsValid() {
			return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
				Error:     "invalid_type",
				Message:   "Invalid type parameter",
				Code:      fiber.StatusBadRequest,
				RequestID: c.Get("X-Request-ID"),
				Timestamp: time.Now(),
			})
		}
	}

	// Get jobs
	jobs, err := bh.batchProcessor.ListBatchJobs(userID, status, jobType, limit, offset)
	if err != nil {
		bh.logError(c, err, "list_batch_jobs")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "list_failed",
			Message:   "Failed to list batch jobs",
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	return c.JSON(fiber.Map{
		"status": "success",
		"data":   jobs,
		"pagination": fiber.Map{
			"limit":  limit,
			"offset": offset,
			"count":  len(jobs),
		},
		"request_id": c.Get("X-Request-ID"),
	})
}

// CancelBatchJob cancels a batch job
func (bh *BatchHandler) CancelBatchJob(c *fiber.Ctx) error {
	jobID := c.Params("jobId")
	if jobID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "missing_job_id",
			Message:   "Job ID is required",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Cancel the job
	if err := bh.batchProcessor.CancelBatchJob(jobID); err != nil {
		bh.logError(c, err, "cancel_batch_job")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "cancel_failed",
			Message:   "Failed to cancel batch job: " + err.Error(),
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Invalidate cache
	if bh.cache != nil {
		cacheKey := "batch_job:" + jobID
		bh.cache.Delete(cacheKey)
	}

	return c.JSON(fiber.Map{
		"status":     "success",
		"message":    "Batch job cancelled successfully",
		"job_id":     jobID,
		"request_id": c.Get("X-Request-ID"),
	})
}

// RetryBatchJob retries a failed batch job
func (bh *BatchHandler) RetryBatchJob(c *fiber.Ctx) error {
	jobID := c.Params("jobId")
	if jobID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "missing_job_id",
			Message:   "Job ID is required",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Retry the job
	if err := bh.batchProcessor.RetryBatchJob(jobID); err != nil {
		bh.logError(c, err, "retry_batch_job")

		statusCode := fiber.StatusInternalServerError
		errorType := "retry_failed"

		if strings.Contains(err.Error(), "cannot be retried") {
			statusCode = fiber.StatusBadRequest
			errorType = "retry_not_allowed"
		}

		return c.Status(statusCode).JSON(models.ErrorResponse{
			Error:     errorType,
			Message:   err.Error(),
			Code:      statusCode,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Invalidate cache
	if bh.cache != nil {
		cacheKey := "batch_job:" + jobID
		bh.cache.Delete(cacheKey)
	}

	return c.JSON(fiber.Map{
		"status":     "success",
		"message":    "Batch job retry initiated successfully",
		"job_id":     jobID,
		"request_id": c.Get("X-Request-ID"),
	})
}

// GetBatchJobFiles gets files for a batch job
func (bh *BatchHandler) GetBatchJobFiles(c *fiber.Ctx) error {
	jobID := c.Params("jobId")
	if jobID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:     "missing_job_id",
			Message:   "Job ID is required",
			Code:      fiber.StatusBadRequest,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	// Parse query parameters
	statusFilter := c.Query("status")
	limitStr := c.Query("limit", "100")
	offsetStr := c.Query("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	// Build query
	query := bh.db.Where("batch_job_id = ?", jobID)

	if statusFilter != "" {
		fileStatus := models.BatchFileStatus(statusFilter)
		if fileStatus.IsValid() {
			query = query.Where("status = ?", statusFilter)
		}
	}

	// Get files
	var files []models.BatchJobFile
	if err := query.Order("processing_order ASC").Limit(limit).Offset(offset).Find(&files).Error; err != nil {
		bh.logError(c, err, "get_batch_job_files")
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:     "files_retrieval_failed",
			Message:   "Failed to retrieve batch job files",
			Code:      fiber.StatusInternalServerError,
			RequestID: c.Get("X-Request-ID"),
			Timestamp: time.Now(),
		})
	}

	return c.JSON(fiber.Map{
		"status": "success",
		"data":   files,
		"pagination": fiber.Map{
			"limit":  limit,
			"offset": offset,
			"count":  len(files),
		},
		"request_id": c.Get("X-Request-ID"),
	})
}

// Helper methods

// jobToResponse converts a BatchJob to BatchJobResponse
func (bh *BatchHandler) jobToResponse(job *models.BatchJob, tags []string, metadata map[string]interface{}) BatchJobResponse {
	return BatchJobResponse{
		ID:              job.ID,
		Name:            job.Name,
		Description:     job.Description,
		Type:            job.Type,
		Status:          job.Status,
		Priority:        job.Priority,
		Config:          job.Config,
		Progress:        job.GetProgress(),
		TotalFiles:      job.TotalFiles,
		ProcessedFiles:  job.ProcessedFiles,
		SuccessfulFiles: job.SuccessfulFiles,
		FailedFiles:     job.FailedFiles,
		StartedAt:       job.StartedAt,
		CompletedAt:     job.CompletedAt,
		EstimatedETA:    job.EstimatedETA,
		ThroughputFPS:   job.ThroughputFPS,
		TotalDuration:   job.TotalDuration,
		LastError:       job.LastError,
		RetryCount:      job.RetryCount,
		MaxRetries:      job.MaxRetries,
		UserID:          job.UserID,
		Tags:            tags,
		Metadata:        metadata,
		CreatedAt:       job.CreatedAt,
		UpdatedAt:       job.UpdatedAt,
	}
}

// logError logs an error with context
func (bh *BatchHandler) logError(c *fiber.Ctx, err error, operation string) {
	if bh.logger != nil {
		bh.logger.LogError(err, operation, utils.LogContext{
			RequestID: c.Get("X-Request-ID"),
			Component: "batch_handler",
			Operation: operation,
		}, map[string]interface{}{
			"method":     c.Method(),
			"path":       c.Path(),
			"user_agent": c.Get("User-Agent"),
			"client_ip":  c.IP(),
		})
	}
}
