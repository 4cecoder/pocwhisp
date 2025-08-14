package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"pocwhisp/models"
	"pocwhisp/utils"
)

// RAGService provides Retrieval Augmented Generation capabilities
type RAGService struct {
	db              *gorm.DB
	embeddingClient *EmbeddingClient
	cache           MultiLevelCache
	logger          *utils.Logger
	config          *RAGConfig
}

// RAGConfig holds configuration for the RAG service
type RAGConfig struct {
	DefaultChunkSize     int           `json:"default_chunk_size"`
	DefaultOverlap       int           `json:"default_overlap"`
	SimilarityThreshold  float32       `json:"similarity_threshold"`
	MaxResults           int           `json:"max_results"`
	MaxContextLength     int           `json:"max_context_length"`
	EmbeddingModel       string        `json:"embedding_model"`
	EnableSemanticSearch bool          `json:"enable_semantic_search"`
	EnableHybridSearch   bool          `json:"enable_hybrid_search"`
	CacheTTL             time.Duration `json:"cache_ttl"`
}

// EmbeddingClient handles text embedding generation
type EmbeddingClient struct {
	modelName string
	provider  string
	config    map[string]interface{}
}

// NewRAGService creates a new RAG service
func NewRAGService(db *gorm.DB, cache MultiLevelCache, config *RAGConfig) *RAGService {
	if config == nil {
		config = &RAGConfig{
			DefaultChunkSize:     512,
			DefaultOverlap:       50,
			SimilarityThreshold:  0.7,
			MaxResults:           10,
			MaxContextLength:     4000,
			EmbeddingModel:       "text-embedding-ada-002",
			EnableSemanticSearch: true,
			EnableHybridSearch:   true,
			CacheTTL:             1 * time.Hour,
		}
	}

	return &RAGService{
		db:              db,
		embeddingClient: NewEmbeddingClient(config.EmbeddingModel, "openai", nil),
		cache:           cache,
		logger:          utils.GetLogger(),
		config:          config,
	}
}

// NewEmbeddingClient creates a new embedding client
func NewEmbeddingClient(modelName, provider string, config map[string]interface{}) *EmbeddingClient {
	return &EmbeddingClient{
		modelName: modelName,
		provider:  provider,
		config:    config,
	}
}

// ProcessSessionForRAG processes an audio session to create searchable chunks
func (rs *RAGService) ProcessSessionForRAG(ctx context.Context, sessionID string) error {
	rs.logger.WithFields(map[string]interface{}{
		"session_id": sessionID,
		"operation":  "process_session_rag",
	}).Info("Starting RAG processing for session")

	// Parse sessionID to UUID
	sessionUUID, err := uuid.Parse(sessionID)
	if err != nil {
		return fmt.Errorf("invalid session ID: %w", err)
	}

	// Create RAG session
	ragSession := &models.RAGSession{
		SessionID:      sessionUUID,
		Status:         "processing",
		StartedAt:      time.Now(),
		EmbeddingModel: rs.config.EmbeddingModel,
	}

	if err := rs.db.Create(ragSession).Error; err != nil {
		return fmt.Errorf("failed to create RAG session: %w", err)
	}

	// Get transcript segments
	var segments []models.TranscriptSegmentDB
	if err := rs.db.Where("session_id = ?", sessionID).Find(&segments).Error; err != nil {
		return rs.updateRAGSessionError(ragSession.ID, fmt.Sprintf("failed to fetch segments: %v", err))
	}

	// Get summary
	var summary models.SummaryDB
	if err := rs.db.Where("session_id = ?", sessionID).First(&summary).Error; err != nil && err != gorm.ErrRecordNotFound {
		return rs.updateRAGSessionError(ragSession.ID, fmt.Sprintf("failed to fetch summary: %v", err))
	}

	// Process transcript chunks
	if err := rs.processTranscriptChunks(ctx, sessionID, segments, ragSession); err != nil {
		return rs.updateRAGSessionError(ragSession.ID, fmt.Sprintf("failed to process transcript chunks: %v", err))
	}

	// Process summary chunks
	if summary.ID != uuid.Nil {
		if err := rs.processSummaryChunks(ctx, sessionUUID, summary, ragSession); err != nil {
			return rs.updateRAGSessionError(ragSession.ID, fmt.Sprintf("failed to process summary chunks: %v", err))
		}
	}

	// Update RAG session as completed
	return rs.updateRAGSessionCompleted(ragSession.ID, len(segments))
}

