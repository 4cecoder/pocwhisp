package utils

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a minimal valid WAV file for testing
func createTestWAVFile(t *testing.T, sampleRate, channels, duration int) []byte {
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

	// Generate simple sine wave data
	for i := 0; i < dataSize/2; i++ {
		binary.Write(&buf, binary.LittleEndian, int16(0)) // Silent audio for simplicity
	}

	return buf.Bytes()
}

// Helper function to create an invalid WAV file
func createInvalidWAVFile() []byte {
	return []byte("This is not a WAV file")
}

func TestParseWAVHeader(t *testing.T) {
	t.Run("Valid WAV File", func(t *testing.T) {
		// Create a test WAV file: 44.1kHz, stereo, 1 second
		wavData := createTestWAVFile(t, 44100, 2, 1)
		reader := bytes.NewReader(wavData)

		header, err := ParseWAVHeader(reader)
		require.NoError(t, err)

		assert.Equal(t, uint32(44100), header.SampleRate)
		assert.Equal(t, uint16(2), header.Channels)
		assert.Equal(t, uint16(16), header.BitsPerSample)
		assert.Equal(t, uint16(1), header.AudioFormat) // PCM
		assert.True(t, header.DataSize > 0)
	})

	t.Run("Mono WAV File", func(t *testing.T) {
		wavData := createTestWAVFile(t, 16000, 1, 2)
		reader := bytes.NewReader(wavData)

		header, err := ParseWAVHeader(reader)
		require.NoError(t, err)

		assert.Equal(t, uint32(16000), header.SampleRate)
		assert.Equal(t, uint16(1), header.Channels)
		assert.Equal(t, uint16(16), header.BitsPerSample)
	})

	t.Run("Invalid RIFF Header", func(t *testing.T) {
		invalidData := []byte("INVALID_HEADER_DATA")
		reader := bytes.NewReader(invalidData)

		_, err := ParseWAVHeader(reader)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a RIFF file")
	})

	t.Run("Invalid WAVE Format", func(t *testing.T) {
		var buf bytes.Buffer
		buf.WriteString("RIFF")
		binary.Write(&buf, binary.LittleEndian, uint32(100))
		buf.WriteString("NOTW") // Invalid format

		reader := bytes.NewReader(buf.Bytes())
		_, err := ParseWAVHeader(reader)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a WAVE file")
	})

	t.Run("Truncated File", func(t *testing.T) {
		wavData := createTestWAVFile(t, 44100, 2, 1)
		truncatedData := wavData[:20] // Cut off most of the file
		reader := bytes.NewReader(truncatedData)

		_, err := ParseWAVHeader(reader)
		assert.Error(t, err)
	})

	t.Run("Empty File", func(t *testing.T) {
		reader := bytes.NewReader([]byte{})

		_, err := ParseWAVHeader(reader)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a RIFF file")
	})
}

func TestValidateWAVFile(t *testing.T) {
	t.Run("Valid Stereo WAV", func(t *testing.T) {
		wavData := createTestWAVFile(t, 44100, 2, 3)
		reader := bytes.NewReader(wavData)

		err := ValidateWAVFile(reader)
		assert.NoError(t, err)
	})

	t.Run("Valid Mono WAV", func(t *testing.T) {
		wavData := createTestWAVFile(t, 16000, 1, 5)
		reader := bytes.NewReader(wavData)

		err := ValidateWAVFile(reader)
		assert.NoError(t, err)
	})

	t.Run("Unsupported Channel Count", func(t *testing.T) {
		wavData := createTestWAVFile(t, 44100, 8, 1) // 8 channels
		reader := bytes.NewReader(wavData)

		err := ValidateWAVFile(reader)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported channel count")
	})

	t.Run("Invalid Sample Rate", func(t *testing.T) {
		wavData := createTestWAVFile(t, 8000, 2, 1) // 8kHz (too low)
		reader := bytes.NewReader(wavData)

		err := ValidateWAVFile(reader)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported sample rate")
	})

	t.Run("Unsupported Bit Depth", func(t *testing.T) {
		// Create a WAV with 8-bit samples
		var buf bytes.Buffer
		buf.WriteString("RIFF")
		binary.Write(&buf, binary.LittleEndian, uint32(1000))
		buf.WriteString("WAVE")
		buf.WriteString("fmt ")
		binary.Write(&buf, binary.LittleEndian, uint32(16))
		binary.Write(&buf, binary.LittleEndian, uint16(1))     // PCM
		binary.Write(&buf, binary.LittleEndian, uint16(2))     // Stereo
		binary.Write(&buf, binary.LittleEndian, uint32(44100)) // Sample rate
		binary.Write(&buf, binary.LittleEndian, uint32(88200)) // Byte rate
		binary.Write(&buf, binary.LittleEndian, uint16(2))     // Block align
		binary.Write(&buf, binary.LittleEndian, uint16(8))     // 8 bits per sample
		buf.WriteString("data")
		binary.Write(&buf, binary.LittleEndian, uint32(100))

		reader := bytes.NewReader(buf.Bytes())
		err := ValidateWAVFile(reader)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported bit depth")
	})

	t.Run("Non-PCM Format", func(t *testing.T) {
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

		reader := bytes.NewReader(buf.Bytes())
		err := ValidateWAVFile(reader)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "only PCM format is supported")
	})

	t.Run("Invalid WAV File", func(t *testing.T) {
		invalidData := createInvalidWAVFile()
		reader := bytes.NewReader(invalidData)

		err := ValidateWAVFile(reader)
		assert.Error(t, err)
	})
}

