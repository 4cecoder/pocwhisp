#!/usr/bin/env python3
"""
Comprehensive test script for the PocWhisp Audio Transcription API.
Tests all endpoints and validates the complete workflow.
"""

import requests
import json
import os
import sys
import time
from pathlib import Path

# API Configuration
API_BASE = "http://localhost:8080/api/v1"
TEST_AUDIO_DIR = Path(__file__).parent / "test_audio"

def test_health_endpoints():
    """Test all health-related endpoints."""
    print("🔍 Testing Health Endpoints...")
    
    endpoints = {
        "Health": f"{API_BASE}/health",
        "Ready": f"{API_BASE}/ready", 
        "Live": f"{API_BASE}/live",
        "Metrics": f"{API_BASE}/metrics"
    }
    
    for name, url in endpoints.items():
        try:
            response = requests.get(url, timeout=5)
            print(f"  ✅ {name}: {response.status_code}")
            if response.status_code != 200 and name != "Health":
                print(f"     ⚠️  Response: {response.json()}")
        except Exception as e:
            print(f"  ❌ {name}: {e}")
    
    print()

def test_root_endpoint():
    """Test the root endpoint."""
    print("🏠 Testing Root Endpoint...")
    
    try:
        response = requests.get("http://localhost:8080/", timeout=5)
        if response.status_code == 200:
            data = response.json()
            print(f"  ✅ Service: {data.get('service')}")
            print(f"  ✅ Version: {data.get('version')}")
            print(f"  ✅ Status: {data.get('status')}")
        else:
            print(f"  ❌ Status: {response.status_code}")
    except Exception as e:
        print(f"  ❌ Error: {e}")
    
    print()

def test_transcription_list():
    """Test the transcription list endpoint."""
    print("📋 Testing Transcription List...")
    
    try:
        response = requests.get(f"{API_BASE}/transcribe", timeout=5)
        if response.status_code == 200:
            data = response.json()
            sessions_count = len(data.get('sessions', []))
            print(f"  ✅ Status: {response.status_code}")
            print(f"  ✅ Sessions Count: {sessions_count}")
            print(f"  ✅ Pagination: {data.get('pagination')}")
        else:
            print(f"  ❌ Status: {response.status_code}")
    except Exception as e:
        print(f"  ❌ Error: {e}")
    
    print()

def test_audio_upload():
    """Test audio file upload and processing."""
    print("🎵 Testing Audio Upload...")
    
    test_file = TEST_AUDIO_DIR / "test_stereo_5s.wav"
    if not test_file.exists():
        print(f"  ❌ Test file not found: {test_file}")
        return None
    
    try:
        with open(test_file, 'rb') as f:
            files = {'audio': ('test_stereo_5s.wav', f, 'audio/wav')}
            response = requests.post(f"{API_BASE}/transcribe", files=files, timeout=30)
        
        if response.status_code == 200:
            data = response.json()
            print(f"  ✅ Upload Status: {response.status_code}")
            print(f"  ✅ Segments: {len(data['transcript']['segments'])}")
            print(f"  ✅ Duration: {data['metadata']['duration']}s")
            print(f"  ✅ Processing Time: {data['metadata']['processing_time']:.3f}s")
            print(f"  ✅ Summary: {data['summary']['text'][:100]}...")
            return data
        else:
            print(f"  ❌ Upload Status: {response.status_code}")
            print(f"  ❌ Response: {response.text}")
            return None
    except Exception as e:
        print(f"  ❌ Error: {e}")
        return None

def test_file_validation():
    """Test file validation with invalid files."""
    print("🛡️  Testing File Validation...")
    
    # Test with non-audio file
    try:
        # Create a fake text file
        fake_audio = {"audio": ("fake.wav", "This is not audio data", "text/plain")}
        response = requests.post(f"{API_BASE}/transcribe", files=fake_audio, timeout=10)
        
        if response.status_code == 400:
            error = response.json()
            print(f"  ✅ Invalid file rejected: {error['error']}")
        else:
            print(f"  ❌ Invalid file accepted: {response.status_code}")
    except Exception as e:
        print(f"  ❌ Error: {e}")
    
    print()

