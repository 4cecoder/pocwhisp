#!/usr/bin/env python3
"""
Create a simple test stereo WAV file for testing the audio upload endpoint.
This script generates a 5-second stereo WAV file with different tones on left/right channels.
"""

import numpy as np
import wave
import struct
import os

def create_test_stereo_wav(filename, duration=5, sample_rate=44100):
    """
    Create a test stereo WAV file with different frequencies on left/right channels.
    
    Args:
        filename: Output WAV file path
        duration: Duration in seconds
        sample_rate: Sample rate in Hz
    """
    # Generate time array
    t = np.linspace(0, duration, int(sample_rate * duration), False)
    
    # Generate left channel (440 Hz - A4 note)
    left_channel = np.sin(2 * np.pi * 440 * t) * 0.3
    
    # Generate right channel (880 Hz - A5 note)
    right_channel = np.sin(2 * np.pi * 880 * t) * 0.3
    
    # Create stereo data by interleaving left and right samples
    stereo_data = []
    for i in range(len(left_channel)):
        # Convert to 16-bit signed integers
        left_sample = int(left_channel[i] * 32767)
        right_sample = int(right_channel[i] * 32767)
        
        # Clamp to 16-bit range
        left_sample = max(-32768, min(32767, left_sample))
        right_sample = max(-32768, min(32767, right_sample))
        
        stereo_data.extend([left_sample, right_sample])
    
    # Write WAV file
    with wave.open(filename, 'wb') as wav_file:
        # Set WAV file parameters
        wav_file.setnchannels(2)      # Stereo
        wav_file.setsampwidth(2)      # 16-bit
        wav_file.setframerate(sample_rate)
        wav_file.setnframes(len(left_channel))
        
        # Pack data as signed 16-bit integers
        packed_data = struct.pack('<' + 'h' * len(stereo_data), *stereo_data)
        wav_file.writeframes(packed_data)
    
    print(f"Created test WAV file: {filename}")
    print(f"Duration: {duration} seconds")
    print(f"Sample rate: {sample_rate} Hz")
    print(f"Channels: 2 (stereo)")
    print(f"File size: {os.path.getsize(filename)} bytes")

if __name__ == "__main__":
    # Create test directory if it doesn't exist
    test_dir = os.path.dirname(os.path.abspath(__file__))
    test_audio_dir = os.path.join(test_dir, "test_audio")
    os.makedirs(test_audio_dir, exist_ok=True)
    
    # Create test WAV files
    test_files = [
        ("test_stereo_5s.wav", 5),
        ("test_stereo_short.wav", 2),
        ("test_stereo_long.wav", 30)
    ]
    
    for filename, duration in test_files:
        filepath = os.path.join(test_audio_dir, filename)
        create_test_stereo_wav(filepath, duration)
        print("---")
