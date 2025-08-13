import asyncio
import json
import logging
import numpy as np
import time
from typing import Dict, List, Optional, AsyncGenerator, Tuple
from pathlib import Path
import wave
import io
import threading
from queue import Queue, Empty
from dataclasses import dataclass
from datetime import datetime

import torch
import librosa
import whisper
from transformers import pipeline

from ..config import get_settings
from ..models.base import TranscriptionSegment, TranscriptionResult

logger = logging.getLogger(__name__)

@dataclass
class AudioChunk:
    """Represents a chunk of audio data."""
    data: np.ndarray
    timestamp: float
    sequence: int
    sample_rate: int
    channels: int
    is_final: bool = False

@dataclass
class StreamingSegment:
    """Represents a streaming transcription segment."""
    text: str
    start_time: float
    end_time: float
    confidence: float
    speaker: str
    is_interim: bool = True
    sequence: int = 0

class AudioBuffer:
    """Thread-safe audio buffer for streaming data."""
    
    def __init__(self, max_size_seconds: float = 30.0, sample_rate: int = 16000):
        self.max_size_seconds = max_size_seconds
        self.sample_rate = sample_rate
        self.max_samples = int(max_size_seconds * sample_rate)
        
        self._buffer = np.array([], dtype=np.float32)
        self._lock = threading.Lock()
        self._total_samples_added = 0
        
    def add_audio(self, audio_data: np.ndarray) -> None:
        """Add audio data to the buffer."""
        with self._lock:
            # Ensure audio is float32
            if audio_data.dtype != np.float32:
                audio_data = audio_data.astype(np.float32)
            
            # Normalize if needed
            if audio_data.max() > 1.0:
                audio_data = audio_data / 32768.0  # Convert from int16 range
            
            # Add to buffer
            self._buffer = np.concatenate([self._buffer, audio_data])
            self._total_samples_added += len(audio_data)
            
            # Trim buffer if too large
            if len(self._buffer) > self.max_samples:
                excess = len(self._buffer) - self.max_samples
                self._buffer = self._buffer[excess:]
                
    def get_audio(self, duration_seconds: float) -> Optional[np.ndarray]:
        """Get audio data for the specified duration."""
        with self._lock:
            samples_needed = int(duration_seconds * self.sample_rate)
            if len(self._buffer) >= samples_needed:
                audio = self._buffer[:samples_needed].copy()
                return audio
            return None
    
    def get_recent_audio(self, duration_seconds: float) -> Optional[np.ndarray]:
        """Get the most recent audio data."""
        with self._lock:
            samples_needed = int(duration_seconds * self.sample_rate)
            if len(self._buffer) >= samples_needed:
                audio = self._buffer[-samples_needed:].copy()
                return audio
            elif len(self._buffer) > 0:
                return self._buffer.copy()
            return None
    
    def clear(self) -> None:
        """Clear the buffer."""
        with self._lock:
            self._buffer = np.array([], dtype=np.float32)
    
    @property
    def duration_seconds(self) -> float:
        """Get current buffer duration in seconds."""
        with self._lock:
            return len(self._buffer) / self.sample_rate
    
    @property
    def total_duration_seconds(self) -> float:
        """Get total audio duration added to buffer."""
        with self._lock:
            return self._total_samples_added / self.sample_rate

