package utils

import (
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

// Logger provides structured logging capabilities
type Logger struct {
	*logrus.Logger
}

// LogContext provides context for logging
type LogContext struct {
	RequestID string
	UserID    string
	SessionID string
	Operation string
	Component string
	TraceID   string
	SpanID    string
}

// NewLogger creates a new structured logger
func NewLogger() *Logger {
	logger := logrus.New()

	// Set output to stdout
	logger.SetOutput(os.Stdout)

	// Set log level from environment
	level := os.Getenv("LOG_LEVEL")
	switch level {
	case "debug":
		logger.SetLevel(logrus.DebugLevel)
	case "info":
		logger.SetLevel(logrus.InfoLevel)
	case "warn":
		logger.SetLevel(logrus.WarnLevel)
	case "error":
		logger.SetLevel(logrus.ErrorLevel)
	default:
		logger.SetLevel(logrus.InfoLevel)
	}

	// Set JSON formatter for structured logging
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})

	// Add common fields
	logger.WithFields(logrus.Fields{
		"service":     "pocwhisp-api",
		"version":     "1.0.0",
		"environment": getEnv("ENVIRONMENT", "development"),
	})

	return &Logger{Logger: logger}
}

// WithContext adds context information to the logger
func (l *Logger) WithContext(ctx LogContext) *logrus.Entry {
	fields := logrus.Fields{}

	if ctx.RequestID != "" {
		fields["request_id"] = ctx.RequestID
	}
	if ctx.UserID != "" {
		fields["user_id"] = ctx.UserID
	}
	if ctx.SessionID != "" {
		fields["session_id"] = ctx.SessionID
	}
	if ctx.Operation != "" {
		fields["operation"] = ctx.Operation
	}
	if ctx.Component != "" {
		fields["component"] = ctx.Component
	}
	if ctx.TraceID != "" {
		fields["trace_id"] = ctx.TraceID
	}
	if ctx.SpanID != "" {
		fields["span_id"] = ctx.SpanID
	}

	return l.WithFields(fields)
}

// LogHTTPRequest logs HTTP request details
func (l *Logger) LogHTTPRequest(
	method, path, userAgent, clientIP string,
	statusCode int,
	duration time.Duration,
	requestSize, responseSize int,
	requestID string,
) {
	l.WithFields(logrus.Fields{
		"type":          "http_request",
		"method":        method,
		"path":          path,
		"status_code":   statusCode,
		"duration_ms":   duration.Milliseconds(),
		"request_size":  requestSize,
		"response_size": responseSize,
		"user_agent":    userAgent,
		"client_ip":     clientIP,
		"request_id":    requestID,
	}).Info("HTTP request processed")
}

// LogAudioProcessing logs audio processing events
func (l *Logger) LogAudioProcessing(
	sessionID, operation string,
	fileSize int64,
	duration time.Duration,
	success bool,
	errorMsg string,
) {
	fields := logrus.Fields{
		"type":        "audio_processing",
		"session_id":  sessionID,
		"operation":   operation,
		"file_size":   fileSize,
		"duration_ms": duration.Milliseconds(),
		"success":     success,
	}

	if errorMsg != "" {
		fields["error"] = errorMsg
	}

	entry := l.WithFields(fields)
	if success {
		entry.Info("Audio processing completed")
	} else {
		entry.Error("Audio processing failed")
	}
}

// LogTranscription logs transcription events
func (l *Logger) LogTranscription(
	sessionID, modelName, language string,
	segmentCount int,
	confidence float64,
	duration time.Duration,
	success bool,
	errorMsg string,
) {
	fields := logrus.Fields{
		"type":          "transcription",
		"session_id":    sessionID,
		"model":         modelName,
		"language":      language,
		"segment_count": segmentCount,
		"confidence":    confidence,
		"duration_ms":   duration.Milliseconds(),
		"success":       success,
	}

	if errorMsg != "" {
		fields["error"] = errorMsg
	}

	entry := l.WithFields(fields)
	if success {
		entry.Info("Transcription completed")
	} else {
		entry.Error("Transcription failed")
	}
}

// LogCacheOperation logs cache operations
func (l *Logger) LogCacheOperation(
	operation, key string,
	hit bool,
	duration time.Duration,
	cacheLevel string,
) {
	l.WithFields(logrus.Fields{
		"type":        "cache_operation",
		"operation":   operation,
		"key":         key,
		"hit":         hit,
		"duration_ms": duration.Milliseconds(),
		"cache_level": cacheLevel,
	}).Debug("Cache operation")
}

// LogQueueJob logs queue job events
func (l *Logger) LogQueueJob(
	jobID, jobType string,
	priority int,
	status string,
	duration time.Duration,
	workerID string,
	errorMsg string,
) {
	fields := logrus.Fields{
		"type":        "queue_job",
		"job_id":      jobID,
		"job_type":    jobType,
		"priority":    priority,
		"status":      status,
		"duration_ms": duration.Milliseconds(),
		"worker_id":   workerID,
	}

	if errorMsg != "" {
		fields["error"] = errorMsg
	}

	entry := l.WithFields(fields)
	switch status {
	case "completed":
		entry.Info("Job completed successfully")
	case "failed":
		entry.Error("Job failed")
	case "started":
		entry.Info("Job started")
	default:
		entry.Info("Job status updated")
	}
}

