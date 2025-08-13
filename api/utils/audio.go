package utils

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// WAVHeader represents the WAV file header structure
type WAVHeader struct {
	ChunkID       [4]byte // "RIFF"
	ChunkSize     uint32  // File size - 8
	Format        [4]byte // "WAVE"
	Subchunk1ID   [4]byte // "fmt "
	Subchunk1Size uint32  // Size of format chunk (16 for PCM)
	AudioFormat   uint16  // PCM = 1
	NumChannels   uint16  // Number of channels
	SampleRate    uint32  // Sample rate
	ByteRate      uint32  // Byte rate
	BlockAlign    uint16  // Block align
	BitsPerSample uint16  // Bits per sample
	Subchunk2ID   [4]byte // "data"
	Subchunk2Size uint32  // Size of data chunk
}

// AudioMetadata contains basic audio file information
type AudioMetadata struct {
	Duration      float64 // in seconds
	SampleRate    uint32  // samples per second
	Channels      uint16  // number of channels
	BitsPerSample uint16  // bits per sample
	FileSize      int64   // file size in bytes
	IsValid       bool    // whether file is valid WAV
}

// ValidateWAVFile checks if the uploaded file is a valid stereo WAV file
func ValidateWAVFile(filePath string) (*AudioMetadata, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file size
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	// Read WAV header
	var header WAVHeader
	err = binary.Read(file, binary.LittleEndian, &header)
	if err != nil {
		return nil, fmt.Errorf("failed to read WAV header: %w", err)
	}

	// Validate WAV format
	if string(header.ChunkID[:]) != "RIFF" {
		return nil, fmt.Errorf("invalid WAV file: missing RIFF header")
	}

	if string(header.Format[:]) != "WAVE" {
		return nil, fmt.Errorf("invalid WAV file: missing WAVE format")
	}

	if string(header.Subchunk1ID[:]) != "fmt " {
		return nil, fmt.Errorf("invalid WAV file: missing format chunk")
	}

	if header.AudioFormat != 1 {
		return nil, fmt.Errorf("unsupported audio format: only PCM is supported")
	}

	if header.NumChannels != 2 {
		return nil, fmt.Errorf("invalid number of channels: expected stereo (2 channels), got %d", header.NumChannels)
	}

	// Check if we already have data chunk info from header
	var dataChunkSize uint32
	if string(header.Subchunk2ID[:]) == "data" {
		dataChunkSize = header.Subchunk2Size
	} else {
		// Find data chunk (might not be immediately after format chunk)
		for {
			var chunkID [4]byte
			var chunkSize uint32

			err = binary.Read(file, binary.LittleEndian, &chunkID)
			if err == io.EOF {
				return nil, fmt.Errorf("data chunk not found")
			}
			if err != nil {
				return nil, fmt.Errorf("failed to read chunk ID: %w", err)
			}

			err = binary.Read(file, binary.LittleEndian, &chunkSize)
			if err != nil {
				return nil, fmt.Errorf("failed to read chunk size: %w", err)
			}

			if string(chunkID[:]) == "data" {
				dataChunkSize = chunkSize
				break
			}

			// Skip this chunk
			_, err = file.Seek(int64(chunkSize), io.SeekCurrent)
			if err != nil {
				return nil, fmt.Errorf("failed to skip chunk: %w", err)
			}
		}
	}

	// Calculate duration
	bytesPerSample := header.BitsPerSample / 8
	samplesPerSecond := header.SampleRate * uint32(header.NumChannels)
	totalSamples := dataChunkSize / uint32(bytesPerSample)
	duration := float64(totalSamples) / float64(samplesPerSecond)

	metadata := &AudioMetadata{
		Duration:      duration,
		SampleRate:    header.SampleRate,
		Channels:      header.NumChannels,
		BitsPerSample: header.BitsPerSample,
		FileSize:      fileInfo.Size(),
		IsValid:       true,
	}

	return metadata, nil
}

// ValidateAudioConstraints checks if audio meets processing requirements
func ValidateAudioConstraints(metadata *AudioMetadata) error {
	// Check minimum sample rate
	if metadata.SampleRate < 8000 {
		return fmt.Errorf("sample rate too low: minimum 8000 Hz, got %d Hz", metadata.SampleRate)
	}

	// Check maximum duration (30 minutes)
	maxDurationSeconds := 30 * 60 // 30 minutes
	if metadata.Duration > float64(maxDurationSeconds) {
		return fmt.Errorf("audio too long: maximum %d seconds, got %.1f seconds",
			maxDurationSeconds, metadata.Duration)
	}

	// Check minimum duration (1 second)
	if metadata.Duration < 1.0 {
		return fmt.Errorf("audio too short: minimum 1 second, got %.1f seconds", metadata.Duration)
	}

	// Check file size (1GB limit)
	maxFileSize := int64(1024 * 1024 * 1024) // 1GB
	if metadata.FileSize > maxFileSize {
		return fmt.Errorf("file too large: maximum 1GB, got %d bytes", metadata.FileSize)
	}

	// Check bits per sample
	if metadata.BitsPerSample != 16 && metadata.BitsPerSample != 24 && metadata.BitsPerSample != 32 {
		return fmt.Errorf("unsupported bit depth: expected 16, 24, or 32 bits, got %d bits",
			metadata.BitsPerSample)
	}

	return nil
}
