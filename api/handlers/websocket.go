package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"pocwhisp/models"
	"pocwhisp/services"
)

// WebSocketManager manages active WebSocket connections
type WebSocketManager struct {
	connections map[string]*WebSocketConnection
	mutex       sync.RWMutex
	aiClient    *services.AIClient
	db          *gorm.DB
}

// WebSocketConnection represents an active WebSocket connection
type WebSocketConnection struct {
	ID          string
	Conn        *websocket.Conn
	SessionID   string
	UserID      string // For future authentication
	CreatedAt   time.Time
	LastPing    time.Time
	IsStreaming bool
	AudioBuffer []byte
	mutex       sync.Mutex
	cancel      context.CancelFunc
}

// StreamingMessage represents messages sent over WebSocket
type StreamingMessage struct {
	Type      string                 `json:"type"`
	SessionID string                 `json:"session_id,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// AudioChunk represents incoming audio data
type AudioChunk struct {
	Data     []byte `json:"data"`
	Sequence int    `json:"sequence"`
	Final    bool   `json:"final"`
}

// TranscriptionResult represents real-time transcription results
type TranscriptionResult struct {
	Text       string    `json:"text"`
	Speaker    string    `json:"speaker"`
	StartTime  float64   `json:"start_time"`
	EndTime    float64   `json:"end_time"`
	Confidence float64   `json:"confidence"`
	Interim    bool      `json:"interim"`
	Timestamp  time.Time `json:"timestamp"`
}

// NewWebSocketManager creates a new WebSocket manager
func NewWebSocketManager(aiClient *services.AIClient, db *gorm.DB) *WebSocketManager {
	return &WebSocketManager{
		connections: make(map[string]*WebSocketConnection),
		aiClient:    aiClient,
		db:          db,
	}
}

// HandleWebSocketUpgrade handles WebSocket connection upgrades
func (wm *WebSocketManager) HandleWebSocketUpgrade() fiber.Handler {
	return websocket.New(wm.handleWebSocketConnection, websocket.Config{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(c *fiber.Ctx) bool {
			// In production, implement proper origin checking
			return true
		},
		EnableCompression: true,
	})
}

// handleWebSocketConnection handles new WebSocket connections
func (wm *WebSocketManager) handleWebSocketConnection(c *websocket.Conn) {
	connectionID := uuid.New().String()
	sessionID := uuid.New().String()

	log.Printf("New WebSocket connection: %s", connectionID)

	// Create connection context with cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Create connection object
	conn := &WebSocketConnection{
		ID:          connectionID,
		Conn:        c,
		SessionID:   sessionID,
		CreatedAt:   time.Now(),
		LastPing:    time.Now(),
		IsStreaming: false,
		AudioBuffer: make([]byte, 0),
		cancel:      cancel,
	}

	// Register connection
	wm.registerConnection(conn)
	defer wm.unregisterConnection(connectionID)

	// Send welcome message
	welcomeMsg := StreamingMessage{
		Type:      "connected",
		SessionID: sessionID,
		Data: map[string]interface{}{
			"connection_id": connectionID,
			"capabilities":  []string{"real_time_transcription", "audio_streaming", "live_feedback"},
		},
		Timestamp: time.Now(),
	}
	conn.sendMessage(welcomeMsg)

	// Start ping routine
	go wm.pingRoutine(ctx, conn)

	// Handle messages
	for {
		select {
		case <-ctx.Done():
			return
		default:
			messageType, data, err := c.ReadMessage()
			if err != nil {
				log.Printf("WebSocket read error for %s: %v", connectionID, err)
				return
			}

			if err := wm.handleMessage(ctx, conn, messageType, data); err != nil {
				log.Printf("Error handling message for %s: %v", connectionID, err)
				errorMsg := StreamingMessage{
					Type:      "error",
					Error:     err.Error(),
					Timestamp: time.Now(),
				}
				conn.sendMessage(errorMsg)
			}
		}
	}
}

// registerConnection adds a connection to the manager
func (wm *WebSocketManager) registerConnection(conn *WebSocketConnection) {
	wm.mutex.Lock()
	defer wm.mutex.Unlock()
	wm.connections[conn.ID] = conn
	log.Printf("Registered WebSocket connection %s, total: %d", conn.ID, len(wm.connections))
}

// unregisterConnection removes a connection from the manager
func (wm *WebSocketManager) unregisterConnection(connectionID string) {
	wm.mutex.Lock()
	defer wm.mutex.Unlock()

	if conn, exists := wm.connections[connectionID]; exists {
		conn.cancel()
		delete(wm.connections, connectionID)
		log.Printf("Unregistered WebSocket connection %s, remaining: %d", connectionID, len(wm.connections))
	}
}

// handleMessage processes incoming WebSocket messages
func (wm *WebSocketManager) handleMessage(ctx context.Context, conn *WebSocketConnection, messageType int, data []byte) error {
	conn.LastPing = time.Now()

	// Handle different message types
	switch messageType {
	case websocket.TextMessage:
		return wm.handleTextMessage(ctx, conn, data)
	case websocket.BinaryMessage:
		return wm.handleBinaryMessage(ctx, conn, data)
	case websocket.PingMessage:
		return conn.Conn.WriteMessage(websocket.PongMessage, nil)
	case websocket.CloseMessage:
		return fmt.Errorf("connection closed")
	default:
		return fmt.Errorf("unsupported message type: %d", messageType)
	}
}

// handleTextMessage processes text-based WebSocket messages
func (wm *WebSocketManager) handleTextMessage(ctx context.Context, conn *WebSocketConnection, data []byte) error {
	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("failed to parse JSON message: %w", err)
	}

	msgType, ok := msg["type"].(string)
	if !ok {
		return fmt.Errorf("missing or invalid message type")
	}

	switch msgType {
	case "start_streaming":
		return wm.handleStartStreaming(ctx, conn, msg)
	case "stop_streaming":
		return wm.handleStopStreaming(conn)
	case "audio_chunk":
		return wm.handleAudioChunk(ctx, conn, msg)
	case "ping":
		return wm.handlePing(conn)
	case "get_status":
		return wm.handleGetStatus(conn)
	default:
		return fmt.Errorf("unknown message type: %s", msgType)
	}
}

// handleBinaryMessage processes binary WebSocket messages (raw audio data)
func (wm *WebSocketManager) handleBinaryMessage(ctx context.Context, conn *WebSocketConnection, data []byte) error {
	if !conn.IsStreaming {
		return fmt.Errorf("received audio data but streaming not started")
	}

	// Add audio data to buffer
	conn.mutex.Lock()
	conn.AudioBuffer = append(conn.AudioBuffer, data...)
	bufferSize := len(conn.AudioBuffer)
	conn.mutex.Unlock()

	// Process audio chunks when buffer reaches threshold (e.g., 1 second of audio)
	// Assuming 16kHz, 16-bit, stereo = 64KB per second
	const chunkThreshold = 64 * 1024

	if bufferSize >= chunkThreshold {
		go wm.processAudioChunk(ctx, conn)
	}

	// Send acknowledgment
	ackMsg := StreamingMessage{
		Type: "audio_received",
		Data: map[string]interface{}{
			"bytes_received": len(data),
			"buffer_size":    bufferSize,
		},
		Timestamp: time.Now(),
	}

	return conn.sendMessage(ackMsg)
}

// handleStartStreaming initiates real-time audio streaming
func (wm *WebSocketManager) handleStartStreaming(ctx context.Context, conn *WebSocketConnection, msg map[string]interface{}) error {
	if conn.IsStreaming {
		return fmt.Errorf("streaming already active")
	}

	// Extract streaming parameters
	config := map[string]interface{}{
		"sample_rate": 16000,
		"channels":    2,
		"format":      "pcm_s16le",
	}

	if params, ok := msg["config"].(map[string]interface{}); ok {
		for k, v := range params {
			config[k] = v
		}
	}

	// Create streaming session in database
	session := models.AudioSession{
		ID:          conn.SessionID,
		Status:      "streaming",
		UploadedAt:  time.Now(),
		ProcessedAt: nil,
	}

	if err := wm.db.Create(&session).Error; err != nil {
		return fmt.Errorf("failed to create streaming session: %w", err)
	}

	conn.IsStreaming = true
	conn.AudioBuffer = make([]byte, 0)

	// Send confirmation
	responseMsg := StreamingMessage{
		Type:      "streaming_started",
		SessionID: conn.SessionID,
		Data: map[string]interface{}{
			"config":     config,
			"session_id": conn.SessionID,
			"status":     "ready",
		},
		Timestamp: time.Now(),
	}

	return conn.sendMessage(responseMsg)
}

// handleStopStreaming stops real-time audio streaming
func (wm *WebSocketManager) handleStopStreaming(conn *WebSocketConnection) error {
	if !conn.IsStreaming {
		return fmt.Errorf("streaming not active")
	}

	conn.IsStreaming = false

	// Process any remaining audio in buffer
	if len(conn.AudioBuffer) > 0 {
		go wm.processRemainingAudio(conn)
	}

	// Update session status
	if err := wm.db.Model(&models.AudioSession{}).
		Where("id = ?", conn.SessionID).
		Update("status", "completed").Error; err != nil {
		log.Printf("Failed to update session status: %v", err)
	}

	responseMsg := StreamingMessage{
		Type:      "streaming_stopped",
		SessionID: conn.SessionID,
		Data: map[string]interface{}{
			"status": "completed",
		},
		Timestamp: time.Now(),
	}

	return conn.sendMessage(responseMsg)
}

// handleAudioChunk processes structured audio chunk messages
func (wm *WebSocketManager) handleAudioChunk(ctx context.Context, conn *WebSocketConnection, msg map[string]interface{}) error {
	if !conn.IsStreaming {
		return fmt.Errorf("streaming not active")
	}

	// Extract audio chunk data
	chunkData, ok := msg["data"].(string)
	if !ok {
		return fmt.Errorf("missing or invalid audio data")
	}

	// Decode base64 audio data (if needed)
	// audioBytes, err := base64.StdEncoding.DecodeString(chunkData)
	// For now, assume raw bytes
	audioBytes := []byte(chunkData)

	sequence, _ := msg["sequence"].(float64)
	final, _ := msg["final"].(bool)

	// Add to buffer
	conn.mutex.Lock()
	conn.AudioBuffer = append(conn.AudioBuffer, audioBytes...)
	conn.mutex.Unlock()

	// Process chunk
	if final || len(conn.AudioBuffer) >= 64*1024 {
		go wm.processAudioChunk(ctx, conn)
	}

	// Send acknowledgment
	ackMsg := StreamingMessage{
		Type: "chunk_received",
		Data: map[string]interface{}{
			"sequence": int(sequence),
			"final":    final,
		},
		Timestamp: time.Now(),
	}

	return conn.sendMessage(ackMsg)
}

// handlePing responds to ping messages
func (wm *WebSocketManager) handlePing(conn *WebSocketConnection) error {
	conn.LastPing = time.Now()

	responseMsg := StreamingMessage{
		Type: "pong",
		Data: map[string]interface{}{
			"server_time": time.Now().Unix(),
		},
		Timestamp: time.Now(),
	}

	return conn.sendMessage(responseMsg)
}

// handleGetStatus returns connection and streaming status
func (wm *WebSocketManager) handleGetStatus(conn *WebSocketConnection) error {
	wm.mutex.RLock()
	totalConnections := len(wm.connections)
	wm.mutex.RUnlock()

	responseMsg := StreamingMessage{
		Type: "status",
		Data: map[string]interface{}{
			"connection_id":     conn.ID,
			"session_id":        conn.SessionID,
			"is_streaming":      conn.IsStreaming,
			"buffer_size":       len(conn.AudioBuffer),
			"uptime":            time.Since(conn.CreatedAt).Seconds(),
			"total_connections": totalConnections,
		},
		Timestamp: time.Now(),
	}

	return conn.sendMessage(responseMsg)
}

// processAudioChunk processes accumulated audio buffer
func (wm *WebSocketManager) processAudioChunk(ctx context.Context, conn *WebSocketConnection) {
	conn.mutex.Lock()
	audioData := make([]byte, len(conn.AudioBuffer))
	copy(audioData, conn.AudioBuffer)
	conn.AudioBuffer = conn.AudioBuffer[:0] // Clear buffer
	conn.mutex.Unlock()

	// TODO: Send to AI service for real-time transcription
	// For now, simulate processing
	go func() {
		time.Sleep(500 * time.Millisecond) // Simulate processing time

		// Mock transcription result
		result := TranscriptionResult{
			Text:       "Mock real-time transcription result...",
			Speaker:    "left", // or "right"
			StartTime:  0.0,
			EndTime:    2.0,
			Confidence: 0.95,
			Interim:    true,
			Timestamp:  time.Now(),
		}

		transcriptionMsg := StreamingMessage{
			Type:      "transcription",
			SessionID: conn.SessionID,
			Data: map[string]interface{}{
				"result": result,
			},
			Timestamp: time.Now(),
		}

		conn.sendMessage(transcriptionMsg)
	}()
}

// processRemainingAudio processes any remaining audio when streaming stops
func (wm *WebSocketManager) processRemainingAudio(conn *WebSocketConnection) {
	conn.mutex.Lock()
	audioData := make([]byte, len(conn.AudioBuffer))
	copy(audioData, conn.AudioBuffer)
	conn.AudioBuffer = conn.AudioBuffer[:0]
	conn.mutex.Unlock()

	if len(audioData) == 0 {
		return
	}

	// TODO: Send final audio chunk to AI service for processing
	// For now, just acknowledge
	finalMsg := StreamingMessage{
		Type:      "final_processing",
		SessionID: conn.SessionID,
		Data: map[string]interface{}{
			"bytes_processed": len(audioData),
			"status":          "completed",
		},
		Timestamp: time.Now(),
	}

	conn.sendMessage(finalMsg)
}

// pingRoutine sends periodic pings to maintain connection
func (wm *WebSocketManager) pingRoutine(ctx context.Context, conn *WebSocketConnection) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check if connection is still alive
			if time.Since(conn.LastPing) > 60*time.Second {
				log.Printf("WebSocket connection %s timed out", conn.ID)
				conn.Conn.Close()
				return
			}

			// Send ping
			if err := conn.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("Failed to send ping to %s: %v", conn.ID, err)
				return
			}
		}
	}
}

// sendMessage sends a message to the WebSocket connection
func (conn *WebSocketConnection) sendMessage(msg StreamingMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	return conn.Conn.WriteMessage(websocket.TextMessage, data)
}

// GetActiveConnections returns the number of active connections
func (wm *WebSocketManager) GetActiveConnections() int {
	wm.mutex.RLock()
	defer wm.mutex.RUnlock()
	return len(wm.connections)
}

// BroadcastMessage sends a message to all connected clients
func (wm *WebSocketManager) BroadcastMessage(msg StreamingMessage) {
	wm.mutex.RLock()
	connections := make([]*WebSocketConnection, 0, len(wm.connections))
	for _, conn := range wm.connections {
		connections = append(connections, conn)
	}
	wm.mutex.RUnlock()

	for _, conn := range connections {
		if err := conn.sendMessage(msg); err != nil {
			log.Printf("Failed to broadcast to connection %s: %v", conn.ID, err)
		}
	}
}

// Cleanup closes all connections and cleans up resources
func (wm *WebSocketManager) Cleanup() {
	wm.mutex.Lock()
	defer wm.mutex.Unlock()

	for id, conn := range wm.connections {
		conn.cancel()
		conn.Conn.Close()
		delete(wm.connections, id)
	}

	log.Printf("WebSocket manager cleaned up, closed %d connections", len(wm.connections))
}