// processTranscriptChunks processes transcript segments into searchable chunks
func (rs *RAGService) processTranscriptChunks(ctx context.Context, sessionID uuid.UUID, segments []models.TranscriptSegmentDB, ragSession *models.RAGSession) error {
	var chunks []models.DocumentChunk
	chunkIndex := 0

	for _, segment := range segments {
		// Create chunk from segment
		chunk := models.DocumentChunk{
			SessionID:   sessionID,
			Content:     segment.Text,
			ContentType: "transcript",
			ChunkIndex:  chunkIndex,
			ChunkSize:   len(segment.Text),
			Metadata:    rs.createSegmentMetadata(segment),
		}

		// Generate embedding
		embedding, err := rs.generateEmbedding(ctx, segment.Text)
		if err != nil {
			rs.logger.WithFields(map[string]interface{}{
				"session_id": sessionID,
				"segment_id": segment.ID,
				"error":      err.Error(),
			}).Error("Failed to generate embedding for segment")
			continue
		}
		chunk.Embedding = embedding

		chunks = append(chunks, chunk)
		chunkIndex++
	}

	// Save chunks to database
	if len(chunks) > 0 {
		if err := rs.db.CreateInBatches(chunks, 100).Error; err != nil {
			return fmt.Errorf("failed to save transcript chunks: %w", err)
		}
	}

	// Update RAG session progress
	ragSession.ProcessedChunks += len(chunks)
	return rs.db.Save(ragSession).Error
}

// processSummaryChunks processes summary into searchable chunks
func (rs *RAGService) processSummaryChunks(ctx context.Context, sessionID string, summary models.SummaryDB, ragSession *models.RAGSession) error {
	// Split summary into chunks if it's long
	summaryChunks := rs.splitTextIntoChunks(summary.Text, rs.config.DefaultChunkSize, rs.config.DefaultOverlap)

	for i, chunkText := range summaryChunks {
		chunk := models.DocumentChunk{
			SessionID:   sessionID,
			Content:     chunkText,
			ContentType: "summary",
			ChunkIndex:  i,
			ChunkSize:   len(chunkText),
			Metadata:    rs.createSummaryMetadata(summary),
		}

		// Generate embedding
		embedding, err := rs.generateEmbedding(ctx, chunkText)
		if err != nil {
			rs.logger.WithFields(map[string]interface{}{
				"session_id":  sessionID,
				"chunk_index": i,
				"error":       err.Error(),
			}).Error("Failed to generate embedding for summary chunk")
			continue
		}
		chunk.Embedding = embedding

		// Save chunk
		if err := rs.db.Create(&chunk).Error; err != nil {
			return fmt.Errorf("failed to save summary chunk: %w", err)
		}
	}

	// Update RAG session progress
	ragSession.ProcessedChunks += len(summaryChunks)
	return rs.db.Save(ragSession).Error
}

// QueryRAG performs a RAG query with context retrieval
func (rs *RAGService) QueryRAG(ctx context.Context, query models.RAGQuery) (*models.RAGResponse, error) {
	startTime := time.Now()

	// Check cache first
	cacheKey := fmt.Sprintf("rag_query:%s:%s", query.SessionID, utils.SimpleHash(query.Query))
	if cached, err := rs.cache.Get(ctx, cacheKey); err == nil && cached != nil {
		var response models.RAGResponse
		if err := json.Unmarshal(cached, &response); err == nil {
			rs.logger.WithFields(map[string]interface{}{
				"session_id": query.SessionID,
				"cache_hit":  true,
			}).Info("RAG query served from cache")
			return &response, nil
		}
	}

	// Perform vector search
	searchResults, err := rs.vectorSearch(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	// Build context from search results
	context := rs.buildContextFromResults(searchResults, query.Options.MaxResults)

	// Generate response using LLM
	response, err := rs.generateRAGResponse(ctx, query, context, searchResults)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RAG response: %w", err)
	}

	// Cache the response
	if responseBytes, err := json.Marshal(response); err == nil {
		rs.cache.Set(ctx, cacheKey, responseBytes, rs.config.CacheTTL)
	}

	// Save conversation context
	rs.saveConversationContext(query, response, searchResults)

	response.ProcessingTime = time.Since(startTime)
	return response, nil
}

