"""
Audio processing service for channel separation and preprocessing.
Handles stereo audio separation and prepares audio for transcription.
"""

import logging
import numpy as np
import librosa
import soundfile as sf
from pathlib import Path
from typing import Tuple, List, Optional, Union, Dict, Any
import tempfile
import asyncio

logger = logging.getLogger(__name__)


class AudioProcessor:
    """Service for audio processing and channel separation."""
    
    def __init__(self):
        self.temp_dir = Path(tempfile.gettempdir()) / "pocwhisp_audio"
        self.temp_dir.mkdir(exist_ok=True)
    
    async def separate_stereo_channels(
        self, 
        audio_path: Union[str, Path]
    ) -> Tuple[np.ndarray, np.ndarray, int]:
        """
        Separate stereo audio into left and right channels.
        
        Args:
            audio_path: Path to stereo audio file
            
        Returns:
            Tuple of (left_channel, right_channel, sample_rate)
        """
        try:
            logger.info(f"Processing audio file: {audio_path}")
            
            # Load audio file with original sample rate
            audio, sr = librosa.load(audio_path, sr=None, mono=False)
            
            logger.info(f"Audio shape: {audio.shape}, Sample rate: {sr}")
            
            # Handle different audio formats
            if audio.ndim == 1:
                # Mono audio - duplicate to both channels
                left_channel = audio
                right_channel = audio
                logger.warning("Mono audio detected, duplicating to both channels")
                
            elif audio.ndim == 2:
                if audio.shape[0] == 2:
                    # Standard stereo (channels, samples)
                    left_channel = audio[0]
                    right_channel = audio[1]
                    logger.info("Stereo audio separated successfully")
                    
                elif audio.shape[1] == 2:
                    # Interleaved stereo (samples, channels)
                    left_channel = audio[:, 0]
                    right_channel = audio[:, 1]
                    logger.info("Interleaved stereo audio separated")
                    
                else:
                    # Multi-channel, take first two channels
                    left_channel = audio[0] if audio.shape[0] > 0 else np.zeros(audio.shape[1])
                    right_channel = audio[1] if audio.shape[0] > 1 else audio[0]
                    logger.warning(f"Multi-channel audio ({audio.shape[0]} channels), using first two")
                    
            else:
                raise ValueError(f"Unexpected audio shape: {audio.shape}")
            
            # Ensure channels are the same length
            min_length = min(len(left_channel), len(right_channel))
            left_channel = left_channel[:min_length]
            right_channel = right_channel[:min_length]
            
            # Normalize channels
            left_channel = self._normalize_audio(left_channel)
            right_channel = self._normalize_audio(right_channel)
            
            logger.info(f"Channel separation complete: {len(left_channel)} samples at {sr}Hz")
            
            return left_channel, right_channel, sr
            
        except Exception as e:
            logger.error(f"Error separating stereo channels: {e}")
            raise
    
    def _normalize_audio(self, audio: np.ndarray) -> np.ndarray:
        """Normalize audio to [-1, 1] range."""
        if len(audio) == 0:
            return audio
            
        # Remove DC offset
        audio = audio - np.mean(audio)
        
        # Normalize to prevent clipping
        max_val = np.max(np.abs(audio))
        if max_val > 0:
            audio = audio / max_val * 0.95  # Leave some headroom
            
        return audio.astype(np.float32)
    
    async def save_channel_to_file(
        self, 
        audio_data: np.ndarray, 
        sample_rate: int,
        channel_name: str = "channel"
    ) -> Path:
        """Save audio channel to temporary WAV file."""
        try:
            # Create temporary file
            temp_file = self.temp_dir / f"{channel_name}_{id(audio_data)}.wav"
            
            # Save as WAV file
            sf.write(str(temp_file), audio_data, sample_rate)
            
            logger.debug(f"Saved {channel_name} to {temp_file}")
            return temp_file
            
        except Exception as e:
            logger.error(f"Error saving channel to file: {e}")
            raise
    
    async def prepare_channels_for_transcription(
        self, 
        audio_path: Union[str, Path]
    ) -> Tuple[Path, Path, Dict[str, Any]]:
        """
        Prepare stereo audio for transcription by separating channels.
        
        Args:
            audio_path: Path to stereo audio file
            
        Returns:
            Tuple of (left_file_path, right_file_path, metadata)
        """
        try:
            # Separate channels
            left_channel, right_channel, sr = await self.separate_stereo_channels(audio_path)
            
            # Save channels to temporary files
            left_file = await self.save_channel_to_file(left_channel, sr, "left")
            right_file = await self.save_channel_to_file(right_channel, sr, "right")
            
            # Prepare metadata
            metadata = {
                "original_file": str(audio_path),
                "sample_rate": sr,
                "duration": len(left_channel) / sr,
                "channels": 2,
                "left_file": str(left_file),
                "right_file": str(right_file),
                "left_samples": len(left_channel),
                "right_samples": len(right_channel)
            }
            
            logger.info(f"Audio prepared for transcription: {metadata['duration']:.2f}s")
            
            return left_file, right_file, metadata
            
        except Exception as e:
            logger.error(f"Error preparing channels for transcription: {e}")
            raise
    
    async def analyze_audio_quality(
        self, 
        audio_path: Union[str, Path]
    ) -> Dict[str, Any]:
        """Analyze audio quality metrics."""
        try:
            audio, sr = librosa.load(audio_path, sr=None)
            
            # Basic quality metrics
            rms = librosa.feature.rms(y=audio)[0]
            spectral_centroids = librosa.feature.spectral_centroid(y=audio, sr=sr)[0]
            zero_crossing_rate = librosa.feature.zero_crossing_rate(audio)[0]
            
            # Signal-to-noise ratio estimation
            # (simplified approach using energy in different frequency bands)
            stft = librosa.stft(audio)
            magnitude = np.abs(stft)
            
            # Low frequency energy (potential noise)
            low_freq_energy = np.mean(magnitude[:10])
            # Mid frequency energy (speech)
            mid_freq_energy = np.mean(magnitude[10:100])
            
            snr_estimate = 20 * np.log10(mid_freq_energy / (low_freq_energy + 1e-10))
            
            analysis = {
                "duration": len(audio) / sr,
                "sample_rate": sr,
                "rms_mean": float(np.mean(rms)),
                "rms_std": float(np.std(rms)),
                "spectral_centroid_mean": float(np.mean(spectral_centroids)),
                "zero_crossing_rate_mean": float(np.mean(zero_crossing_rate)),
                "snr_estimate": float(snr_estimate),
                "quality_score": self._calculate_quality_score(
                    np.mean(rms), 
                    snr_estimate, 
                    np.mean(zero_crossing_rate)
                )
            }
            
            logger.debug(f"Audio quality analysis: {analysis}")
            return analysis
            
        except Exception as e:
            logger.error(f"Error analyzing audio quality: {e}")
            return {"error": str(e)}
    
    def _calculate_quality_score(
        self, 
        rms: float, 
        snr: float, 
        zcr: float
    ) -> float:
        """Calculate a simple quality score (0-1)."""
        # Normalize components
        rms_score = min(rms * 10, 1.0)  # RMS volume
        snr_score = max(min((snr + 10) / 40, 1.0), 0.0)  # SNR (-10 to 30 dB)
        zcr_score = 1.0 - min(zcr * 2, 1.0)  # Lower ZCR is better for speech
        
        # Weighted average
        quality = (rms_score * 0.3 + snr_score * 0.5 + zcr_score * 0.2)
        
        return float(quality)
    
    async def cleanup_temp_files(self, older_than_hours: int = 1) -> int:
        """Clean up temporary audio files older than specified hours."""
        import time
        
        cleaned_count = 0
        current_time = time.time()
        cutoff_time = current_time - (older_than_hours * 3600)
        
        try:
            for file_path in self.temp_dir.glob("*.wav"):
                if file_path.stat().st_mtime < cutoff_time:
                    file_path.unlink()
                    cleaned_count += 1
                    
            logger.info(f"Cleaned up {cleaned_count} temporary audio files")
            return cleaned_count
            
        except Exception as e:
            logger.error(f"Error cleaning up temp files: {e}")
            return 0
    
    async def resample_audio(
        self, 
        audio_data: np.ndarray, 
        orig_sr: int, 
        target_sr: int = 16000
    ) -> np.ndarray:
        """Resample audio to target sample rate."""
        if orig_sr == target_sr:
            return audio_data
            
        try:
            resampled = librosa.resample(
                audio_data, 
                orig_sr=orig_sr, 
                target_sr=target_sr,
                res_type='kaiser_best'
            )
            
            logger.debug(f"Resampled audio from {orig_sr}Hz to {target_sr}Hz")
            return resampled
            
        except Exception as e:
            logger.error(f"Error resampling audio: {e}")
            raise
    
    async def detect_voice_activity(
        self, 
        audio_data: np.ndarray, 
        sample_rate: int,
        threshold: float = 0.01
    ) -> List[Tuple[float, float]]:
        """
        Detect voice activity periods in audio.
        
        Returns:
            List of (start_time, end_time) tuples for voice segments
        """
        try:
            # Calculate RMS energy in overlapping windows
            frame_length = int(0.025 * sample_rate)  # 25ms frames
            hop_length = int(0.01 * sample_rate)     # 10ms hop
            
            rms = librosa.feature.rms(
                y=audio_data,
                frame_length=frame_length,
                hop_length=hop_length
            )[0]
            
            # Apply threshold
            voice_frames = rms > threshold
            
            # Convert frame indices to time
            times = librosa.frames_to_time(
                np.arange(len(voice_frames)),
                sr=sample_rate,
                hop_length=hop_length
            )
            
            # Find continuous voice segments
            segments = []
            start_time = None
            
            for i, is_voice in enumerate(voice_frames):
                if is_voice and start_time is None:
                    start_time = times[i]
                elif not is_voice and start_time is not None:
                    segments.append((start_time, times[i]))
                    start_time = None
            
            # Handle case where voice continues to end
            if start_time is not None:
                segments.append((start_time, times[-1]))
            
            logger.debug(f"Detected {len(segments)} voice segments")
            return segments
            
        except Exception as e:
            logger.error(f"Error detecting voice activity: {e}")
            return []


# Global audio processor instance
audio_processor = AudioProcessor()