class StreamingTranscriptionService:
    """Real-time streaming transcription service."""
    
    def __init__(self):
        self.settings = get_settings()
        self.device = self._get_device()
        
        # Initialize models
        self.whisper_model = None
        self.vad_model = None
        self._model_lock = threading.Lock()
        
        # Streaming configuration
        self.chunk_duration = 2.0  # Process 2-second chunks
        self.overlap_duration = 0.5  # 0.5-second overlap
        self.min_chunk_duration = 0.5  # Minimum chunk size
        self.max_silence_duration = 1.0  # Max silence before finalizing
        
        # Active streams
        self._active_streams: Dict[str, 'StreamingSession'] = {}
        self._stream_lock = threading.Lock()
        
        logger.info(f"Streaming transcription service initialized on {self.device}")
    
    def _get_device(self) -> str:
        """Determine the best device for processing."""
        if self.settings.device == "auto":
            if torch.cuda.is_available():
                return "cuda"
            elif hasattr(torch.backends, 'mps') and torch.backends.mps.is_available():
                return "mps"
            else:
                return "cpu"
        return self.settings.device
    
    async def initialize_models(self) -> None:
        """Initialize Whisper and VAD models."""
        with self._model_lock:
            if self.whisper_model is None:
                logger.info("Loading Whisper model for streaming...")
                
                # Use smaller model for real-time processing
                model_name = "base" if self.device == "cpu" else "small"
                self.whisper_model = whisper.load_model(model_name, device=self.device)
                
                logger.info(f"Whisper model '{model_name}' loaded on {self.device}")
            
            # Initialize VAD (Voice Activity Detection) - simplified version
            if self.vad_model is None:
                try:
                    # You could use a more sophisticated VAD model here
                    # For now, we'll use energy-based detection
                    self.vad_model = "energy_based"
                    logger.info("VAD model initialized (energy-based)")
                except Exception as e:
                    logger.warning(f"Failed to load VAD model: {e}")
                    self.vad_model = None
    
    async def start_stream(self, stream_id: str, config: Dict) -> 'StreamingSession':
        """Start a new streaming session."""
        await self.initialize_models()
        
        session = StreamingSession(
            stream_id=stream_id,
            transcription_service=self,
            config=config
        )
        
        with self._stream_lock:
            self._active_streams[stream_id] = session
        
        logger.info(f"Started streaming session: {stream_id}")
        return session
    
    async def stop_stream(self, stream_id: str) -> Optional[TranscriptionResult]:
        """Stop a streaming session and return final results."""
        with self._stream_lock:
            session = self._active_streams.pop(stream_id, None)
        
        if session:
            final_result = await session.finalize()
            logger.info(f"Stopped streaming session: {stream_id}")
            return final_result
        
        return None
    
    def get_active_streams(self) -> List[str]:
        """Get list of active stream IDs."""
        with self._stream_lock:
            return list(self._active_streams.keys())
    
    async def transcribe_chunk(
        self, 
        audio_data: np.ndarray, 
        language: Optional[str] = None
    ) -> List[StreamingSegment]:
        """Transcribe a single audio chunk."""
        if self.whisper_model is None:
            await self.initialize_models()
        
        try:
            # Ensure audio is the right format for Whisper
            if len(audio_data.shape) > 1:
                # Convert stereo to mono by averaging channels
                audio_data = np.mean(audio_data, axis=1)
            
            # Normalize audio
            if audio_data.max() > 1.0:
                audio_data = audio_data / audio_data.max()
            
            # Transcribe with Whisper
            with self._model_lock:
                result = self.whisper_model.transcribe(
                    audio_data,
                    language=language,
                    word_timestamps=True,
                    verbose=False
                )
            
            # Convert to streaming segments
            segments = []
            for i, segment in enumerate(result.get("segments", [])):
                # Extract speaker info (simplified - in production you'd use diarization)
                speaker = self._detect_speaker(audio_data, segment)
                
                streaming_segment = StreamingSegment(
                    text=segment["text"].strip(),
                    start_time=segment["start"],
                    end_time=segment["end"],
                    confidence=segment.get("avg_logprob", 0.0),
                    speaker=speaker,
                    is_interim=True,
                    sequence=i
                )
                segments.append(streaming_segment)
            
            return segments
            
        except Exception as e:
            logger.error(f"Error in chunk transcription: {e}")
            return []
    
    def _detect_speaker(self, audio_data: np.ndarray, segment: Dict) -> str:
        """Detect speaker for a segment (simplified implementation)."""
        # In production, you'd use proper speaker diarization
        # For now, we'll use a simple energy-based heuristic
        
        start_sample = int(segment["start"] * 16000)
        end_sample = int(segment["end"] * 16000)
        
        if start_sample < len(audio_data) and end_sample <= len(audio_data):
            segment_audio = audio_data[start_sample:end_sample]
            
            # Simple heuristic: higher energy = speaker 1, lower = speaker 2
            energy = np.mean(np.abs(segment_audio))
            return "left" if energy > 0.1 else "right"
        
        return "unknown"
    
    def _detect_voice_activity(self, audio_data: np.ndarray) -> bool:
        """Detect if audio contains voice activity."""
        if self.vad_model == "energy_based":
            # Simple energy-based VAD
            energy = np.mean(np.abs(audio_data))
            return energy > 0.01  # Threshold for voice activity
        
        return True  # Fallback to always processing