func TestGetAudioDuration(t *testing.T) {
	t.Run("Calculate Duration", func(t *testing.T) {
		wavData := createTestWAVFile(t, 44100, 2, 5) // 5 seconds
		reader := bytes.NewReader(wavData)

		duration, err := GetAudioDuration(reader)
		require.NoError(t, err)

		// Should be approximately 5 seconds (allowing for small variations)
		assert.InDelta(t, 5.0, duration, 0.1)
	})

	t.Run("Short Audio", func(t *testing.T) {
		wavData := createTestWAVFile(t, 16000, 1, 1) // 1 second, mono
		reader := bytes.NewReader(wavData)

		duration, err := GetAudioDuration(reader)
		require.NoError(t, err)

		assert.InDelta(t, 1.0, duration, 0.1)
	})

	t.Run("Invalid WAV", func(t *testing.T) {
		invalidData := createInvalidWAVFile()
		reader := bytes.NewReader(invalidData)

		_, err := GetAudioDuration(reader)
		assert.Error(t, err)
	})
}

func TestValidateAudioFile(t *testing.T) {
	t.Run("Valid WAV File", func(t *testing.T) {
		wavData := createTestWAVFile(t, 44100, 2, 3)
		reader := bytes.NewReader(wavData)

		format, err := ValidateAudioFile(reader, "test.wav")
		require.NoError(t, err)
		assert.Equal(t, "wav", format)
	})

	t.Run("Unsupported File Extension", func(t *testing.T) {
		wavData := createTestWAVFile(t, 44100, 2, 1)
		reader := bytes.NewReader(wavData)

		_, err := ValidateAudioFile(reader, "test.xyz")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported file format")
	})

	t.Run("MP3 File Extension", func(t *testing.T) {
		// For MP3, we can't easily create valid test data,
		// but we can test the extension validation
		reader := bytes.NewReader([]byte("fake mp3 data"))

		_, err := ValidateAudioFile(reader, "test.mp3")
		// Should accept MP3 extension but may fail on content validation
		// (this is expected since we're providing fake data)
		// The important thing is that the extension is recognized
		if err != nil {
			assert.NotContains(t, err.Error(), "unsupported file format")
		}
	})

	t.Run("Case Insensitive Extension", func(t *testing.T) {
		wavData := createTestWAVFile(t, 44100, 2, 1)
		reader := bytes.NewReader(wavData)

		format, err := ValidateAudioFile(reader, "test.WAV")
		require.NoError(t, err)
		assert.Equal(t, "wav", format)
	})

	t.Run("No Extension", func(t *testing.T) {
		wavData := createTestWAVFile(t, 44100, 2, 1)
		reader := bytes.NewReader(wavData)

		_, err := ValidateAudioFile(reader, "test_file_without_extension")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported file format")
	})
}

// Test file operations
func TestFileOperations(t *testing.T) {
	t.Run("Create and Validate WAV File", func(t *testing.T) {
		// Create temporary directory
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test.wav")

		// Create test WAV data
		wavData := createTestWAVFile(t, 44100, 2, 2)

		// Write to file
		err := os.WriteFile(testFile, wavData, 0644)
		require.NoError(t, err)

		// Open and validate
		file, err := os.Open(testFile)
		require.NoError(t, err)
		defer file.Close()

		// Test header parsing
		header, err := ParseWAVHeader(file)
		require.NoError(t, err)
		assert.Equal(t, uint32(44100), header.SampleRate)

		// Reset file position
		_, err = file.Seek(0, io.SeekStart)
		require.NoError(t, err)

		// Test validation
		err = ValidateWAVFile(file)
		assert.NoError(t, err)

		// Reset file position
		_, err = file.Seek(0, io.SeekStart)
		require.NoError(t, err)

		// Test duration calculation
		duration, err := GetAudioDuration(file)
		require.NoError(t, err)
		assert.InDelta(t, 2.0, duration, 0.1)
	})
}

// Benchmark tests
func BenchmarkParseWAVHeader(b *testing.B) {
	wavData := createTestWAVFile(b, 44100, 2, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := bytes.NewReader(wavData)
		_, err := ParseWAVHeader(reader)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateWAVFile(b *testing.B) {
	wavData := createTestWAVFile(b, 44100, 2, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := bytes.NewReader(wavData)
		err := ValidateWAVFile(reader)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetAudioDuration(b *testing.B) {
	wavData := createTestWAVFile(b, 44100, 2, 5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := bytes.NewReader(wavData)
		_, err := GetAudioDuration(reader)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Edge case tests
func TestWAVHeaderEdgeCases(t *testing.T) {
	t.Run("Maximum Values", func(t *testing.T) {
		// Test with high sample rate and channels within supported range
		wavData := createTestWAVFile(t, 96000, 2, 1) // High quality audio
		reader := bytes.NewReader(wavData)

		header, err := ParseWAVHeader(reader)
		require.NoError(t, err)
		assert.Equal(t, uint32(96000), header.SampleRate)
	})

	t.Run("Minimum Supported Values", func(t *testing.T) {
		wavData := createTestWAVFile(t, 16000, 1, 1) // Minimum quality
		reader := bytes.NewReader(wavData)

		err := ValidateWAVFile(reader)
		assert.NoError(t, err)
	})

	t.Run("Large Audio File", func(t *testing.T) {
		// Create a larger file to test performance
		wavData := createTestWAVFile(t, 44100, 2, 60) // 1 minute
		reader := bytes.NewReader(wavData)

		duration, err := GetAudioDuration(reader)
		require.NoError(t, err)
		assert.InDelta(t, 60.0, duration, 1.0) // Allow 1 second tolerance
	})
}
