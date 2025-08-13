package handlers

import (
	"fmt"
	"os"
	"path/filepath"
	"pocwhisp/models"
	"pocwhisp/services"
	"pocwhisp/utils"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	MaxFileSize  = 1024 * 1024 * 1024 // 1GB
	TempDir      = "/tmp/pocwhisp"
	AllowedTypes = "audio/wav"
)

// TranscribeHandler handles audio file upload and processing
type TranscribeHandler struct {
	db       *gorm.DB
	aiClient *services.AIClient
	cache    *services.MultiLevelCache
}

// NewTranscribeHandler creates a new transcribe handler
func NewTranscribeHandler(db *gorm.DB, aiServiceURL string) *TranscribeHandler {
	aiClient := services.NewAIClient(aiServiceURL, 60*time.Second)
	cache := services.GetCache() // Get global cache instance
	return &TranscribeHandler{
		db:       db,
		aiClient: aiClient,
		cache:    cache,
	}
}

// UploadAudio handles the POST /transcribe endpoint
func (h *TranscribeHandler) UploadAudio(c *fiber.Ctx) error {
	startTime := time.Now()

	// Ensure temp directory exists
	if err := os.MkdirAll(TempDir, 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:   "server_error",
			Message: "Failed to create temporary directory",
			Code:    fiber.StatusInternalServerError,
		})
	}

	// Get the uploaded file
	file, err := c.FormFile("audio")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:   "missing_file",
			Message: "No audio file provided. Use 'audio' field name.",
			Code:    fiber.StatusBadRequest,
		})
	}

	// Validate file size
	if file.Size > MaxFileSize {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:   "file_too_large",
			Message: fmt.Sprintf("File size exceeds maximum allowed size of %d bytes", MaxFileSize),
			Code:    fiber.StatusBadRequest,
		})
	}

	// Validate file type by extension and content-type
	contentType := file.Header.Get("Content-Type")
	filename := file.Filename
	isValidWAV := contentType == AllowedTypes ||
		contentType == "audio/x-wav" ||
		contentType == "audio/wave" ||
		contentType == "application/octet-stream" || // Some browsers send this for WAV
		strings.HasSuffix(strings.ToLower(filename), ".wav")

	if !isValidWAV {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:   "invalid_file_type",
			Message: fmt.Sprintf("Invalid file type. Only WAV files are supported. Got Content-Type: %s, Filename: %s", contentType, filename),
			Code:    fiber.StatusBadRequest,
		})
	}

	// Generate unique filename
	fileID := uuid.New().String()
	tempFilePath := filepath.Join(TempDir, fileID+".wav")

	// Save uploaded file
	if err := c.SaveFile(file, tempFilePath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:   "save_error",
			Message: "Failed to save uploaded file",
			Code:    fiber.StatusInternalServerError,
		})
	}

	// Clean up file when done
	defer func() {
		if err := os.Remove(tempFilePath); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: Failed to clean up temp file %s: %v\n", tempFilePath, err)
		}
	}()

	// Validate WAV file and extract metadata
	metadata, err := utils.ValidateWAVFile(tempFilePath)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:   "invalid_audio",
			Message: fmt.Sprintf("Invalid WAV file: %v", err),
			Code:    fiber.StatusBadRequest,
		})
	}

	// Validate audio constraints
	if err := utils.ValidateAudioConstraints(metadata); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:   "audio_constraints",
			Message: err.Error(),
			Code:    fiber.StatusBadRequest,
		})
	}

	// Create database record for audio session
	sessionID := uuid.New()
	audioSession := models.AudioSession{
		ID:           sessionID,
		Filename:     fileID + ".wav",
		OriginalName: file.Filename,
		Duration:     metadata.Duration,
		Channels:     metadata.Channels,
		SampleRate:   metadata.SampleRate,
		FileSize:     metadata.FileSize,
		Status:       "uploaded",
	}

	// Save to database
	if err := h.db.Create(&audioSession).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:   "database_error",
			Message: "Failed to save audio session to database",
			Code:    fiber.StatusInternalServerError,
		})
	}

	// Create processing job for transcription
	transcriptionJob := models.ProcessingJob{
		SessionID: sessionID,
		JobType:   "transcription",
		Status:    "queued",
		Priority:  5,
	}

	if err := h.db.Create(&transcriptionJob).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:   "database_error",
			Message: "Failed to create processing job",
			Code:    fiber.StatusInternalServerError,
		})
	}

	// Check if AI service is available
	if healthy, err := h.aiClient.IsHealthy(); err != nil || !healthy {
		// AI service not available, create job for later processing
		transcriptionJob.Status = "pending"
		h.db.Save(&transcriptionJob)

		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
			"session_id":        sessionID,
			"status":            "accepted",
			"message":           "Audio uploaded, queued for processing",
			"ai_service_status": "unavailable",
		})
	}

	// Process with AI service
	transcriptionResult, err := h.aiClient.TranscribeAudio(tempFilePath)
	if err != nil {
		// Mark job as failed
		transcriptionJob.Status = "failed"
		transcriptionJob.ErrorMessage = err.Error()
		h.db.Save(&transcriptionJob)

		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:   "ai_processing_failed",
			Message: fmt.Sprintf("AI processing failed: %v", err),
			Code:    fiber.StatusInternalServerError,
		})
	}

	// Save transcription results to database
	err = h.saveTranscriptionResults(&audioSession, transcriptionResult)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:   "database_save_failed",
			Message: "Failed to save transcription results",
			Code:    fiber.StatusInternalServerError,
		})
	}

	// Mark job as completed
	transcriptionJob.Status = "completed"
	now := time.Now()
	transcriptionJob.CompletedAt = &now
	h.db.Save(&transcriptionJob)

	// Update session status
	audioSession.Status = "completed"
	audioSession.ProcessedAt = &now
	h.db.Save(&audioSession)

	// Convert to API response format
	response := h.convertToAPIResponse(&audioSession, transcriptionResult, time.Since(startTime).Seconds())

	return c.JSON(response)
}

