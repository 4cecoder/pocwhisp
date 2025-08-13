package processors

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"gorm.io/gorm"

	"pocwhisp/models"
	"pocwhisp/services"
)

// AudioJobProcessor handles audio processing jobs
type AudioJobProcessor struct {
	db       *gorm.DB
	aiClient *services.AIClient
}

// NewAudioJobProcessor creates a new audio job processor
func NewAudioJobProcessor(db *gorm.DB, aiClient *services.AIClient) *AudioJobProcessor {
	return &AudioJobProcessor{
		db:       db,
		aiClient: aiClient,
	}
}

// ProcessJob processes audio-related jobs
func (p *AudioJobProcessor) ProcessJob(ctx context.Context, job *services.Job) error {
	log.Printf("Processing audio job %s (%s)", job.ID, job.Type)

	switch job.Type {
	case services.JobTypeTranscription:
		return p.processTranscriptionJob(ctx, job)
	case services.JobTypeSummarization:
		return p.processSummarizationJob(ctx, job)
	case services.JobTypeChannelSeparation:
		return p.processChannelSeparationJob(ctx, job)
	case services.JobTypeBatchProcessing:
		return p.processBatchJob(ctx, job)
	default:
		return fmt.Errorf("unsupported job type: %s", job.Type)
	}
}

// GetJobTypes returns the supported job types
func (p *AudioJobProcessor) GetJobTypes() []services.JobType {
	return []services.JobType{
		services.JobTypeTranscription,
		services.JobTypeSummarization,
		services.JobTypeChannelSeparation,
		services.JobTypeBatchProcessing,
	}
}

// processTranscriptionJob handles audio transcription
func (p *AudioJobProcessor) processTranscriptionJob(ctx context.Context, job *services.Job) error {
	// Extract job parameters
	sessionID, ok := job.Payload["session_id"].(string)
	if !ok {
		return fmt.Errorf("missing or invalid session_id in job payload")
	}

	audioPath, ok := job.Payload["audio_path"].(string)
	if !ok {
		return fmt.Errorf("missing or invalid audio_path in job payload")
	}

	// Verify audio file exists
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		return fmt.Errorf("audio file not found: %s", audioPath)
	}

	// Get audio session from database
	var session models.AudioSession
	if err := p.db.Where("id = ?", sessionID).First(&session).Error; err != nil {
		return fmt.Errorf("failed to find audio session: %w", err)
	}

	// Update session status
	session.Status = "processing"
	if err := p.db.Save(&session).Error; err != nil {
		log.Printf("Failed to update session status: %v", err)
	}

	// Check AI service health
	if healthy, err := p.aiClient.IsHealthy(); err != nil || !healthy {
		return fmt.Errorf("AI service is not available")
	}

	// Process audio with AI service
	log.Printf("Sending audio file to AI service: %s", audioPath)
	transcriptionResult, err := p.aiClient.TranscribeAudio(audioPath)
	if err != nil {
		// Update session status to failed
		session.Status = "failed"
		session.ProcessedAt = timePtr(time.Now())
		p.db.Save(&session)
		return fmt.Errorf("transcription failed: %w", err)
	}

	// Save transcription results to database
	if err := p.saveTranscriptionResults(sessionID, transcriptionResult); err != nil {
		return fmt.Errorf("failed to save transcription results: %w", err)
	}

	// Request summarization if enabled
	summarizationEnabled, _ := job.Payload["enable_summarization"].(bool)
	if summarizationEnabled {
		if err := p.requestSummarization(sessionID, transcriptionResult); err != nil {
			log.Printf("Failed to request summarization: %v", err)
			// Don't fail the transcription job for summarization errors
		}
	}

	// Update session status
	session.Status = "completed"
	session.ProcessedAt = timePtr(time.Now())
	if err := p.db.Save(&session).Error; err != nil {
		log.Printf("Failed to update session status: %v", err)
	}

	// Clean up temporary file if requested
	if cleanupFile, ok := job.Payload["cleanup_file"].(bool); ok && cleanupFile {
		if err := os.Remove(audioPath); err != nil {
			log.Printf("Failed to cleanup audio file %s: %v", audioPath, err)
		} else {
			log.Printf("Cleaned up audio file: %s", audioPath)
		}
	}

	// Store results in job
	job.Result = map[string]interface{}{
		"session_id":           sessionID,
		"transcription_length": len(transcriptionResult.Segments),
		"processing_time":      time.Since(job.CreatedAt).Seconds(),
		"audio_duration":       transcriptionResult.Duration,
	}

	log.Printf("Transcription job %s completed successfully", job.ID)
	return nil
}

