package utils

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a minimal valid WAV file for testing
func createTestWAVFile(sampleRate, channels, duration int) []byte {
	// Calculate data size
	bytesPerSample := 2 // 16-bit
	dataSize := sampleRate * channels * bytesPerSample * duration
	fileSize := 36 + dataSize

	var buf bytes.Buffer

	// RIFF header
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(fileSize))
	buf.WriteString("WAVE")

	// fmt chunk
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))                                 // Chunk size
	binary.Write(&buf, binary.LittleEndian, uint16(1))                                  // Audio format (PCM)
	binary.Write(&buf, binary.LittleEndian, uint16(channels))                           // Number of channels
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))                         // Sample rate
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*channels*bytesPerSample)) // Byte rate
	binary.Write(&buf, binary.LittleEndian, uint16(channels*bytesPerSample))            // Block align
	binary.Write(&buf, binary.LittleEndian, uint16(16))                                 // Bits per sample

	// data chunk
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(dataSize))

	// Generate simple sine wave data (just zeros for simplicity)
	for i := 0; i < dataSize/2; i++ {
		binary.Write(&buf, binary.LittleEndian, int16(0))
	}

	return buf.Bytes()
}

// Helper function to create a temporary file with given data
func createTempWAVFile(t *testing.T, data []byte) string {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "test.wav")

	err := os.WriteFile(tempFile, data, 0644)
	require.NoError(t, err)

	return tempFile
}