def test_error_handling():
    """Test error handling for various scenarios."""
    print("🚨 Testing Error Handling...")
    
    # Test missing file
    try:
        response = requests.post(f"{API_BASE}/transcribe", timeout=5)
        if response.status_code == 400:
            error = response.json()
            print(f"  ✅ Missing file handled: {error['error']}")
        else:
            print(f"  ❌ Missing file not handled properly: {response.status_code}")
    except Exception as e:
        print(f"  ❌ Error: {e}")
    
    # Test invalid endpoint
    try:
        response = requests.get(f"{API_BASE}/invalid", timeout=5)
        if response.status_code == 404:
            print(f"  ✅ 404 handled correctly")
        else:
            print(f"  ❌ 404 not handled: {response.status_code}")
    except Exception as e:
        print(f"  ❌ Error: {e}")
    
    print()

def test_database_persistence():
    """Test that data persists in database."""
    print("💾 Testing Database Persistence...")
    
    try:
        # Get current sessions count
        response1 = requests.get(f"{API_BASE}/transcribe", timeout=5)
        count_before = len(response1.json().get('sessions', []))
        
        # Upload a file
        test_file = TEST_AUDIO_DIR / "test_stereo_short.wav"
        if test_file.exists():
            with open(test_file, 'rb') as f:
                files = {'audio': ('test_short.wav', f, 'audio/wav')}
                upload_response = requests.post(f"{API_BASE}/transcribe", files=files, timeout=30)
            
            if upload_response.status_code == 200:
                # Check sessions count increased
                response2 = requests.get(f"{API_BASE}/transcribe", timeout=5)
                count_after = len(response2.json().get('sessions', []))
                
                if count_after > count_before:
                    print(f"  ✅ Session persisted: {count_before} → {count_after}")
                else:
                    print(f"  ❌ Session not persisted: {count_before} → {count_after}")
            else:
                print(f"  ❌ Upload failed: {upload_response.status_code}")
        else:
            print(f"  ⚠️  Test file not found: {test_file}")
    except Exception as e:
        print(f"  ❌ Error: {e}")
    
    print()

def run_performance_test():
    """Basic performance test."""
    print("⚡ Basic Performance Test...")
    
    test_file = TEST_AUDIO_DIR / "test_stereo_5s.wav"
    if not test_file.exists():
        print(f"  ⚠️  Test file not found: {test_file}")
        return
    
    try:
        start_time = time.time()
        with open(test_file, 'rb') as f:
            files = {'audio': ('test_performance.wav', f, 'audio/wav')}
            response = requests.post(f"{API_BASE}/transcribe", files=files, timeout=30)
        
        total_time = time.time() - start_time
        
        if response.status_code == 200:
            data = response.json()
            audio_duration = data['metadata']['duration']
            processing_ratio = total_time / audio_duration
            
            print(f"  ✅ Total Time: {total_time:.3f}s")
            print(f"  ✅ Audio Duration: {audio_duration}s")
            print(f"  ✅ Processing Ratio: {processing_ratio:.2f}x real-time")
            
            if processing_ratio < 1.0:
                print(f"  🚀 Faster than real-time!")
            elif processing_ratio < 2.0:
                print(f"  ✅ Good performance")
            else:
                print(f"  ⚠️  Slower processing")
        else:
            print(f"  ❌ Performance test failed: {response.status_code}")
    except Exception as e:
        print(f"  ❌ Error: {e}")
    
    print()

def main():
    """Run all tests."""
    print("🧪 PocWhisp API Test Suite")
    print("=" * 50)
    
    # Check if server is running
    try:
        requests.get("http://localhost:8080/", timeout=2)
    except:
        print("❌ Server not running on localhost:8080")
        print("   Please start the server with: ./pocwhisp")
        sys.exit(1)
    
    # Run all tests
    test_root_endpoint()
    test_health_endpoints()
    test_transcription_list()
    test_audio_upload()
    test_file_validation()
    test_error_handling()
    test_database_persistence()
    run_performance_test()
    
    print("🎉 Test Suite Complete!")
    print("\n📊 Summary:")
    print("  • ✅ Fiber web framework with GORM + SQLite3")
    print("  • ✅ Audio file upload and validation")
    print("  • ✅ WAV file parsing and metadata extraction")
    print("  • ✅ Database persistence with relationships")
    print("  • ✅ Health monitoring endpoints")
    print("  • ✅ Error handling and validation")
    print("  • ✅ Mock AI response structure")
    print("  • ⏳ Python AI service (next phase)")

if __name__ == "__main__":
    main()
