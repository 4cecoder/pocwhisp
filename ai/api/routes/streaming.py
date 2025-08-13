from fastapi import APIRouter, WebSocket, WebSocketDisconnect, HTTPException, Depends
from fastapi.responses import JSONResponse
import json
import logging
import asyncio
from typing import Dict, List, Optional
import time

from ...services.streaming_service import get_streaming_service, StreamingSession
from ...config import get_settings

logger = logging.getLogger(__name__)
router = APIRouter(prefix="/stream", tags=["streaming"])

# Global state for active WebSocket connections
active_connections: Dict[str, WebSocket] = {}
active_sessions: Dict[str, StreamingSession] = {}

@router.websocket("/transcribe")
async def websocket_transcribe(websocket: WebSocket):
    """WebSocket endpoint for real-time transcription."""
    await websocket.accept()
    
    stream_id = None
    session = None
    
    try:
        # Wait for initial configuration
        config_data = await websocket.receive_text()
        config = json.loads(config_data)
        
        # Validate configuration
        if config.get("type") != "start_stream":
            await websocket.send_text(json.dumps({
                "type": "error",
                "error": "Expected 'start_stream' message"
            }))
            return
        
        # Extract stream configuration
        stream_config = config.get("config", {})
        stream_id = f"stream_{int(time.time() * 1000)}"
        
        # Start streaming session
        streaming_service = get_streaming_service()
        session = await streaming_service.start_stream(stream_id, stream_config)
        
        # Store connection
        active_connections[stream_id] = websocket
        active_sessions[stream_id] = session
        
        # Send confirmation
        await websocket.send_text(json.dumps({
            "type": "stream_started",
            "stream_id": stream_id,
            "config": stream_config
        }))
        
        # Start result streaming task
        result_task = asyncio.create_task(stream_results(websocket, session))
        
        # Process incoming audio data
        while True:
            try:
                # Receive data (binary for audio, text for control messages)
                data = await websocket.receive()
                
                if "bytes" in data:
                    # Audio data
                    audio_bytes = data["bytes"]
                    await session.add_audio_data(audio_bytes)
                    
                elif "text" in data:
                    # Control message
                    message = json.loads(data["text"])
                    
                    if message.get("type") == "stop_stream":
                        break
                    elif message.get("type") == "get_stats":
                        stats = session.get_statistics()
                        await websocket.send_text(json.dumps({
                            "type": "statistics",
                            "data": stats
                        }))
                    
            except WebSocketDisconnect:
                break
            except Exception as e:
                logger.error(f"Error processing WebSocket data: {e}")
                await websocket.send_text(json.dumps({
                    "type": "error",
                    "error": str(e)
                }))
        
        # Cancel result streaming
        result_task.cancel()
        
        # Finalize session
        if session:
            final_result = await session.finalize()
            await websocket.send_text(json.dumps({
                "type": "stream_ended",
                "final_result": {
                    "segments": [
                        {
                            "speaker": seg.speaker,
                            "start_time": seg.start_time,
                            "end_time": seg.end_time,
                            "text": seg.text,
                            "confidence": seg.confidence
                        }
                        for seg in final_result.segments
                    ],
                    "duration": final_result.duration,
                    "processing_time": final_result.processing_time
                }
            }))
        
    except WebSocketDisconnect:
        logger.info(f"WebSocket disconnected: {stream_id}")
    except Exception as e:
        logger.error(f"WebSocket error: {e}")
        if websocket.client_state.name != "DISCONNECTED":
            await websocket.send_text(json.dumps({
                "type": "error",
                "error": str(e)
            }))
    
    finally:
        # Cleanup
        if stream_id:
            active_connections.pop(stream_id, None)
            active_sessions.pop(stream_id, None)
            
            if session:
                streaming_service = get_streaming_service()
                await streaming_service.stop_stream(stream_id)

async def stream_results(websocket: WebSocket, session: StreamingSession):
    """Stream transcription results to WebSocket client."""
    try:
        async for result in session.get_results():
            await websocket.send_text(json.dumps(result))
    except asyncio.CancelledError:
        pass
    except Exception as e:
        logger.error(f"Error streaming results: {e}")

@router.get("/sessions")
async def get_active_sessions():
    """Get list of active streaming sessions."""
    streaming_service = get_streaming_service()
    active_streams = streaming_service.get_active_streams()
    
    sessions_info = []
    for stream_id in active_streams:
        if stream_id in active_sessions:
            session = active_sessions[stream_id]
            sessions_info.append(session.get_statistics())
    
    return {
        "active_sessions": len(active_streams),
        "sessions": sessions_info
    }

@router.get("/sessions/{stream_id}/stats")
async def get_session_stats(stream_id: str):
    """Get statistics for a specific streaming session."""
    if stream_id not in active_sessions:
        raise HTTPException(status_code=404, detail="Stream not found")
    
    session = active_sessions[stream_id]
    return session.get_statistics()

@router.post("/sessions/{stream_id}/stop")
async def stop_session(stream_id: str):
    """Stop a specific streaming session."""
    if stream_id not in active_sessions:
        raise HTTPException(status_code=404, detail="Stream not found")
    
    streaming_service = get_streaming_service()
    final_result = await streaming_service.stop_stream(stream_id)
    
    # Cleanup
    active_connections.pop(stream_id, None)
    active_sessions.pop(stream_id, None)
    
    if final_result:
        return {
            "message": "Stream stopped successfully",
            "final_result": {
                "segments": [
                    {
                        "speaker": seg.speaker,
                        "start_time": seg.start_time,
                        "end_time": seg.end_time,
                        "text": seg.text,
                        "confidence": seg.confidence
                    }
                    for seg in final_result.segments
                ],
                "duration": final_result.duration,
                "processing_time": final_result.processing_time
            }
        }
    else:
        return {"message": "Stream stopped (no final result)"}

@router.get("/health")
async def streaming_health():
    """Check streaming service health."""
    streaming_service = get_streaming_service()
    
    try:
        # Initialize models if not already done
        await streaming_service.initialize_models()
        
        return {
            "status": "healthy",
            "device": streaming_service.device,
            "active_streams": len(streaming_service.get_active_streams()),
            "whisper_model_loaded": streaming_service.whisper_model is not None,
            "vad_model_loaded": streaming_service.vad_model is not None
        }
    except Exception as e:
        logger.error(f"Streaming health check failed: {e}")
        return JSONResponse(
            status_code=503,
            content={
                "status": "unhealthy",
                "error": str(e)
            }
        )

@router.post("/test")
async def test_streaming():
    """Test endpoint for streaming functionality."""
    try:
        streaming_service = get_streaming_service()
        await streaming_service.initialize_models()
        
        # Create test audio data
        import numpy as np
        sample_rate = 16000
        duration = 2  # 2 seconds
        samples = int(sample_rate * duration)
        
        # Generate test tone
        t = np.linspace(0, duration, samples, False)
        test_audio = np.sin(2 * np.pi * 440 * t).astype(np.float32)
        
        # Test transcription
        segments = await streaming_service.transcribe_chunk(test_audio)
        
        return {
            "status": "success",
            "test_audio_duration": duration,
            "segments_detected": len(segments),
            "segments": [
                {
                    "text": seg.text,
                    "start_time": seg.start_time,
                    "end_time": seg.end_time,
                    "confidence": seg.confidence,
                    "speaker": seg.speaker
                }
                for seg in segments
            ]
        }
        
    except Exception as e:
        logger.error(f"Streaming test failed: {e}")
        return JSONResponse(
            status_code=500,
            content={
                "status": "error",
                "error": str(e)
            }
        )
