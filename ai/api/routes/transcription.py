"""
Transcription API endpoints.
"""

import logging
import tempfile
import aiofiles
from pathlib import Path
from fastapi import APIRouter, HTTPException, UploadFile, File, BackgroundTasks
from typing import Dict, Any, List, Optional
import time

from ...services import transcription_service
from ...config import get_settings

logger = logging.getLogger(__name__)

router = APIRouter()


@router.post("/")
async def transcribe_audio(
    audio: UploadFile = File(...),
    language: Optional[str] = None,
    temperature: Optional[float] = None
) -> Dict[str, Any]:
    """
    Transcribe uploaded audio file with speaker separation.
    
    Args:
        audio: Uploaded audio file (WAV, MP3, etc.)
        language: Optional language code for transcription
        temperature: Optional temperature for Whisper model
        
    Returns:
        Transcription result with speaker-labeled segments
    """
    try:
        # Validate service readiness
        if not await transcription_service.is_ready():
            raise HTTPException(
                status_code=503,
                detail="Transcription service not ready"
            )
        
        # Validate file
        if not audio.filename:
            raise HTTPException(
                status_code=400,
                detail="No filename provided"
            )
        
        # Check file format
        supported_formats = await transcription_service.get_supported_formats()
        file_ext = Path(audio.filename).suffix.lower().lstrip('.')
        
        if file_ext not in supported_formats:
            raise HTTPException(
                status_code=400,
                detail=f"Unsupported format: {file_ext}. Supported: {supported_formats}"
            )
        
        # Save uploaded file temporarily
        with tempfile.NamedTemporaryFile(
            delete=False,
            suffix=f".{file_ext}",
            prefix="transcribe_"
        ) as temp_file:
            temp_path = Path(temp_file.name)
            
            # Write uploaded content to temp file
            content = await audio.read()
            await aiofiles.open(temp_path, 'wb').write(content)
        
        try:
            # Validate audio file
            validation = await transcription_service.validate_audio_file(temp_path)
            if not validation.get("valid", False):
                raise HTTPException(
                    status_code=400,
                    detail=f"Invalid audio file: {validation.get('error', 'Unknown error')}"
                )
            
            # Prepare transcription options
            transcription_options = {}
            if language:
                transcription_options["language"] = language
            if temperature is not None:
                transcription_options["temperature"] = temperature
            
            # Perform transcription
            logger.info(f"Starting transcription of {audio.filename}")
            result = await transcription_service.transcribe_audio(
                temp_path,
                **transcription_options
            )
            
            # Format response
            response = {
                "success": True,
                "filename": audio.filename,
                "transcription": {
                    "segments": [
                        {
                            "speaker": seg.speaker,
                            "start_time": seg.start_time,
                            "end_time": seg.end_time,
                            "text": seg.text,
                            "confidence": seg.confidence,
                            "language": seg.language
                        }
                        for seg in result.segments
                    ],
                    "language": result.language,
                    "duration": result.duration,
                    "processing_time": result.processing_time,
                    "model_version": result.model_version,
                    "confidence": result.confidence
                },
                "validation": validation,
                "timestamp": time.time()
            }
            
            logger.info(f"Transcription completed: {len(result.segments)} segments")
            return response
            
        finally:
            # Clean up temporary file
            try:
                temp_path.unlink()
            except Exception as e:
                logger.warning(f"Failed to clean up temp file {temp_path}: {e}")
    
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Transcription failed: {e}")
        raise HTTPException(
            status_code=500,
            detail=f"Transcription failed: {str(e)}"
        )


@router.post("/batch")
async def transcribe_batch(
    audio_files: List[UploadFile] = File(...),
    language: Optional[str] = None
) -> Dict[str, Any]:
    """
    Transcribe multiple audio files in batch.
    
    Args:
        audio_files: List of uploaded audio files
        language: Optional language code for transcription
        
    Returns:
        Batch transcription results
    """
    try:
        # Validate service readiness
        if not await transcription_service.is_ready():
            raise HTTPException(
                status_code=503,
                detail="Transcription service not ready"
            )
        
        if len(audio_files) > 10:  # Limit batch size
            raise HTTPException(
                status_code=400,
                detail="Batch size limited to 10 files"
            )
        
        # Process each file
        results = []
        temp_files = []
        
        try:
            # Save all files first
            for audio in audio_files:
                if not audio.filename:
                    results.append({
                        "filename": "unknown",
                        "success": False,
                        "error": "No filename provided"
                    })
                    continue
                
                file_ext = Path(audio.filename).suffix.lower().lstrip('.')
                
                with tempfile.NamedTemporaryFile(
                    delete=False,
                    suffix=f".{file_ext}",
                    prefix="batch_"
                ) as temp_file:
                    temp_path = Path(temp_file.name)
                    temp_files.append(temp_path)
                    
                    content = await audio.read()
                    await aiofiles.open(temp_path, 'wb').write(content)
            
            # Transcribe using batch service
            valid_files = [f for f in temp_files if f.exists()]
            
            if valid_files:
                transcription_options = {}
                if language:
                    transcription_options["language"] = language
                
                batch_results = await transcription_service.transcribe_batch(
                    valid_files,
                    **transcription_options
                )
                
                # Format results
                for i, result in enumerate(batch_results):
                    filename = audio_files[i].filename if i < len(audio_files) else "unknown"
                    
                    if result.segments:  # Successful transcription
                        results.append({
                            "filename": filename,
                            "success": True,
                            "transcription": {
                                "segments": [
                                    {
                                        "speaker": seg.speaker,
                                        "start_time": seg.start_time,
                                        "end_time": seg.end_time,
                                        "text": seg.text,
                                        "confidence": seg.confidence
                                    }
                                    for seg in result.segments
                                ],
                                "duration": result.duration,
                                "processing_time": result.processing_time,
                                "confidence": result.confidence
                            }
                        })
                    else:  # Failed transcription
                        results.append({
                            "filename": filename,
                            "success": False,
                            "error": "Transcription failed"
                        })
            
            return {
                "success": True,
                "total_files": len(audio_files),
                "processed_files": len(results),
                "results": results,
                "timestamp": time.time()
            }
            
        finally:
            # Clean up all temporary files
            for temp_file in temp_files:
                try:
                    temp_file.unlink()
                except Exception as e:
                    logger.warning(f"Failed to clean up temp file {temp_file}: {e}")
    
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Batch transcription failed: {e}")
        raise HTTPException(
            status_code=500,
            detail=f"Batch transcription failed: {str(e)}"
        )


@router.get("/formats")
async def get_supported_formats() -> Dict[str, Any]:
    """Get list of supported audio formats."""
    try:
        formats = await transcription_service.get_supported_formats()
        return {
            "supported_formats": formats,
            "timestamp": time.time()
        }
    except Exception as e:
        logger.error(f"Failed to get supported formats: {e}")
        raise HTTPException(
            status_code=500,
            detail=f"Failed to get supported formats: {str(e)}"
        )


@router.get("/status")
async def get_transcription_status() -> Dict[str, Any]:
    """Get detailed transcription service status."""
    try:
        status = await transcription_service.get_service_status()
        return {
            "status": status,
            "timestamp": time.time()
        }
    except Exception as e:
        logger.error(f"Failed to get transcription status: {e}")
        raise HTTPException(
            status_code=500,
            detail=f"Failed to get status: {str(e)}"
        )