// LogWebSocket logs WebSocket events
func (l *Logger) LogWebSocket(
	connectionID, sessionID string,
	event string,
	messageType string,
	dataSize int,
	duration time.Duration,
) {
	l.WithFields(logrus.Fields{
		"type":          "websocket",
		"connection_id": connectionID,
		"session_id":    sessionID,
		"event":         event,
		"message_type":  messageType,
		"data_size":     dataSize,
		"duration_ms":   duration.Milliseconds(),
	}).Info("WebSocket event")
}

// LogDatabaseOperation logs database operations
func (l *Logger) LogDatabaseOperation(
	operation, table string,
	affectedRows int,
	duration time.Duration,
	success bool,
	errorMsg string,
) {
	fields := logrus.Fields{
		"type":          "database",
		"operation":     operation,
		"table":         table,
		"affected_rows": affectedRows,
		"duration_ms":   duration.Milliseconds(),
		"success":       success,
	}

	if errorMsg != "" {
		fields["error"] = errorMsg
	}

	entry := l.WithFields(fields)
	if success {
		entry.Debug("Database operation completed")
	} else {
		entry.Error("Database operation failed")
	}
}

// LogAIService logs AI service interactions
func (l *Logger) LogAIService(
	service, operation string,
	inputSize int,
	duration time.Duration,
	success bool,
	errorMsg string,
) {
	fields := logrus.Fields{
		"type":        "ai_service",
		"service":     service,
		"operation":   operation,
		"input_size":  inputSize,
		"duration_ms": duration.Milliseconds(),
		"success":     success,
	}

	if errorMsg != "" {
		fields["error"] = errorMsg
	}

	entry := l.WithFields(fields)
	if success {
		entry.Info("AI service call completed")
	} else {
		entry.Error("AI service call failed")
	}
}

// LogSecurity logs security-related events
func (l *Logger) LogSecurity(
	event, userID, clientIP string,
	severity string,
	details map[string]interface{},
) {
	fields := logrus.Fields{
		"type":      "security",
		"event":     event,
		"user_id":   userID,
		"client_ip": clientIP,
		"severity":  severity,
	}

	// Add additional details
	for key, value := range details {
		fields[key] = value
	}

	entry := l.WithFields(fields)
	switch severity {
	case "critical":
		entry.Error("Critical security event")
	case "high":
		entry.Warn("High severity security event")
	case "medium":
		entry.Info("Medium severity security event")
	case "low":
		entry.Debug("Low severity security event")
	default:
		entry.Info("Security event")
	}
}

// LogPerformance logs performance metrics
func (l *Logger) LogPerformance(
	operation string,
	duration time.Duration,
	memoryUsage int64,
	cpuUsage float64,
	details map[string]interface{},
) {
	fields := logrus.Fields{
		"type":         "performance",
		"operation":    operation,
		"duration_ms":  duration.Milliseconds(),
		"memory_bytes": memoryUsage,
		"cpu_percent":  cpuUsage,
	}

	// Add additional details
	for key, value := range details {
		fields[key] = value
	}

	l.WithFields(fields).Info("Performance metrics")
}

// LogError logs structured errors
func (l *Logger) LogError(
	err error,
	operation string,
	context LogContext,
	details map[string]interface{},
) {
	fields := logrus.Fields{
		"type":      "error",
		"error":     err.Error(),
		"operation": operation,
	}

	// Add context fields
	if context.RequestID != "" {
		fields["request_id"] = context.RequestID
	}
	if context.SessionID != "" {
		fields["session_id"] = context.SessionID
	}
	if context.UserID != "" {
		fields["user_id"] = context.UserID
	}
	if context.Component != "" {
		fields["component"] = context.Component
	}

	// Add additional details
	for key, value := range details {
		fields[key] = value
	}

	l.WithFields(fields).Error("Operation failed")
}

// LogBusiness logs business-level events
func (l *Logger) LogBusiness(
	event string,
	userID, sessionID string,
	value float64,
	currency string,
	details map[string]interface{},
) {
	fields := logrus.Fields{
		"type":       "business",
		"event":      event,
		"user_id":    userID,
		"session_id": sessionID,
		"value":      value,
		"currency":   currency,
	}

	// Add additional details
	for key, value := range details {
		fields[key] = value
	}

	l.WithFields(fields).Info("Business event")
}

// Helper function to get environment variables
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Global logger instance
var globalLogger *Logger

// InitializeLogger initializes the global logger
func InitializeLogger() {
	globalLogger = NewLogger()
}

// GetLogger returns the global logger instance
func GetLogger() *Logger {
	if globalLogger == nil {
		InitializeLogger()
	}
	return globalLogger
}
