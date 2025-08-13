"""
Summarization API endpoints.
"""

import logging
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel
from typing import Dict, Any, List, Optional
import time

from ...services import summarization_service
from ...models import TranscriptionResult, TranscriptionSegment

logger = logging.getLogger(__name__)

router = APIRouter()


class TranscriptionInput(BaseModel):
    """Input model for transcription data."""
    segments: List[Dict[str, Any]]
    language: Optional[str] = None
    duration: Optional[float] = None
    confidence: Optional[float] = None


class TextInput(BaseModel):
    """Input model for direct text summarization."""
    text: str
    conversation_type: Optional[str] = None


@router.post("/")
async def summarize_transcription(
    transcription_data: TranscriptionInput
) -> Dict[str, Any]:
    """
    Generate summary from transcription data.
    
    Args:
        transcription_data: Transcription result to summarize
        
    Returns:
        Summary with key points, sentiment, and analysis
    """
    try:
        # Validate service readiness
        if not await summarization_service.is_ready():
            raise HTTPException(
                status_code=503,
                detail="Summarization service not ready"
            )
        
        # Convert input to TranscriptionResult
        segments = []
        for seg_data in transcription_data.segments:
            segment = TranscriptionSegment(
                speaker=seg_data.get("speaker", "unknown"),
                start_time=seg_data.get("start_time", 0.0),
                end_time=seg_data.get("end_time", 0.0),
                text=seg_data.get("text", ""),
                confidence=seg_data.get("confidence", 0.0),
                language=seg_data.get("language")
            )
            segments.append(segment)
        
        transcription = TranscriptionResult(
            segments=segments,
            language=transcription_data.language,
            duration=transcription_data.duration or 0.0,
            processing_time=0.0,
            model_version="input",
            confidence=transcription_data.confidence or 0.0
        )
        
        # Generate summary
        logger.info(f"Generating summary for {len(segments)} segments")
        summary_result = await summarization_service.summarize_transcription(
            transcription
        )
        
        # Format response
        response = {
            "success": True,
            "summary": {
                "text": summary_result.text,
                "key_points": summary_result.key_points,
                "sentiment": summary_result.sentiment,
                "confidence": summary_result.confidence,
                "processing_time": summary_result.processing_time,
                "model_version": summary_result.model_version
            },
            "input_stats": {
                "total_segments": len(segments),
                "total_duration": transcription.duration,
                "speaker_breakdown": _calculate_speaker_stats(segments)
            },
            "timestamp": time.time()
        }
        
        logger.info("Summary generation completed")
        return response
        
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Summarization failed: {e}")
        raise HTTPException(
            status_code=500,
            detail=f"Summarization failed: {str(e)}"
        )


@router.post("/text")
async def summarize_text(
    text_data: TextInput
) -> Dict[str, Any]:
    """
    Generate summary from raw text.
    
    Args:
        text_data: Text to summarize
        
    Returns:
        Summary with key points and analysis
    """
    try:
        # Validate service readiness
        if not await summarization_service.is_ready():
            raise HTTPException(
                status_code=503,
                detail="Summarization service not ready"
            )
        
        if not text_data.text.strip():
            raise HTTPException(
                status_code=400,
                detail="Text cannot be empty"
            )
        
        # Create mock transcription for processing
        mock_segment = TranscriptionSegment(
            speaker="unknown",
            start_time=0.0,
            end_time=60.0,  # Estimate 1 minute
            text=text_data.text,
            confidence=1.0
        )
        
        mock_transcription = TranscriptionResult(
            segments=[mock_segment],
            duration=60.0,
            processing_time=0.0,
            model_version="text-input",
            confidence=1.0
        )
        
        # Generate summary
        logger.info(f"Generating summary for text: {len(text_data.text)} characters")
        summary_result = await summarization_service.summarize_transcription(
            mock_transcription
        )
        
        # Format response
        response = {
            "success": True,
            "summary": {
                "text": summary_result.text,
                "key_points": summary_result.key_points,
                "sentiment": summary_result.sentiment,
                "confidence": summary_result.confidence,
                "processing_time": summary_result.processing_time,
                "model_version": summary_result.model_version
            },
            "input_stats": {
                "character_count": len(text_data.text),
                "word_count": len(text_data.text.split()),
                "conversation_type": text_data.conversation_type
            },
            "timestamp": time.time()
        }
        
        logger.info("Text summary generation completed")
        return response
        
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Text summarization failed: {e}")
        raise HTTPException(
            status_code=500,
            detail=f"Text summarization failed: {str(e)}"
        )


@router.post("/sentiment")
async def analyze_sentiment(
    text_data: TextInput
) -> Dict[str, Any]:
    """
    Perform detailed sentiment analysis on text.
    
    Args:
        text_data: Text to analyze
        
    Returns:
        Detailed sentiment analysis
    """
    try:
        # Validate service readiness
        if not await summarization_service.is_ready():
            raise HTTPException(
                status_code=503,
                detail="Summarization service not ready"
            )
        
        if not text_data.text.strip():
            raise HTTPException(
                status_code=400,
                detail="Text cannot be empty"
            )
        
        # Perform sentiment analysis
        logger.info(f"Analyzing sentiment for text: {len(text_data.text)} characters")
        sentiment_result = await summarization_service.analyze_sentiment_details(
            text_data.text
        )
        
        response = {
            "success": True,
            "sentiment_analysis": sentiment_result,
            "input_stats": {
                "character_count": len(text_data.text),
                "word_count": len(text_data.text.split())
            },
            "timestamp": time.time()
        }
        
        logger.info("Sentiment analysis completed")
        return response
        
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Sentiment analysis failed: {e}")
        raise HTTPException(
            status_code=500,
            detail=f"Sentiment analysis failed: {str(e)}"
        )


@router.get("/status")
async def get_summarization_status() -> Dict[str, Any]:
    """Get detailed summarization service status."""
    try:
        status = await summarization_service.get_service_status()
        return {
            "status": status,
            "timestamp": time.time()
        }
    except Exception as e:
        logger.error(f"Failed to get summarization status: {e}")
        raise HTTPException(
            status_code=500,
            detail=f"Failed to get status: {str(e)}"
        )


def _calculate_speaker_stats(segments: List[TranscriptionSegment]) -> Dict[str, Any]:
    """Calculate speaker statistics from segments."""
    if not segments:
        return {}
    
    speaker_counts = {}
    speaker_durations = {}
    speaker_words = {}
    
    for segment in segments:
        speaker = segment.speaker
        duration = segment.end_time - segment.start_time
        word_count = len(segment.text.split())
        
        speaker_counts[speaker] = speaker_counts.get(speaker, 0) + 1
        speaker_durations[speaker] = speaker_durations.get(speaker, 0) + duration
        speaker_words[speaker] = speaker_words.get(speaker, 0) + word_count
    
    return {
        "segment_counts": speaker_counts,
        "durations": speaker_durations,
        "word_counts": speaker_words
    }
