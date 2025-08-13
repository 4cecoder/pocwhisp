package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// AIClient handles communication with the Python AI service
type AIClient struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
}

// TranscriptionResponse represents the response from AI service
type TranscriptionResponse struct {
	Success       bool   `json:"success"`
	Filename      string `json:"filename"`
	Transcription struct {
		Segments []struct {
			Speaker    string  `json:"speaker"`
			StartTime  float64 `json:"start_time"`
			EndTime    float64 `json:"end_time"`
			Text       string  `json:"text"`
			Confidence float64 `json:"confidence"`
			Language   string  `json:"language,omitempty"`
		} `json:"segments"`
		Language       string  `json:"language,omitempty"`
		Duration       float64 `json:"duration"`
		ProcessingTime float64 `json:"processing_time"`
		ModelVersion   string  `json:"model_version"`
		Confidence     float64 `json:"confidence"`
	} `json:"transcription"`
	Validation map[string]interface{} `json:"validation"`
	Timestamp  float64                `json:"timestamp"`
}

// SummarizationRequest represents the request for summarization
type SummarizationRequest struct {
	Segments []struct {
		Speaker    string  `json:"speaker"`
		StartTime  float64 `json:"start_time"`
		EndTime    float64 `json:"end_time"`
		Text       string  `json:"text"`
		Confidence float64 `json:"confidence"`
		Language   string  `json:"language,omitempty"`
	} `json:"segments"`
	Language   string  `json:"language,omitempty"`
	Duration   float64 `json:"duration,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

// SummarizationResponse represents the response from summarization
type SummarizationResponse struct {
	Success bool `json:"success"`
	Summary struct {
		Text           string   `json:"text"`
		KeyPoints      []string `json:"key_points"`
		Sentiment      string   `json:"sentiment"`
		Confidence     float64  `json:"confidence"`
		ProcessingTime float64  `json:"processing_time"`
		ModelVersion   string   `json:"model_version"`
	} `json:"summary"`
	InputStats map[string]interface{} `json:"input_stats"`
	Timestamp  float64                `json:"timestamp"`
}

// HealthResponse represents AI service health status
type HealthResponse struct {
	Status    string                 `json:"status"`
	Timestamp float64                `json:"timestamp"`
	Services  map[string]interface{} `json:"services"`
	Models    map[string]interface{} `json:"models"`
	GPU       map[string]interface{} `json:"gpu"`
}

// NewAIClient creates a new AI service client
func NewAIClient(baseURL string, timeout time.Duration) *AIClient {
	if timeout == 0 {
		timeout = 60 * time.Second // Default 60 second timeout
	}

	return &AIClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// IsHealthy checks if the AI service is healthy and ready
func (c *AIClient) IsHealthy() (bool, error) {
	url := fmt.Sprintf("%s/health", c.baseURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return false, fmt.Errorf("failed to check AI service health: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}

	// Try to read the error response
	body, _ := io.ReadAll(resp.Body)
	return false, fmt.Errorf("AI service unhealthy (status %d): %s", resp.StatusCode, string(body))
}

// GetHealthStatus gets detailed health status from AI service
func (c *AIClient) GetHealthStatus() (*HealthResponse, error) {
	url := fmt.Sprintf("%s/health", c.baseURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get AI service health: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read health response: %w", err)
	}

	var healthResp HealthResponse
	if err := json.Unmarshal(body, &healthResp); err != nil {
		return nil, fmt.Errorf("failed to parse health response: %w", err)
	}

	return &healthResp, nil
}

// TranscribeAudio sends audio file to AI service for transcription
func (c *AIClient) TranscribeAudio(audioFilePath string) (*TranscriptionResponse, error) {
	// Open the audio file
	file, err := os.Open(audioFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open audio file: %w", err)
	}
	defer file.Close()

	// Create multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add the audio file
	part, err := writer.CreateFormFile("audio", filepath.Base(audioFilePath))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("failed to copy file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create the request
	url := fmt.Sprintf("%s/transcribe/", c.baseURL)
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send the request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send transcription request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read transcription response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("transcription failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var transcriptionResp TranscriptionResponse
	if err := json.Unmarshal(respBody, &transcriptionResp); err != nil {
		return nil, fmt.Errorf("failed to parse transcription response: %w", err)
	}

	return &transcriptionResp, nil
}

// SummarizeTranscription sends transcription to AI service for summarization
func (c *AIClient) SummarizeTranscription(request *SummarizationRequest) (*SummarizationResponse, error) {
	// Marshal request
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal summarization request: %w", err)
	}

	// Create request
	url := fmt.Sprintf("%s/summarize/", c.baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create summarization request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send summarization request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read summarization response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("summarization failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var summaryResp SummarizationResponse
	if err := json.Unmarshal(respBody, &summaryResp); err != nil {
		return nil, fmt.Errorf("failed to parse summarization response: %w", err)
	}

	return &summaryResp, nil
}

// Ping performs a simple connectivity test
func (c *AIClient) Ping() error {
	url := fmt.Sprintf("%s/health/live", c.baseURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("failed to ping AI service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("AI service ping failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}