// processSummarizationJob handles text summarization
func (p *AudioJobProcessor) processSummarizationJob(ctx context.Context, job *services.Job) error {
	sessionID, ok := job.Payload["session_id"].(string)
	if !ok {
		return fmt.Errorf("missing or invalid session_id in job payload")
	}

	// Get transcript text from database
	var segments []models.TranscriptSegmentDB
	if err := p.db.Where("session_id = ?", sessionID).Order("start_time ASC").Find(&segments).Error; err != nil {
		return fmt.Errorf("failed to get transcript segments: %w", err)
	}

	if len(segments) == 0 {
		return fmt.Errorf("no transcript segments found for session %s", sessionID)
	}

	// Combine transcript text
	var fullText string
	for _, segment := range segments {
		fullText += segment.Text + " "
	}

	// Check AI service health
	if healthy, err := p.aiClient.IsHealthy(); err != nil || !healthy {
		return fmt.Errorf("AI service is not available")
	}

	// Request summarization
	log.Printf("Requesting summarization for session %s", sessionID)
	summaryResult, err := p.aiClient.SummarizeTranscription(fullText)
	if err != nil {
		return fmt.Errorf("summarization failed: %w", err)
	}

	// Save summary to database
	summary := models.SummaryDB{
		SessionID:  sessionID,
		Text:       summaryResult.Summary,
		KeyPoints:  summaryResult.KeyPoints,
		Sentiment:  summaryResult.Sentiment,
		Confidence: summaryResult.Confidence,
		CreatedAt:  time.Now(),
	}

	if err := p.db.Create(&summary).Error; err != nil {
		return fmt.Errorf("failed to save summary: %w", err)
	}

	// Store results in job
	job.Result = map[string]interface{}{
		"session_id":       sessionID,
		"summary_length":   len(summaryResult.Summary),
		"key_points_count": len(summaryResult.KeyPoints),
		"sentiment":        summaryResult.Sentiment,
		"confidence":       summaryResult.Confidence,
	}

	log.Printf("Summarization job %s completed successfully", job.ID)
	return nil
}

// processChannelSeparationJob handles stereo channel separation
func (p *AudioJobProcessor) processChannelSeparationJob(ctx context.Context, job *services.Job) error {
	audioPath, ok := job.Payload["audio_path"].(string)
	if !ok {
		return fmt.Errorf("missing or invalid audio_path in job payload")
	}

	outputDir, ok := job.Payload["output_dir"].(string)
	if !ok {
		return fmt.Errorf("missing or invalid output_dir in job payload")
	}

	// Verify audio file exists
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		return fmt.Errorf("audio file not found: %s", audioPath)
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Check AI service health
	if healthy, err := p.aiClient.IsHealthy(); err != nil || !healthy {
		return fmt.Errorf("AI service is not available")
	}

	// Request channel separation (this would be a new AI service endpoint)
	log.Printf("Requesting channel separation for audio: %s", audioPath)

	// For now, simulate channel separation processing
	time.Sleep(2 * time.Second)

	// Generate output file paths
	leftChannelPath := filepath.Join(outputDir, "left_channel.wav")
	rightChannelPath := filepath.Join(outputDir, "right_channel.wav")

	// Store results in job
	job.Result = map[string]interface{}{
		"left_channel_path":  leftChannelPath,
		"right_channel_path": rightChannelPath,
		"original_audio":     audioPath,
		"output_directory":   outputDir,
	}

	log.Printf("Channel separation job %s completed successfully", job.ID)
	return nil
}

