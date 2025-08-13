"""
Transcription service that coordinates audio processing and Whisper model inference.
Handles the complete pipeline from audio input to transcribed text with speaker labels.
"""

import logging
import asyncio
from pathlib import Path
from typing import Union, Dict, Any, List, Optional
import time

from ..models import model_manager, TranscriptionResult, TranscriptionSegment
from ..models.whisper_model import create_whisper_model
from .audio_processor import audio_processor
from ..config import get_settings

logger = logging.getLogger(__name__)


class TranscriptionService:
    """Service for complete audio transcription with speaker separation."""
    
    def __init__(self):
        self.settings = get_settings()
        self.whisper_model = None
        self.is_initialized = False
    
    async def initialize(self) -> bool:
        """Initialize the transcription service with models."""
        try:
            logger.info("Initializing transcription service...")
            
            # Create and register Whisper model
            self.whisper_model = create_whisper_model(
                model_name=self.settings.whisper_model,
                use_fast=True
            )
            
            model_manager.register_model("whisper", self.whisper_model)
            
            # Load the model
            success = await model_manager.load_model("whisper")
            
            if success:
                self.is_initialized = True
                logger.info("Transcription service initialized successfully")
            else:
                logger.error("Failed to load Whisper model")
            
            return success
            
        except Exception as e:
            logger.error(f"Error initializing transcription service: {e}")
            return False
    
    async def shutdown(self) -> bool:
        """Shutdown the transcription service."""
        try:
            if self.whisper_model:
                await model_manager.unload_model("whisper")
            
            self.is_initialized = False
            logger.info("Transcription service shutdown complete")
            return True
            
        except Exception as e:
            logger.error(f"Error shutting down transcription service: {e}")
            return False
    
    async def is_ready(self) -> bool:
        """Check if the service is ready for transcription."""
        return (
            self.is_initialized and 
            self.whisper_model and 
            await self.whisper_model.is_ready()
        )
    
    async def transcribe_audio(
        self, 
        audio_path: Union[str, Path],
        **kwargs
    ) -> TranscriptionResult:
        """
        Transcribe audio file with speaker separation.
        
        Args:
            audio_path: Path to audio file
            **kwargs: Additional transcription options
            
        Returns:
            TranscriptionResult with speaker-labeled segments
        """
        if not await self.is_ready():
            raise RuntimeError("Transcription service not ready")
        
        start_time = time.time()
        
        try:
            logger.info(f"Starting transcription of: {audio_path}")
            
            # Step 1: Analyze audio quality
            quality_analysis = await audio_processor.analyze_audio_quality(audio_path)
            logger.info(f"Audio quality score: {quality_analysis.get('quality_score', 'unknown')}")
            
            # Step 2: Separate stereo channels
            left_file, right_file, metadata = await audio_processor.prepare_channels_for_transcription(audio_path)
            
            try:
                # Step 3: Transcribe each channel
                logger.info("Transcribing left channel...")
                left_result = await self.whisper_model.transcribe(left_file, **kwargs)
                
                logger.info("Transcribing right channel...")
                right_result = await self.whisper_model.transcribe(right_file, **kwargs)
                
                # Step 4: Combine results with proper speaker labels
                combined_result = await self._combine_channel_results(
                    left_result, 
                    right_result, 
                    metadata
                )
                
                # Step 5: Add quality metrics to metadata
                combined_result.processing_time = time.time() - start_time
                
                logger.info(f"Transcription complete: {len(combined_result.segments)} segments in {combined_result.processing_time:.2f}s")
                
                return combined_result
                
            finally:
                # Clean up temporary files
                try:
                    left_file.unlink(missing_ok=True)
                    right_file.unlink(missing_ok=True)
                except Exception as e:
                    logger.warning(f"Error cleaning up temp files: {e}")
            
        except Exception as e:
            logger.error(f"Transcription failed: {e}")
            raise
    
    async def _combine_channel_results(
        self,
        left_result: TranscriptionResult,
        right_result: TranscriptionResult,
        metadata: Dict[str, Any]
    ) -> TranscriptionResult:
        """Combine transcription results from left and right channels."""
        try:
            # Collect all segments
            all_segments = []
            
            # Add left channel segments with speaker label
            for segment in left_result.segments:
                segment.speaker = "left"
                all_segments.append(segment)
            
            # Add right channel segments with speaker label
            for segment in right_result.segments:
                segment.speaker = "right"
                all_segments.append(segment)
            
            # Sort segments by start time
            all_segments.sort(key=lambda x: x.start_time)
            
            # Merge nearby segments from same speaker
            merged_segments = await self._merge_nearby_segments(all_segments)
            
            # Calculate combined metadata
            total_duration = metadata.get('duration', 0)
            avg_confidence = (
                (left_result.confidence + right_result.confidence) / 2
                if left_result.confidence and right_result.confidence
                else max(left_result.confidence or 0, right_result.confidence or 0)
            )
            
            # Determine language (prefer non-None result)
            language = left_result.language or right_result.language
            
            return TranscriptionResult(
                segments=merged_segments,
                language=language,
                duration=total_duration,
                processing_time=0,  # Will be set by caller
                model_version=left_result.model_version,
                confidence=avg_confidence
            )
            
        except Exception as e:
            logger.error(f"Error combining channel results: {e}")
            raise
    
    async def _merge_nearby_segments(
        self, 
        segments: List[TranscriptionSegment],
        max_gap: float = 1.0
    ) -> List[TranscriptionSegment]:
        """Merge segments from the same speaker that are close together."""
        if not segments:
            return segments
        
        merged = []
        current_segment = segments[0]
        
        for next_segment in segments[1:]:
            # Check if segments can be merged
            same_speaker = current_segment.speaker == next_segment.speaker
            small_gap = (next_segment.start_time - current_segment.end_time) <= max_gap
            
            if same_speaker and small_gap:
                # Merge segments
                current_segment.end_time = next_segment.end_time
                current_segment.text += " " + next_segment.text.strip()
                current_segment.confidence = (current_segment.confidence + next_segment.confidence) / 2
            else:
                # Save current segment and start new one
                merged.append(current_segment)
                current_segment = next_segment
        
        # Add the last segment
        merged.append(current_segment)
        
        logger.debug(f"Merged {len(segments)} segments into {len(merged)} segments")
        return merged
    
    async def transcribe_batch(
        self, 
        audio_paths: List[Union[str, Path]],
        **kwargs
    ) -> List[TranscriptionResult]:
        """Transcribe multiple audio files in batch."""
        if not await self.is_ready():
            raise RuntimeError("Transcription service not ready")
        
        logger.info(f"Starting batch transcription of {len(audio_paths)} files")
        
        results = []
        
        for i, audio_path in enumerate(audio_paths):
            try:
                logger.info(f"Processing file {i+1}/{len(audio_paths)}: {audio_path}")
                result = await self.transcribe_audio(audio_path, **kwargs)
                results.append(result)
                
            except Exception as e:
                logger.error(f"Failed to transcribe {audio_path}: {e}")
                # Add empty result for failed transcription
                results.append(TranscriptionResult(
                    segments=[],
                    duration=0.0,
                    processing_time=0.0,
                    model_version="whisper-error",
                    confidence=0.0
                ))
        
        logger.info(f"Batch transcription complete: {len(results)} results")
        return results
    
    async def get_supported_formats(self) -> List[str]:
        """Get list of supported audio formats."""
        return [
            "wav", "mp3", "flac", "m4a", "ogg", 
            "aac", "wma", "aiff", "au", "mp4"
        ]
    
    async def validate_audio_file(self, audio_path: Union[str, Path]) -> Dict[str, Any]:
        """Validate audio file and return metadata."""
        try:
            # Basic file checks
            path = Path(audio_path)
            if not path.exists():
                return {"valid": False, "error": "File does not exist"}
            
            if not path.is_file():
                return {"valid": False, "error": "Path is not a file"}
            
            # Check file extension
            supported_formats = await self.get_supported_formats()
            if path.suffix.lower().lstrip('.') not in supported_formats:
                return {
                    "valid": False, 
                    "error": f"Unsupported format. Supported: {supported_formats}"
                }
            
            # Analyze audio
            quality_analysis = await audio_processor.analyze_audio_quality(audio_path)
            
            if "error" in quality_analysis:
                return {"valid": False, "error": quality_analysis["error"]}
            
            # Check duration limits
            max_duration = self.settings.max_audio_length
            if quality_analysis["duration"] > max_duration:
                return {
                    "valid": False,
                    "error": f"Audio too long: {quality_analysis['duration']:.1f}s > {max_duration}s"
                }
            
            return {
                "valid": True,
                "duration": quality_analysis["duration"],
                "sample_rate": quality_analysis["sample_rate"],
                "quality_score": quality_analysis["quality_score"],
                "file_size": path.stat().st_size
            }
            
        except Exception as e:
            return {"valid": False, "error": str(e)}
    
    async def get_service_status(self) -> Dict[str, Any]:
        """Get current service status and statistics."""
        model_info = model_manager.get_model_info("whisper")
        health = await model_manager.health_check()
        
        return {
            "initialized": self.is_initialized,
            "ready": await self.is_ready(),
            "model_info": model_info.dict() if model_info else None,
            "health": health,
            "settings": {
                "model_name": self.settings.whisper_model,
                "max_audio_length": self.settings.max_audio_length,
                "chunk_length": self.settings.chunk_length
            }
        }


# Global transcription service instance
transcription_service = TranscriptionService()