// vectorSearch performs semantic search using vector similarity
func (rs *RAGService) vectorSearch(ctx context.Context, query models.RAGQuery) ([]models.VectorSearchResult, error) {
	// Generate query embedding
	queryEmbedding, err := rs.generateEmbedding(ctx, query.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	// Get document chunks for the session
	var chunks []models.DocumentChunk
	dbQuery := rs.db.Where("session_id = ?", query.SessionID)

	// Apply filters
	if len(query.Filters.ContentTypes) > 0 {
		dbQuery = dbQuery.Where("content_type IN ?", query.Filters.ContentTypes)
	}

	if err := dbQuery.Find(&chunks).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch document chunks: %w", err)
	}

	// Calculate similarities and rank results
	var results []models.VectorSearchResult
	for _, chunk := range chunks {
		similarity := rs.calculateCosineSimilarity(queryEmbedding, chunk.Embedding)

		if similarity >= query.Options.SimilarityThreshold {
			metadata := make(map[string]interface{})
			if chunk.Metadata != "" {
				json.Unmarshal([]byte(chunk.Metadata), &metadata)
			}

			results = append(results, models.VectorSearchResult{
				ChunkID:    chunk.ID.String(),
				Content:    chunk.Content,
				Similarity: similarity,
				Metadata:   metadata,
				Source:     chunk.ContentType,
			})
		}
	}

	// Sort by similarity (descending)
	rs.sortResultsBySimilarity(results)

	// Limit results
	if len(results) > query.Options.MaxResults {
		results = results[:query.Options.MaxResults]
	}

	return results, nil
}

// generateEmbedding generates text embeddings
func (rs *RAGService) generateEmbedding(ctx context.Context, text string) ([]float32, error) {
	// For now, return a mock embedding
	// In production, this would call OpenAI, local model, or HuggingFace
	dimensions := 1536 // OpenAI ada-002 dimensions

	// Simple hash-based mock embedding for demonstration
	hash := utils.SimpleHash(text)
	embedding := make([]float32, dimensions)

	for i := 0; i < dimensions; i++ {
		// Generate deterministic "random" values based on hash
		seed := (hash + int64(i)) % 1000
		embedding[i] = float32(seed) / 1000.0
	}

	// Normalize to unit vector
	rs.normalizeVector(embedding)

	return embedding, nil
}

// calculateCosineSimilarity calculates cosine similarity between two vectors
func (rs *RAGService) calculateCosineSimilarity(vec1, vec2 []float32) float32 {
	if len(vec1) != len(vec2) {
		return 0.0
	}

	var dotProduct, norm1, norm2 float32
	for i := 0; i < len(vec1); i++ {
		dotProduct += vec1[i] * vec2[i]
		norm1 += vec1[i] * vec1[i]
		norm2 += vec2[i] * vec2[i]
	}

	if norm1 == 0 || norm2 == 0 {
		return 0.0
	}

	return dotProduct / (float32(math.Sqrt(float64(norm1))) * float32(math.Sqrt(float64(norm2))))
}

// normalizeVector normalizes a vector to unit length
func (rs *RAGService) normalizeVector(vec []float32) {
	var norm float32
	for _, v := range vec {
		norm += v * v
	}

	if norm > 0 {
		norm = float32(math.Sqrt(float64(norm)))
		for i := range vec {
			vec[i] /= norm
		}
	}
}

// buildContextFromResults builds context string from search results
func (rs *RAGService) buildContextFromResults(results []models.VectorSearchResult, maxResults int) string {
	var contextParts []string

	for i, result := range results {
		if i >= maxResults {
			break
		}

		contextParts = append(contextParts, fmt.Sprintf("Source %d (%s): %s", i+1, result.Source, result.Content))
	}

	return strings.Join(contextParts, "\n\n")
}

// generateRAGResponse generates response using LLM with retrieved context
func (rs *RAGService) generateRAGResponse(ctx context.Context, query models.RAGQuery, context string, sources []models.VectorSearchResult) (*models.RAGResponse, error) {
	// Build prompt with context
	prompt := fmt.Sprintf(`Based on the following context, answer the question: "%s"

Context:
%s

Answer:`, query.Query, context)

	// For now, return a mock response
	// In production, this would call the LLM service
	response := &models.RAGResponse{
		QueryID:    utils.GenerateUUID(),
		Response:   fmt.Sprintf("Based on the provided context, here's what I found regarding your question: '%s'. The context contains relevant information that addresses your query.", query.Query),
		Context:    context,
		Sources:    sources,
		Confidence: 0.85,
		ModelUsed:  "llama-2-7b-chat",
		TokensUsed: len(prompt) + 100, // Approximate
		Metadata: map[string]interface{}{
			"query_type":     "rag",
			"context_length": len(context),
			"sources_count":  len(sources),
		},
	}

	return response, nil
}