// GetTranscription handles GET /transcribe/:id endpoint
func (h *TranscribeHandler) GetTranscription(c *fiber.Ctx) error {
	sessionID := c.Params("id")
	if sessionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:   "missing_id",
			Message: "Session ID is required",
			Code:    fiber.StatusBadRequest,
		})
	}

	// Try to get from cache first
	if h.cache != nil {
		if cachedResult, found := h.cache.GetTranscript(sessionID); found {
			return c.JSON(cachedResult)
		}
	}

	// Parse UUID
	sessionUUID, err := uuid.Parse(sessionID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid session ID format",
			Code:    fiber.StatusBadRequest,
		})
	}

	// Find audio session with relationships
	var audioSession models.AudioSession
	err = h.db.Preload("Segments").Preload("Summary").Preload("Jobs").
		First(&audioSession, "id = ?", sessionUUID).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(models.ErrorResponse{
				Error:   "not_found",
				Message: "Audio session not found",
				Code:    fiber.StatusNotFound,
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:   "database_error",
			Message: "Failed to retrieve audio session",
			Code:    fiber.StatusInternalServerError,
		})
	}

	// Convert to response format
	response := audioSession.ToResponse()

	// Cache the response for future requests
	if h.cache != nil {
		h.cache.SetTranscript(sessionID, response)
	}

	return c.JSON(response)
}

// ListTranscriptions handles GET /transcribe endpoint
func (h *TranscribeHandler) ListTranscriptions(c *fiber.Ctx) error {
	var audioSessions []models.AudioSession

	// Parse query parameters
	limit := c.QueryInt("limit", 10)
	offset := c.QueryInt("offset", 0)
	status := c.Query("status", "")

	query := h.db.Preload("Summary")

	// Filter by status if provided
	if status != "" {
		query = query.Where("status = ?", status)
	}

	// Apply pagination and ordering
	err := query.Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&audioSessions).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error:   "database_error",
			Message: "Failed to retrieve audio sessions",
			Code:    fiber.StatusInternalServerError,
		})
	}

	// Convert to response format
	responses := make([]models.TranscriptionResponse, len(audioSessions))
	for i, session := range audioSessions {
		responses[i] = *session.ToResponse()
	}

	return c.JSON(fiber.Map{
		"sessions": responses,
		"pagination": fiber.Map{
			"limit":  limit,
			"offset": offset,
			"total":  len(responses),
		},
	})
}