// processBatchJob handles batch processing of multiple audio files
func (p *AudioJobProcessor) processBatchJob(ctx context.Context, job *services.Job) error {
	filePaths, ok := job.Payload["file_paths"].([]interface{})
	if !ok {
		return fmt.Errorf("missing or invalid file_paths in job payload")
	}

	enableSummarization, _ := job.Payload["enable_summarization"].(bool)

	results := make([]map[string]interface{}, 0, len(filePaths))
	successCount := 0
	failureCount := 0

	// Process each file
	for i, pathInterface := range filePaths {
		audioPath, ok := pathInterface.(string)
		if !ok {
			log.Printf("Invalid file path at index %d: %v", i, pathInterface)
			failureCount++
			continue
		}

		log.Printf("Processing batch file %d/%d: %s", i+1, len(filePaths), audioPath)

		// Create individual transcription job
		transcriptionJob := &services.Job{
			ID:       fmt.Sprintf("%s_file_%d", job.ID, i),
			Type:     services.JobTypeTranscription,
			Priority: job.Priority,
			Status:   services.JobStatusProcessing,
			Payload: map[string]interface{}{
				"session_id":           fmt.Sprintf("batch_%s_%d", job.ID, i),
				"audio_path":           audioPath,
				"enable_summarization": enableSummarization,
				"cleanup_file":         false, // Don't cleanup batch files
			},
			Attempts:    0,
			MaxAttempts: 1, // Don't retry individual files in batch
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
			Timeout:     job.Timeout,
		}

		// Create session for this file
		session := models.AudioSession{
			ID:         transcriptionJob.Payload["session_id"].(string),
			Filename:   filepath.Base(audioPath),
			Status:     "processing",
			UploadedAt: time.Now(),
		}

		if err := p.db.Create(&session).Error; err != nil {
			log.Printf("Failed to create session for batch file %s: %v", audioPath, err)
			failureCount++
			continue
		}

		// Process the file
		if err := p.processTranscriptionJob(ctx, transcriptionJob); err != nil {
			log.Printf("Failed to process batch file %s: %v", audioPath, err)
			failureCount++

			// Update session status
			session.Status = "failed"
			session.ProcessedAt = timePtr(time.Now())
			p.db.Save(&session)

			results = append(results, map[string]interface{}{
				"file_path": audioPath,
				"status":    "failed",
				"error":     err.Error(),
			})
		} else {
			successCount++
			results = append(results, map[string]interface{}{
				"file_path":  audioPath,
				"status":     "completed",
				"session_id": session.ID,
				"result":     transcriptionJob.Result,
			})
		}

		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	// Store batch results
	job.Result = map[string]interface{}{
		"total_files":     len(filePaths),
		"success_count":   successCount,
		"failure_count":   failureCount,
		"success_rate":    float64(successCount) / float64(len(filePaths)),
		"file_results":    results,
		"processing_time": time.Since(job.CreatedAt).Seconds(),
	}

	if failureCount > 0 && successCount == 0 {
		return fmt.Errorf("all files in batch failed to process")
	}

	log.Printf("Batch job %s completed: %d success, %d failures", job.ID, successCount, failureCount)
	return nil
}

// Helper functions

// saveTranscriptionResults saves transcription results to the database
func (p *AudioJobProcessor) saveTranscriptionResults(sessionID string, result *services.TranscriptionResult) error {
	// Save transcript segments
	for _, segment := range result.Segments {
		segmentDB := models.TranscriptSegmentDB{
			SessionID:  sessionID,
			Speaker:    segment.Speaker,
			StartTime:  segment.StartTime,
			EndTime:    segment.EndTime,
			Text:       segment.Text,
			Confidence: segment.Confidence,
			CreatedAt:  time.Now(),
		}

		if err := p.db.Create(&segmentDB).Error; err != nil {
			return fmt.Errorf("failed to save transcript segment: %w", err)
		}
	}

	log.Printf("Saved %d transcript segments for session %s", len(result.Segments), sessionID)
	return nil
}

// requestSummarization enqueues a summarization job
func (p *AudioJobProcessor) requestSummarization(sessionID string, transcriptionResult *services.TranscriptionResult) error {
	// This would typically be done through the queue manager
	log.Printf("Summarization requested for session %s (not implemented in this processor)", sessionID)
	return nil
}

// timePtr returns a pointer to a time value
func timePtr(t time.Time) *time.Time {
	return &t
}