// saveConversationContext saves the conversation for future reference
func (rs *RAGService) saveConversationContext(query models.RAGQuery, response *models.RAGResponse, sources []models.VectorSearchResult) {
	sourceIDs := make([]string, len(sources))
	for i, source := range sources {
		sourceIDs[i] = source.ChunkID
	}

	sourcesJSON, _ := json.Marshal(sourceIDs)

	conversationContext := &models.ConversationContext{
		SessionID:  query.SessionID,
		Query:      query.Query,
		Context:    response.Context,
		Response:   response.Response,
		Relevance:  response.Confidence,
		Sources:    string(sourcesJSON),
		ModelUsed:  response.ModelUsed,
		TokensUsed: response.TokensUsed,
	}

	if err := rs.db.Create(conversationContext).Error; err != nil {
		rs.logger.WithFields(map[string]interface{}{
			"session_id": query.SessionID,
			"error":      err.Error(),
		}).Error("Failed to save conversation context")
	}
}

// splitTextIntoChunks splits text into overlapping chunks
func (rs *RAGService) splitTextIntoChunks(text string, chunkSize, overlap int) []string {
	if len(text) <= chunkSize {
		return []string{text}
	}

	var chunks []string
	start := 0

	for start < len(text) {
		end := start + chunkSize
		if end > len(text) {
			end = len(text)
		}

		chunks = append(chunks, text[start:end])

		start = end - overlap
		if start >= len(text) {
			break
		}
	}

	return chunks
}

// sortResultsBySimilarity sorts results by similarity score
func (rs *RAGService) sortResultsBySimilarity(results []models.VectorSearchResult) {
	// Simple bubble sort for demonstration
	// In production, use sort.Slice for better performance
	for i := 0; i < len(results)-1; i++ {
		for j := 0; j < len(results)-i-1; j++ {
			if results[j].Similarity < results[j+1].Similarity {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}
}

// createSegmentMetadata creates metadata for transcript segments
func (rs *RAGService) createSegmentMetadata(segment models.TranscriptSegmentDB) string {
	metadata := map[string]interface{}{
		"speaker":    segment.Speaker,
		"start_time": segment.StartTime,
		"end_time":   segment.EndTime,
		"confidence": segment.Confidence,
		"type":       "transcript_segment",
	}

	metadataJSON, _ := json.Marshal(metadata)
	return string(metadataJSON)
}

// createSummaryMetadata creates metadata for summary chunks
func (rs *RAGService) createSummaryMetadata(summary models.SummaryDB) string {
	metadata := map[string]interface{}{
		"model_used": summary.ModelUsed,
		"sentiment":  summary.Sentiment,
		"type":       "summary",
	}

	metadataJSON, _ := json.Marshal(metadata)
	return string(metadataJSON)
}

// updateRAGSessionError updates RAG session with error status
func (rs *RAGService) updateRAGSessionError(sessionID utils.UUID, errorMsg string) error { // Changed from uuid.UUID to utils.UUID
	return rs.db.Model(&models.RAGSession{}).Where("id = ?", sessionID).Updates(map[string]interface{}{
		"status": "failed",
		"error":  errorMsg,
	}).Error
}

// updateRAGSessionCompleted updates RAG session as completed
func (rs *RAGService) updateRAGSessionCompleted(sessionID utils.UUID, totalChunks int) error { // Changed from uuid.UUID to utils.UUID
	now := time.Now()
	return rs.db.Model(&models.RAGSession{}).Where("id = ?", sessionID).Updates(map[string]interface{}{
		"status":       "completed",
		"total_chunks": totalChunks,
		"completed_at": &now,
	}).Error
}

// GetRAGSessionStatus gets the status of a RAG processing session
func (rs *RAGService) GetRAGSessionStatus(sessionID string) (*models.RAGSession, error) {
	var session models.RAGSession
	err := rs.db.Where("session_id = ?", sessionID).First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// GetSessionChunks retrieves all chunks for a session
func (rs *RAGService) GetSessionChunks(sessionID string) ([]models.DocumentChunk, error) {
	var chunks []models.DocumentChunk
	err := rs.db.Where("session_id = ?", sessionID).Order("chunk_index").Find(&chunks).Error
	return chunks, err
}