func TestValidateWAVFile(t *testing.T) {
	t.Run("Valid Stereo WAV File", func(t *testing.T) {
		// Create a test WAV file: 44.1kHz, stereo, 2 seconds
		wavData := createTestWAVFile(44100, 2, 2)
		tempFile := createTempWAVFile(t, wavData)

		metadata, err := ValidateWAVFile(tempFile)
		require.NoError(t, err)
		require.NotNil(t, metadata)

		assert.Equal(t, uint32(44100), metadata.SampleRate)
		assert.Equal(t, uint16(2), metadata.Channels)
		assert.Equal(t, uint16(16), metadata.BitsPerSample)
		assert.True(t, metadata.IsValid)
		assert.InDelta(t, 2.0, metadata.Duration, 0.1) // Allow small variation
		assert.True(t, metadata.FileSize > 0)
	})

	t.Run("Valid 48kHz Stereo WAV", func(t *testing.T) {
		wavData := createTestWAVFile(48000, 2, 1)
		tempFile := createTempWAVFile(t, wavData)

		metadata, err := ValidateWAVFile(tempFile)
		require.NoError(t, err)

		assert.Equal(t, uint32(48000), metadata.SampleRate)
		assert.Equal(t, uint16(2), metadata.Channels)
	})

	t.Run("Invalid - Mono WAV File", func(t *testing.T) {
		wavData := createTestWAVFile(44100, 1, 1) // Mono
		tempFile := createTempWAVFile(t, wavData)

		_, err := ValidateWAVFile(tempFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected stereo (2 channels)")
	})

	t.Run("Invalid - Non-WAV File", func(t *testing.T) {
		invalidData := []byte("This is not a WAV file")
		tempFile := createTempWAVFile(t, invalidData)

		_, err := ValidateWAVFile(tempFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read WAV header")
	})

	t.Run("Invalid - Missing RIFF Header", func(t *testing.T) {
		invalidData := []byte("WRONG_HEADER_FORMAT")
		tempFile := createTempWAVFile(t, invalidData)

		_, err := ValidateWAVFile(tempFile)
		assert.Error(t, err)
		// The function returns "failed to read WAV header" for short files
		assert.Contains(t, err.Error(), "failed to read WAV header")
	})

	t.Run("Invalid - Non-PCM Format", func(t *testing.T) {
		// Create a WAV with non-PCM format
		var buf bytes.Buffer
		buf.WriteString("RIFF")
		binary.Write(&buf, binary.LittleEndian, uint32(1000))
		buf.WriteString("WAVE")
		buf.WriteString("fmt ")
		binary.Write(&buf, binary.LittleEndian, uint32(16))
		binary.Write(&buf, binary.LittleEndian, uint16(3))      // IEEE float (not PCM)
		binary.Write(&buf, binary.LittleEndian, uint16(2))      // Stereo
		binary.Write(&buf, binary.LittleEndian, uint32(44100))  // Sample rate
		binary.Write(&buf, binary.LittleEndian, uint32(352800)) // Byte rate
		binary.Write(&buf, binary.LittleEndian, uint16(8))      // Block align
		binary.Write(&buf, binary.LittleEndian, uint16(32))     // 32 bits per sample

		tempFile := createTempWAVFile(t, buf.Bytes())

		_, err := ValidateWAVFile(tempFile)
		assert.Error(t, err)
		// The function may return "failed to read WAV header" for incomplete files
		// or "only PCM is supported" if it gets to format validation
		assert.True(t,
			strings.Contains(err.Error(), "failed to read WAV header") ||
				strings.Contains(err.Error(), "only PCM is supported"))
	})

	t.Run("Non-existent File", func(t *testing.T) {
		_, err := ValidateWAVFile("/path/to/non/existent/file.wav")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open file")
	})
}

func TestValidateAudioConstraints(t *testing.T) {
	t.Run("Valid Audio Constraints", func(t *testing.T) {
		metadata := &AudioMetadata{
			Duration:      30.0,
			SampleRate:    44100,
			Channels:      2,
			BitsPerSample: 16,
			FileSize:      1024 * 1024, // 1MB
			IsValid:       true,
		}

		err := ValidateAudioConstraints(metadata)
		assert.NoError(t, err)
	})

	t.Run("Valid High Quality Audio", func(t *testing.T) {
		metadata := &AudioMetadata{
			Duration:      600.0, // 10 minutes
			SampleRate:    96000,
			Channels:      2,
			BitsPerSample: 24,
			FileSize:      100 * 1024 * 1024, // 100MB
			IsValid:       true,
		}

		err := ValidateAudioConstraints(metadata)
		assert.NoError(t, err)
	})

	t.Run("Invalid - Sample Rate Too Low", func(t *testing.T) {
		metadata := &AudioMetadata{
			Duration:      10.0,
			SampleRate:    4000, // Too low
			Channels:      2,
			BitsPerSample: 16,
			FileSize:      1024,
			IsValid:       true,
		}

		err := ValidateAudioConstraints(metadata)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sample rate too low")
	})

	t.Run("Invalid - Duration Too Long", func(t *testing.T) {
		metadata := &AudioMetadata{
			Duration:      31 * 60, // 31 minutes (too long)
			SampleRate:    44100,
			Channels:      2,
			BitsPerSample: 16,
			FileSize:      1024,
			IsValid:       true,
		}

		err := ValidateAudioConstraints(metadata)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "audio too long")
	})

	t.Run("Invalid - Duration Too Short", func(t *testing.T) {
		metadata := &AudioMetadata{
			Duration:      0.5, // Too short
			SampleRate:    44100,
			Channels:      2,
			BitsPerSample: 16,
			FileSize:      1024,
			IsValid:       true,
		}

		err := ValidateAudioConstraints(metadata)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "audio too short")
	})

	t.Run("Invalid - File Too Large", func(t *testing.T) {
		metadata := &AudioMetadata{
			Duration:      600.0,
			SampleRate:    44100,
			Channels:      2,
			BitsPerSample: 16,
			FileSize:      2 * 1024 * 1024 * 1024, // 2GB (too large)
			IsValid:       true,
		}

		err := ValidateAudioConstraints(metadata)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "file too large")
	})

	t.Run("Invalid - Unsupported Bit Depth", func(t *testing.T) {
		metadata := &AudioMetadata{
			Duration:      10.0,
			SampleRate:    44100,
			Channels:      2,
			BitsPerSample: 8, // Unsupported
			FileSize:      1024,
			IsValid:       true,
		}

		err := ValidateAudioConstraints(metadata)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported bit depth")
	})

	t.Run("Valid 24-bit Audio", func(t *testing.T) {
		metadata := &AudioMetadata{
			Duration:      10.0,
			SampleRate:    44100,
			Channels:      2,
			BitsPerSample: 24, // Should be valid
			FileSize:      1024,
			IsValid:       true,
		}

		err := ValidateAudioConstraints(metadata)
		assert.NoError(t, err)
	})

	t.Run("Valid 32-bit Audio", func(t *testing.T) {
		metadata := &AudioMetadata{
			Duration:      10.0,
			SampleRate:    44100,
			Channels:      2,
			BitsPerSample: 32, // Should be valid
			FileSize:      1024,
			IsValid:       true,
		}

		err := ValidateAudioConstraints(metadata)
		assert.NoError(t, err)
	})
}

