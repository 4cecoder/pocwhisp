"""
Configuration management for the AI service.
Handles environment variables, model settings, and service configuration.
"""

import os
import torch
from pathlib import Path
from typing import Optional, Dict, Any
try:
    from pydantic_settings import BaseSettings
    from pydantic import Field
except ImportError:
    from pydantic import BaseSettings, Field


class Settings(BaseSettings):
    """Application settings with environment variable support."""
    
    # Service configuration
    host: str = Field(default="0.0.0.0", env="AI_HOST")
    port: int = Field(default=8081, env="AI_PORT")
    workers: int = Field(default=1, env="AI_WORKERS")
    
    # Model paths and settings
    models_dir: str = Field(default="./models", env="MODELS_DIR")
    whisper_model: str = Field(default="large-v3", env="WHISPER_MODEL")
    llama_model_path: str = Field(default="", env="LLAMA_MODEL_PATH")
    
    # GPU and device settings
    device: str = Field(default="auto", env="DEVICE")
    gpu_memory_fraction: float = Field(default=0.8, env="GPU_MEMORY_FRACTION")
    torch_dtype: str = Field(default="float16", env="TORCH_DTYPE")
    
    # Processing settings
    max_audio_length: int = Field(default=1800, env="MAX_AUDIO_LENGTH")  # 30 minutes
    chunk_length: int = Field(default=30, env="CHUNK_LENGTH")  # seconds
    batch_size: int = Field(default=1, env="BATCH_SIZE")
    
    # Whisper settings
    whisper_temperature: float = Field(default=0.0, env="WHISPER_TEMPERATURE")
    whisper_best_of: int = Field(default=1, env="WHISPER_BEST_OF")
    whisper_beam_size: int = Field(default=1, env="WHISPER_BEAM_SIZE")
    
    # Llama settings
    llama_max_tokens: int = Field(default=512, env="LLAMA_MAX_TOKENS")
    llama_temperature: float = Field(default=0.1, env="LLAMA_TEMPERATURE")
    llama_top_p: float = Field(default=0.9, env="LLAMA_TOP_P")
    
    # API settings
    cors_origins: list = Field(default=["*"], env="CORS_ORIGINS")
    api_key: Optional[str] = Field(default=None, env="AI_API_KEY")
    
    # Logging
    log_level: str = Field(default="INFO", env="LOG_LEVEL")
    log_format: str = Field(default="json", env="LOG_FORMAT")
    
    class Config:
        env_file = ".env"
        case_sensitive = False


def get_device() -> torch.device:
    """Determine the best available device for model inference."""
    settings = get_settings()
    
    if settings.device == "auto":
        if torch.cuda.is_available():
            device = torch.device("cuda")
            print(f"Using GPU: {torch.cuda.get_device_name()}")
            print(f"GPU Memory: {torch.cuda.get_device_properties(0).total_memory / 1e9:.1f} GB")
        elif hasattr(torch.backends, 'mps') and torch.backends.mps.is_available():
            device = torch.device("mps")
            print("Using Apple Metal Performance Shaders (MPS)")
        else:
            device = torch.device("cpu")
            print("Using CPU")
    else:
        device = torch.device(settings.device)
        print(f"Using specified device: {device}")
    
    return device


def get_torch_dtype() -> torch.dtype:
    """Get the appropriate torch dtype for model inference."""
    settings = get_settings()
    dtype_map = {
        "float32": torch.float32,
        "float16": torch.float16,
        "bfloat16": torch.bfloat16,
    }
    return dtype_map.get(settings.torch_dtype, torch.float16)


def get_model_cache_dir() -> Path:
    """Get the model cache directory, creating it if necessary."""
    settings = get_settings()
    cache_dir = Path(settings.models_dir)
    cache_dir.mkdir(parents=True, exist_ok=True)
    return cache_dir


def get_gpu_info() -> Dict[str, Any]:
    """Get information about available GPU resources."""
    gpu_info = {
        "cuda_available": torch.cuda.is_available(),
        "mps_available": hasattr(torch.backends, 'mps') and torch.backends.mps.is_available(),
        "device_count": 0,
        "current_device": None,
        "memory_info": {},
    }
    
    if torch.cuda.is_available():
        gpu_info["device_count"] = torch.cuda.device_count()
        gpu_info["current_device"] = torch.cuda.current_device()
        
        for i in range(torch.cuda.device_count()):
            props = torch.cuda.get_device_properties(i)
            memory = torch.cuda.get_device_properties(i).total_memory
            gpu_info["memory_info"][f"gpu_{i}"] = {
                "name": props.name,
                "total_memory": memory,
                "total_memory_gb": memory / 1e9,
            }
    
    return gpu_info


def validate_models() -> Dict[str, bool]:
    """Validate that required models are available or can be downloaded."""
    validation = {
        "whisper_available": False,
        "llama_available": False,
        "models_dir_exists": False,
    }
    
    settings = get_settings()
    models_dir = get_model_cache_dir()
    validation["models_dir_exists"] = models_dir.exists()
    
    # Check Whisper model availability
    try:
        import whisper
        available_models = whisper.available_models()
        validation["whisper_available"] = settings.whisper_model in available_models
    except ImportError:
        validation["whisper_available"] = False
    
    # Check Llama model availability
    if settings.llama_model_path:
        llama_path = Path(settings.llama_model_path)
        validation["llama_available"] = llama_path.exists()
    else:
        validation["llama_available"] = False
    
    return validation


# Global settings instance
_settings: Optional[Settings] = None


def get_settings() -> Settings:
    """Get the global settings instance (singleton pattern)."""
    global _settings
    if _settings is None:
        _settings = Settings()
    return _settings


def reload_settings() -> Settings:
    """Reload settings from environment (useful for testing)."""
    global _settings
    _settings = Settings()
    return _settings


# Configuration for different environments
ENV_CONFIGS = {
    "development": {
        "log_level": "DEBUG",
        "workers": 1,
        "whisper_model": "base",  # Smaller model for dev
        "gpu_memory_fraction": 0.5,
    },
    "production": {
        "log_level": "INFO",
        "workers": 2,
        "whisper_model": "large-v3",
        "gpu_memory_fraction": 0.8,
    },
    "testing": {
        "log_level": "WARNING",
        "workers": 1,
        "whisper_model": "tiny",  # Fastest for tests
        "device": "cpu",
    },
}


def apply_env_config(env: str) -> None:
    """Apply environment-specific configuration."""
    if env in ENV_CONFIGS:
        config = ENV_CONFIGS[env]
        for key, value in config.items():
            os.environ[key.upper()] = str(value)
        reload_settings()
        print(f"Applied {env} configuration")
    else:
        print(f"Unknown environment: {env}")


if __name__ == "__main__":
    # Test configuration
    settings = get_settings()
    print("=== AI Service Configuration ===")
    print(f"Host: {settings.host}:{settings.port}")
    print(f"Device: {get_device()}")
    print(f"Models Dir: {get_model_cache_dir()}")
    print(f"Whisper Model: {settings.whisper_model}")
    print("\n=== GPU Info ===")
    gpu_info = get_gpu_info()
    for key, value in gpu_info.items():
        print(f"{key}: {value}")
    print("\n=== Model Validation ===")
    validation = validate_models()
    for key, value in validation.items():
        print(f"{key}: {'✅' if value else '❌'}")