// saveTranscriptionResults saves AI transcription results to database
func (h *TranscribeHandler) saveTranscriptionResults(session *models.AudioSession, aiResult *services.TranscriptionResponse) error {
	// Save transcript segments
	for _, segment := range aiResult.Transcription.Segments {
		dbSegment := models.TranscriptSegmentDB{
			SessionID:  session.ID,
			Speaker:    segment.Speaker,
			StartTime:  segment.StartTime,
			EndTime:    segment.EndTime,
			Text:       segment.Text,
			Confidence: segment.Confidence,
		}

		if err := h.db.Create(&dbSegment).Error; err != nil {
			return fmt.Errorf("failed to save transcript segment: %w", err)
		}
	}

	return nil
}

// convertToAPIResponse converts AI service response to API response format
func (h *TranscribeHandler) convertToAPIResponse(session *models.AudioSession, aiResult *services.TranscriptionResponse, totalProcessingTime float64) models.TranscriptionResponse {
	// Convert segments
	segments := make([]models.TranscriptSegment, len(aiResult.Transcription.Segments))
	for i, seg := range aiResult.Transcription.Segments {
		segments[i] = models.TranscriptSegment{
			Speaker:   seg.Speaker,
			StartTime: seg.StartTime,
			EndTime:   seg.EndTime,
			Text:      seg.Text,
		}
	}

	// If we have segments, try to get summary
	var summary models.Summary
	if len(segments) > 0 {
		summaryResult, err := h.getSummaryForTranscription(aiResult)
		if err == nil && summaryResult != nil {
			summary = models.Summary{
				Text:      summaryResult.Summary.Text,
				KeyPoints: summaryResult.Summary.KeyPoints,
				Sentiment: summaryResult.Summary.Sentiment,
			}
		} else {
			// Fallback summary
			summary = models.Summary{
				Text:      "Conversation transcribed successfully",
				KeyPoints: []string{"Audio processed", "Speakers identified"},
				Sentiment: "neutral",
			}
		}
	}

	return models.TranscriptionResponse{
		Transcript: models.Transcript{
			Segments: segments,
		},
		Summary: summary,
		Metadata: models.Metadata{
			Duration:       aiResult.Transcription.Duration,
			ProcessingTime: totalProcessingTime,
			ModelVersions: models.ModelVersions{
				Whisper: aiResult.Transcription.ModelVersion,
				Llama:   "llama-2-7b",
			},
			ProcessedAt: time.Now(),
		},
	}
}

// getSummaryForTranscription gets summary from AI service
func (h *TranscribeHandler) getSummaryForTranscription(transcriptionResult *services.TranscriptionResponse) (*services.SummarizationResponse, error) {
	// Convert to summarization request format
	request := &services.SummarizationRequest{
		Language:   transcriptionResult.Transcription.Language,
		Duration:   transcriptionResult.Transcription.Duration,
		Confidence: transcriptionResult.Transcription.Confidence,
	}

	// Convert segments
	for _, seg := range transcriptionResult.Transcription.Segments {
		request.Segments = append(request.Segments, struct {
			Speaker    string  `json:"speaker"`
			StartTime  float64 `json:"start_time"`
			EndTime    float64 `json:"end_time"`
			Text       string  `json:"text"`
			Confidence float64 `json:"confidence"`
			Language   string  `json:"language,omitempty"`
		}{
			Speaker:    seg.Speaker,
			StartTime:  seg.StartTime,
			EndTime:    seg.EndTime,
			Text:       seg.Text,
			Confidence: seg.Confidence,
			Language:   seg.Language,
		})
	}

	// Get summary from AI service
	return h.aiClient.SummarizeTranscription(request)
}
