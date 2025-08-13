# Audio Transcription & Summary PoC

## Overview

A proof-of-concept REST API service that processes stereo audio files to generate speaker-labeled transcripts with timestamps and AI-generated summaries. Built with Go for the API layer and Python for AI processing, designed to run fully on GPU servers without external dependencies.

## Technical Architecture

### Core Components
- **Go REST API**: High-performance API using Gin framework
- **Python AI Service**: Whisper Large V3 for ASR, Llama for summarization
- **Audio Processing**: Channel separation for speaker diarization
- **Docker Deployment**: GPU-enabled containerized service

### Technology Stack
- **Backend API**: Go 1.21+ with Gin web framework
- **AI Processing**: Python 3.11+ with PyTorch, Transformers, Librosa
- **Audio Format**: Stereo WAV files (left/right channel separation)
- **Models**: Whisper Large V3, Llama (local deployment)
- **Containerization**: Docker with NVIDIA GPU support

## API Specification

### Endpoints

#### POST /transcribe
Upload and process audio file for transcription and summary.

**Request:**
- Content-Type: `multipart/form-data`
- Field: `audio` (WAV file, stereo, 16-bit recommended)

**Response:**
```json
{
  "transcript": {
    "segments": [
      {
        "speaker": "left|right",
        "start_time": 0.0,
        "end_time": 2.5,
        "text": "Hello, how can I help you today?"
      }
    ]
  },
  "summary": {
    "text": "AI-generated summary of the conversation",
    "key_points": ["point1", "point2"],
    "sentiment": "positive|neutral|negative"
  },
  "metadata": {
    "duration": 120.5,
    "processing_time": 8.2,
    "model_versions": {
      "whisper": "large-v3",
      "llama": "7b"
    }
  }
}
```

#### GET /health
Service health check endpoint.

## Speaker Diarization Strategy

For stereo WAV files with speakers on separate channels:
1. **Channel Separation**: Split left/right channels into mono tracks
2. **Independent Processing**: Process each channel through Whisper
3. **Timestamp Alignment**: Merge results with speaker labels ("left"/"right")
4. **Validation**: Ensure temporal consistency and gap handling

## Performance Requirements

- **Processing Time**: < 1/4 of audio duration for transcription
- **Memory Usage**: Efficient GPU memory management
- **File Size Limits**: Support up to 1GB WAV files
- **Concurrent Requests**: Handle multiple simultaneous uploads

## Development Phases

### Phase 1: Core Infrastructure
- [ ] Go API server with audio upload handling
- [ ] Docker environment with GPU support
- [ ] Basic audio file validation and storage

### Phase 2: AI Integration
- [ ] Whisper Large V3 integration
- [ ] Channel separation and processing
- [ ] Llama summarization pipeline

### Phase 3: Integration & Testing
- [ ] End-to-end API integration
- [ ] Comprehensive test suite
- [ ] Performance optimization

## File Structure
```
pocwhisp/
├── api/                    # Go REST API
│   ├── main.go
│   ├── handlers/
│   ├── models/
│   └── utils/
├── ai/                     # Python AI services
│   ├── transcription.py
│   ├── summarization.py
│   ├── audio_processing.py
│   └── requirements.txt
├── docker/
│   ├── Dockerfile.api
│   ├── Dockerfile.ai
│   └── docker-compose.yml
├── tests/
│   ├── test_audio/
│   └── test_script.py
├── docs/
└── README.md
```

## Future Roadmap

### Immediate Extensions
- Batch processing capabilities
- Async job queuing with callbacks
- Network resilience and recovery
- Parallel processing optimization

### Advanced Features
- Multi-GPU scaling with Kubernetes
- Live streaming transcription
- RAG-based conversation analysis
- Real-time agent assistance
- Advanced speaker identification
- Custom model fine-tuning

## Success Metrics

- **Accuracy**: >95% transcription accuracy on clear audio
- **Performance**: Real-time processing (4x speed minimum)
- **Reliability**: 99.9% uptime for core transcription service
- **Scalability**: Linear performance scaling with GPU resources

## Deliverables

1. **Dockerized Service**: Complete containerized application
2. **API Documentation**: Comprehensive endpoint documentation
3. **Test Suite**: Automated testing with sample audio files
4. **README**: Setup and deployment instructions
5. **Sample Audio**: Test files demonstrating functionality

---

**Estimated Timeline**: 2-3 weeks
**Target Deployment**: GPU-enabled server environment
**Primary Use Case**: Call center conversation analysis