class StreamingSession:
    """Manages a single streaming transcription session."""
    
    def __init__(self, stream_id: str, transcription_service: StreamingTranscriptionService, config: Dict):
        self.stream_id = stream_id
        self.transcription_service = transcription_service
        self.config = config
        
        # Audio configuration
        self.sample_rate = config.get("sample_rate", 16000)
        self.channels = config.get("channels", 2)
        
        # Buffers
        self.audio_buffer = AudioBuffer(
            max_size_seconds=30.0,
            sample_rate=self.sample_rate
        )
        
        # Processing state
        self.last_processed_time = 0.0
        self.sequence_number = 0
        self.final_segments: List[StreamingSegment] = []
        self.interim_segments: List[StreamingSegment] = []
        
        # Threading
        self.processing_queue = Queue()
        self.result_queue = Queue()
        self.is_active = True
        self.processing_thread = None
        
        # Statistics
        self.start_time = time.time()
        self.total_audio_duration = 0.0
        self.total_chunks_processed = 0
        
        # Start processing thread
        self._start_processing_thread()
        
        logger.info(f"Streaming session initialized: {stream_id}")
    
    def _start_processing_thread(self) -> None:
        """Start the background processing thread."""
        self.processing_thread = threading.Thread(
            target=self._processing_worker,
            daemon=True
        )
        self.processing_thread.start()
    
    def _processing_worker(self) -> None:
        """Background worker for processing audio chunks."""
        while self.is_active:
            try:
                # Get next chunk to process
                chunk = self.processing_queue.get(timeout=1.0)
                if chunk is None:  # Shutdown signal
                    break
                
                # Process the chunk
                asyncio.run(self._process_chunk(chunk))
                
            except Empty:
                continue
            except Exception as e:
                logger.error(f"Error in processing worker: {e}")
    
    async def add_audio_data(self, audio_data: bytes) -> None:
        """Add raw audio data to the stream."""
        try:
            # Convert bytes to numpy array
            if self.channels == 2:
                # Stereo 16-bit PCM
                audio_array = np.frombuffer(audio_data, dtype=np.int16)
                audio_array = audio_array.reshape(-1, 2).astype(np.float32)
            else:
                # Mono 16-bit PCM
                audio_array = np.frombuffer(audio_data, dtype=np.int16).astype(np.float32)
            
            # Add to buffer
            self.audio_buffer.add_audio(audio_array)
            
            # Check if we should process a chunk
            current_duration = self.audio_buffer.duration_seconds
            time_since_last_process = current_duration - self.last_processed_time
            
            if time_since_last_process >= self.transcription_service.chunk_duration:
                await self._queue_chunk_for_processing()
                
        except Exception as e:
            logger.error(f"Error adding audio data: {e}")
    
    async def _queue_chunk_for_processing(self) -> None:
        """Queue an audio chunk for processing."""
        try:
            chunk_audio = self.audio_buffer.get_recent_audio(
                self.transcription_service.chunk_duration + self.transcription_service.overlap_duration
            )
            
            if chunk_audio is not None and len(chunk_audio) > 0:
                chunk = AudioChunk(
                    data=chunk_audio,
                    timestamp=time.time(),
                    sequence=self.sequence_number,
                    sample_rate=self.sample_rate,
                    channels=self.channels
                )
                
                self.processing_queue.put(chunk)
                self.sequence_number += 1
                self.last_processed_time = self.audio_buffer.duration_seconds
                
        except Exception as e:
            logger.error(f"Error queuing chunk for processing: {e}")
    
    async def _process_chunk(self, chunk: AudioChunk) -> None:
        """Process a single audio chunk."""
        try:
            start_time = time.time()
            
            # Check for voice activity
            has_voice = self.transcription_service._detect_voice_activity(chunk.data)
            if not has_voice:
                logger.debug(f"No voice activity detected in chunk {chunk.sequence}")
                return
            
            # Transcribe the chunk
            segments = await self.transcription_service.transcribe_chunk(chunk.data)
            
            # Adjust segment timestamps based on buffer position
            buffer_start_time = self.audio_buffer.total_duration_seconds - self.audio_buffer.duration_seconds
            
            for segment in segments:
                segment.start_time += buffer_start_time
                segment.end_time += buffer_start_time
                segment.sequence = chunk.sequence
            
            # Update interim segments
            self.interim_segments = segments
            
            # Put results in queue for retrieval
            result = {
                "type": "interim_transcription",
                "stream_id": self.stream_id,
                "segments": [self._segment_to_dict(s) for s in segments],
                "sequence": chunk.sequence,
                "processing_time": time.time() - start_time,
                "timestamp": chunk.timestamp
            }
            
            self.result_queue.put(result)
            self.total_chunks_processed += 1
            
            logger.debug(f"Processed chunk {chunk.sequence} in {time.time() - start_time:.3f}s")
            
        except Exception as e:
            logger.error(f"Error processing chunk {chunk.sequence}: {e}")
    
    async def get_results(self) -> AsyncGenerator[Dict, None]:
        """Get streaming transcription results."""
        while self.is_active:
            try:
                result = self.result_queue.get(timeout=0.1)
                yield result
            except Empty:
                await asyncio.sleep(0.1)
    
    def get_latest_results(self) -> List[Dict]:
        """Get all pending results."""
        results = []
        while True:
            try:
                result = self.result_queue.get_nowait()
                results.append(result)
            except Empty:
                break
        return results
    
    async def finalize(self) -> TranscriptionResult:
        """Finalize the session and return complete results."""
        self.is_active = False
        
        # Process any remaining audio
        if self.audio_buffer.duration_seconds > self.last_processed_time:
            await self._queue_chunk_for_processing()
            
            # Wait a bit for final processing
            await asyncio.sleep(1.0)
        
        # Stop processing thread
        self.processing_queue.put(None)  # Shutdown signal
        if self.processing_thread and self.processing_thread.is_alive():
            self.processing_thread.join(timeout=5.0)
        
        # Finalize segments (merge interim results)
        self._finalize_segments()
        
        # Create final result
        final_segments = [
            TranscriptionSegment(
                speaker=s.speaker,
                start_time=s.start_time,
                end_time=s.end_time,
                text=s.text,
                confidence=s.confidence
            )
            for s in self.final_segments
        ]
        
        result = TranscriptionResult(
            segments=final_segments,
            duration=self.audio_buffer.total_duration_seconds,
            language="auto",  # Could be detected
            processing_time=time.time() - self.start_time
        )
        
        logger.info(f"Streaming session finalized: {self.stream_id}")
        return result
    
    def _finalize_segments(self) -> None:
        """Finalize segments by merging and cleaning up interim results."""
        # For now, just mark interim segments as final
        # In production, you'd merge overlapping segments and clean up
        
        for segment in self.interim_segments:
            segment.is_interim = False
            self.final_segments.append(segment)
        
        # Sort by start time
        self.final_segments.sort(key=lambda s: s.start_time)
    
    def _segment_to_dict(self, segment: StreamingSegment) -> Dict:
        """Convert segment to dictionary."""
        return {
            "text": segment.text,
            "start_time": segment.start_time,
            "end_time": segment.end_time,
            "confidence": segment.confidence,
            "speaker": segment.speaker,
            "is_interim": segment.is_interim,
            "sequence": segment.sequence
        }
    
    def get_statistics(self) -> Dict:
        """Get session statistics."""
        return {
            "stream_id": self.stream_id,
            "duration": time.time() - self.start_time,
            "total_audio_duration": self.audio_buffer.total_duration_seconds,
            "current_buffer_duration": self.audio_buffer.duration_seconds,
            "chunks_processed": self.total_chunks_processed,
            "segments_count": len(self.final_segments) + len(self.interim_segments),
            "processing_ratio": self.total_chunks_processed / max(1, self.audio_buffer.total_duration_seconds),
            "is_active": self.is_active
        }

# Global service instance
_streaming_service: Optional[StreamingTranscriptionService] = None

def get_streaming_service() -> StreamingTranscriptionService:
    """Get the global streaming transcription service."""
    global _streaming_service
    if _streaming_service is None:
        _streaming_service = StreamingTranscriptionService()
    return _streaming_service
