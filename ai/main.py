"""
Main FastAPI application for the AI service.
Provides endpoints for audio transcription and text summarization.
"""

import asyncio
import logging
import uvicorn
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Dict, Any, List

from fastapi import FastAPI, HTTPException, UploadFile, File, BackgroundTasks
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse
import structlog

from .config import get_settings, get_gpu_info, validate_models
from .models import model_manager
from .services import transcription_service, summarization_service
from .api.routes import health, streaming

# Configure structured logging
structlog.configure(
    processors=[
        structlog.stdlib.filter_by_level,
        structlog.stdlib.add_logger_name,
        structlog.stdlib.add_log_level,
        structlog.stdlib.PositionalArgumentsFormatter(),
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.processors.StackInfoRenderer(),
        structlog.processors.format_exc_info,
        structlog.processors.UnicodeDecoder(),
        structlog.processors.JSONRenderer()
    ],
    context_class=dict,
    logger_factory=structlog.stdlib.LoggerFactory(),
    wrapper_class=structlog.stdlib.BoundLogger,
    cache_logger_on_first_use=True,
)

logger = structlog.get_logger(__name__)


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage application startup and shutdown."""
    # Startup
    logger.info("Starting AI service...")
    
    settings = get_settings()
    
    # Log system information
    gpu_info = get_gpu_info()
    logger.info("System information", gpu_info=gpu_info)
    
    # Validate models
    model_validation = validate_models()
    logger.info("Model validation", validation=model_validation)
    
    # Initialize services
    try:
        # Initialize transcription service
        transcription_success = await transcription_service.initialize()
        logger.info("Transcription service initialized", success=transcription_success)
        
        # Initialize summarization service (use mock for now)
        summarization_success = await summarization_service.initialize(use_mock=True)
        logger.info("Summarization service initialized", success=summarization_success)
        
        if not transcription_success:
            logger.error("Failed to initialize transcription service")
        
        if not summarization_success:
            logger.error("Failed to initialize summarization service")
        
        logger.info("AI service startup complete")
        
    except Exception as e:
        logger.error("Failed to initialize services", error=str(e))
        raise
    
    yield
    
    # Shutdown
    logger.info("Shutting down AI service...")
    
    try:
        await transcription_service.shutdown()
        await summarization_service.shutdown()
        logger.info("AI service shutdown complete")
    except Exception as e:
        logger.error("Error during shutdown", error=str(e))


def create_app() -> FastAPI:
    """Create and configure the FastAPI application."""
    settings = get_settings()
    
    app = FastAPI(
        title="PocWhisp AI Service",
        description="Audio transcription and summarization service using Whisper and Llama",
        version="1.0.0",
        lifespan=lifespan,
        docs_url="/docs",
        redoc_url="/redoc"
    )
    
    # Configure CORS
    app.add_middleware(
        CORSMiddleware,
        allow_origins=settings.cors_origins,
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )
    
    # Include routers
    app.include_router(health.router, prefix="/health", tags=["health"])
    app.include_router(streaming.router, prefix="/stream", tags=["streaming"])
    
    # Import and include other routers
    from .api.routes import transcription, summarization
    app.include_router(transcription.router, prefix="/transcribe", tags=["transcription"])
    app.include_router(summarization.router, prefix="/summarize", tags=["summarization"])
    
    # Root endpoint
    @app.get("/")
    async def root():
        """Service information endpoint."""
        return {
            "service": "PocWhisp AI Service",
            "version": "1.0.0",
            "status": "running",
            "endpoints": {
                "docs": "/docs",
                "health": "/health",
                "transcribe": "/transcribe",
                "summarize": "/summarize"
            }
        }
    
    # Global exception handler
    @app.exception_handler(Exception)
    async def global_exception_handler(request, exc):
        logger.error("Unhandled exception", 
                    path=request.url.path, 
                    method=request.method,
                    error=str(exc))
        return JSONResponse(
            status_code=500,
            content={
                "error": "internal_server_error",
                "message": "An internal error occurred",
                "detail": str(exc) if settings.log_level == "DEBUG" else "Contact support"
            }
        )
    
    return app


# Create the app instance
app = create_app()


if __name__ == "__main__":
    settings = get_settings()
    
    # Configure logging
    log_config = {
        "version": 1,
        "disable_existing_loggers": False,
        "formatters": {
            "default": {
                "format": "%(asctime)s - %(name)s - %(levelname)s - %(message)s",
            },
        },
        "handlers": {
            "default": {
                "formatter": "default",
                "class": "logging.StreamHandler",
                "stream": "ext://sys.stdout",
            },
        },
        "root": {
            "level": settings.log_level,
            "handlers": ["default"],
        },
    }
    
    # Run the server
    uvicorn.run(
        "main:app",
        host=settings.host,
        port=settings.port,
        workers=settings.workers,
        log_config=log_config,
        reload=False,  # Set to True for development
        access_log=True
    )
