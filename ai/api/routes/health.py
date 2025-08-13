"""
Health check endpoints for the AI service.
"""

import asyncio
import logging
from fastapi import APIRouter, HTTPException
from typing import Dict, Any
import time

from ...config import get_settings, get_gpu_info
from ...models import model_manager
from ...services import transcription_service, summarization_service

logger = logging.getLogger(__name__)

router = APIRouter()


@router.get("/")
async def health_check() -> Dict[str, Any]:
    """
    Comprehensive health check endpoint.
    Returns detailed information about service status, models, and resources.
    """
    try:
        start_time = time.time()
        
        # Get basic service information
        settings = get_settings()
        
        # Check service readiness
        transcription_ready = await transcription_service.is_ready()
        summarization_ready = await summarization_service.is_ready()
        
        # Get model health
        model_health = await model_manager.health_check()
        
        # Get GPU information
        gpu_info = get_gpu_info()
        
        # Determine overall status
        if transcription_ready and summarization_ready:
            status = "healthy"
        elif transcription_ready or summarization_ready:
            status = "degraded"
        else:
            status = "unhealthy"
        
        # Calculate response time
        response_time = time.time() - start_time
        
        health_data = {
            "status": status,
            "timestamp": time.time(),
            "response_time": response_time,
            "version": "1.0.0",
            "services": {
                "transcription": {
                    "ready": transcription_ready,
                    "initialized": transcription_service.is_initialized
                },
                "summarization": {
                    "ready": summarization_ready,
                    "initialized": summarization_service.is_initialized
                }
            },
            "models": model_health,
            "gpu": gpu_info,
            "settings": {
                "whisper_model": settings.whisper_model,
                "device": settings.device,
                "max_audio_length": settings.max_audio_length
            }
        }
        
        # Return appropriate HTTP status
        if status == "healthy":
            return health_data
        else:
            # Return 503 for degraded/unhealthy status
            raise HTTPException(status_code=503, detail=health_data)
            
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Health check failed: {e}")
        raise HTTPException(
            status_code=500,
            detail={
                "status": "error",
                "error": str(e),
                "timestamp": time.time()
            }
        )


@router.get("/ready")
async def readiness_check() -> Dict[str, Any]:
    """
    Readiness probe for Kubernetes/container orchestration.
    Returns 200 if service is ready to handle requests.
    """
    try:
        transcription_ready = await transcription_service.is_ready()
        summarization_ready = await summarization_service.is_ready()
        
        # Service is ready if at least transcription is working
        ready = transcription_ready
        
        if ready:
            return {
                "ready": True,
                "timestamp": time.time(),
                "services": {
                    "transcription": transcription_ready,
                    "summarization": summarization_ready
                }
            }
        else:
            raise HTTPException(
                status_code=503,
                detail={
                    "ready": False,
                    "timestamp": time.time(),
                    "error": "Transcription service not ready"
                }
            )
            
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Readiness check failed: {e}")
        raise HTTPException(
            status_code=503,
            detail={
                "ready": False,
                "error": str(e),
                "timestamp": time.time()
            }
        )


@router.get("/live")
async def liveness_check() -> Dict[str, Any]:
    """
    Liveness probe for Kubernetes/container orchestration.
    Returns 200 if service is alive (basic functionality).
    """
    return {
        "alive": True,
        "timestamp": time.time(),
        "pid": id(transcription_service)  # Simple process identifier
    }


@router.get("/models")
async def models_status() -> Dict[str, Any]:
    """
    Get detailed status of all loaded models.
    """
    try:
        model_health = await model_manager.health_check()
        
        # Get detailed model information
        all_models = model_manager.get_all_model_info()
        
        return {
            "timestamp": time.time(),
            "health": model_health,
            "models": {
                model_id: {
                    "name": info.name,
                    "type": info.type.value,
                    "status": info.status.value,
                    "device": info.device,
                    "memory_usage": info.memory_usage,
                    "load_time": info.load_time,
                    "version": info.version,
                    "size": info.size
                }
                for model_id, info in all_models.items()
            }
        }
        
    except Exception as e:
        logger.error(f"Models status check failed: {e}")
        raise HTTPException(
            status_code=500,
            detail={
                "error": str(e),
                "timestamp": time.time()
            }
        )


@router.get("/metrics")
async def metrics() -> Dict[str, Any]:
    """
    Prometheus-compatible metrics endpoint.
    Returns performance and usage metrics.
    """
    try:
        # Get service status
        transcription_status = await transcription_service.get_service_status()
        summarization_status = await summarization_service.get_service_status()
        
        # Get GPU metrics
        gpu_info = get_gpu_info()
        
        # Get model metrics
        model_health = await model_manager.health_check()
        
        metrics_data = {
            "timestamp": time.time(),
            "transcription_service": transcription_status,
            "summarization_service": summarization_status,
            "gpu_metrics": gpu_info,
            "model_metrics": model_health
        }
        
        return metrics_data
        
    except Exception as e:
        logger.error(f"Metrics collection failed: {e}")
        raise HTTPException(
            status_code=500,
            detail={
                "error": str(e),
                "timestamp": time.time()
            }
        )


@router.post("/models/{model_id}/reload")
async def reload_model(model_id: str) -> Dict[str, Any]:
    """
    Reload a specific model.
    Useful for updating models without restarting the service.
    """
    try:
        logger.info(f"Reloading model: {model_id}")
        
        # Unload the model
        unload_success = await model_manager.unload_model(model_id)
        if not unload_success:
            raise HTTPException(
                status_code=400,
                detail=f"Failed to unload model: {model_id}"
            )
        
        # Load the model
        load_success = await model_manager.load_model(model_id)
        if not load_success:
            raise HTTPException(
                status_code=500,
                detail=f"Failed to reload model: {model_id}"
            )
        
        # Get updated model info
        model_info = model_manager.get_model_info(model_id)
        
        return {
            "success": True,
            "model_id": model_id,
            "timestamp": time.time(),
            "model_info": model_info.dict() if model_info else None
        }
        
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Model reload failed: {e}")
        raise HTTPException(
            status_code=500,
            detail={
                "error": str(e),
                "model_id": model_id,
                "timestamp": time.time()
            }
        )