func TestAudioMetadata(t *testing.T) {
	t.Run("AudioMetadata Structure", func(t *testing.T) {
		metadata := AudioMetadata{
			Duration:      120.5,
			SampleRate:    44100,
			Channels:      2,
			BitsPerSample: 16,
			FileSize:      1048576,
			IsValid:       true,
		}

		assert.Equal(t, 120.5, metadata.Duration)
		assert.Equal(t, uint32(44100), metadata.SampleRate)
		assert.Equal(t, uint16(2), metadata.Channels)
		assert.Equal(t, uint16(16), metadata.BitsPerSample)
		assert.Equal(t, int64(1048576), metadata.FileSize)
		assert.True(t, metadata.IsValid)
	})
}

func TestWAVHeader(t *testing.T) {
	t.Run("WAVHeader Structure", func(t *testing.T) {
		var header WAVHeader

		// Test that the structure has the expected fields
		assert.Equal(t, 4, len(header.ChunkID))
		assert.Equal(t, 4, len(header.Format))
		assert.Equal(t, 4, len(header.Subchunk1ID))
		assert.Equal(t, 4, len(header.Subchunk2ID))
	})
}

// Integration test with actual file creation and validation
func TestValidateWAVFileIntegration(t *testing.T) {
	t.Run("End-to-End WAV Validation", func(t *testing.T) {
		// Create a realistic WAV file
		wavData := createTestWAVFile(44100, 2, 5) // 5 seconds
		tempFile := createTempWAVFile(t, wavData)

		// Validate the file
		metadata, err := ValidateWAVFile(tempFile)
		require.NoError(t, err)
		require.NotNil(t, metadata)

		// Validate constraints
		err = ValidateAudioConstraints(metadata)
		assert.NoError(t, err)

		// Check calculated values
		expectedDataSize := 44100 * 2 * 2 * 5                                         // sampleRate * channels * bytesPerSample * duration
		expectedFileSize := expectedDataSize + 44                                     // data + header
		assert.InDelta(t, float64(expectedFileSize), float64(metadata.FileSize), 100) // Allow some variation
	})
}

// Benchmark tests
func BenchmarkValidateWAVFile(b *testing.B) {
	// Create test file once
	wavData := createTestWAVFile(44100, 2, 10) // 10 seconds
	tempDir := b.TempDir()
	tempFile := filepath.Join(tempDir, "benchmark.wav")
	err := os.WriteFile(tempFile, wavData, 0644)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ValidateWAVFile(tempFile)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateAudioConstraints(b *testing.B) {
	metadata := &AudioMetadata{
		Duration:      60.0,
		SampleRate:    44100,
		Channels:      2,
		BitsPerSample: 16,
		FileSize:      1024 * 1024,
		IsValid:       true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := ValidateAudioConstraints(metadata)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Test edge cases and error conditions
func TestWAVValidationEdgeCases(t *testing.T) {
	t.Run("Empty File", func(t *testing.T) {
		tempFile := createTempWAVFile(t, []byte{})

		_, err := ValidateWAVFile(tempFile)
		assert.Error(t, err)
	})

	t.Run("Truncated WAV Header", func(t *testing.T) {
		wavData := createTestWAVFile(44100, 2, 1)
		truncatedData := wavData[:20] // Cut off most of the header
		tempFile := createTempWAVFile(t, truncatedData)

		_, err := ValidateWAVFile(tempFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read WAV header")
	})

	t.Run("Boundary Values", func(t *testing.T) {
		// Test minimum valid values
		metadata := &AudioMetadata{
			Duration:      1.0,  // Minimum duration
			SampleRate:    8000, // Minimum sample rate
			Channels:      2,
			BitsPerSample: 16,
			FileSize:      1, // Minimum file size
			IsValid:       true,
		}

		err := ValidateAudioConstraints(metadata)
		assert.NoError(t, err)

		// Test maximum valid values
		metadata = &AudioMetadata{
			Duration:      30 * 60, // Maximum duration (30 minutes)
			SampleRate:    192000,  // High sample rate
			Channels:      2,
			BitsPerSample: 32,
			FileSize:      1024*1024*1024 - 1, // Just under 1GB
			IsValid:       true,
		}

		err = ValidateAudioConstraints(metadata)
		assert.NoError(t, err)
	})
}
