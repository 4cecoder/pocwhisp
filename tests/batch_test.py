#!/usr/bin/env python3
"""
Batch Processing Test Script

This script tests the batch processing functionality of the PocWhisp API.
It creates test audio files, submits batch jobs, monitors progress, and validates results.
"""

import asyncio
import json
import os
import requests
import time
import wave
import numpy as np
from datetime import datetime
from typing import Dict, List, Optional

# Configuration
API_BASE_URL = "http://localhost:8080/api/v1"
TEST_FILES_DIR = "/tmp/batch_test_files"
SAMPLE_RATE = 44100
DURATION = 5  # seconds per file
NUM_TEST_FILES = 10

class BatchProcessingTester:
    def __init__(self, base_url: str = API_BASE_URL):
        self.base_url = base_url
        self.session = requests.Session()
        self.test_files = []
        
    def create_test_files(self, num_files: int = NUM_TEST_FILES) -> List[str]:
        """Create test WAV files for batch processing"""
        print(f"Creating {num_files} test audio files...")
        
        # Ensure test directory exists
        os.makedirs(TEST_FILES_DIR, exist_ok=True)
        
        file_paths = []
        
        for i in range(num_files):
            # Generate different frequency tones for each file
            freq = 440 + (i * 50)  # A4 to higher notes
            
            # Generate stereo audio data
            t = np.linspace(0, DURATION, int(SAMPLE_RATE * DURATION), False)
            
            # Left channel: sine wave
            left_channel = np.sin(2 * np.pi * freq * t) * 0.5
            
            # Right channel: different frequency
            right_freq = freq * 1.2
            right_channel = np.sin(2 * np.pi * right_freq * t) * 0.5
            
            # Combine channels
            stereo_audio = np.column_stack((left_channel, right_channel))
            
            # Convert to 16-bit integers
            audio_data = (stereo_audio * 32767).astype(np.int16)
            
            # Save as WAV file
            filename = f"test_batch_{i+1:03d}_{freq}hz.wav"
            filepath = os.path.join(TEST_FILES_DIR, filename)
            
            with wave.open(filepath, 'w') as wav_file:
                wav_file.setnchannels(2)  # Stereo
                wav_file.setsampwidth(2)  # 16-bit
                wav_file.setframerate(SAMPLE_RATE)
                wav_file.writeframes(audio_data.tobytes())
            
            file_paths.append(filepath)
            
        print(f"Created {len(file_paths)} test files in {TEST_FILES_DIR}")
        self.test_files = file_paths
        return file_paths
    
    def test_health_check(self) -> bool:
        """Test API health before running batch tests"""
        print("Checking API health...")
        
        try:
            response = self.session.get(f"{self.base_url}/health", timeout=10)
            if response.status_code == 200:
                health_data = response.json()
                print(f"✅ API is healthy: {health_data.get('status', 'unknown')}")
                return True
            else:
                print(f"❌ API health check failed: {response.status_code}")
                return False
        except Exception as e:
            print(f"❌ API health check error: {e}")
            return False
    
    def create_batch_job(self, 
                        name: str,
                        job_type: str = "full_processing",
                        priority: int = 5,
                        file_paths: Optional[List[str]] = None) -> Optional[Dict]:
        """Create a new batch job"""
        if file_paths is None:
            file_paths = self.test_files
        
        if not file_paths:
            print("❌ No test files available")
            return None
        
        print(f"Creating batch job '{name}' with {len(file_paths)} files...")
        
        payload = {
            "name": name,
            "description": f"Test batch job created at {datetime.now().isoformat()}",
            "type": job_type,
            "priority": priority,
            "file_paths": file_paths,
            "user_id": "test_user",
            "tags": ["test", "batch", "automated"],
            "metadata": {
                "created_by": "batch_test.py",
                "test_run": True,
                "num_files": len(file_paths)
            },
            "config": {
                "enable_transcription": True,
                "enable_summarization": True,
                "enable_channel_separation": True,
                "max_concurrent_files": 2,
                "transcription_quality": "high",
                "summarization_length": "medium",
                "include_timestamps": True,
                "include_confidence": True,
                "use_gpu_acceleration": True
            }
        }
        
        try:
            response = self.session.post(
                f"{self.base_url}/batch/jobs",
                json=payload,
                timeout=30
            )
            
            if response.status_code == 201:
                job_data = response.json()
                job_id = job_data['data']['id']
                print(f"✅ Batch job created successfully: {job_id}")
                return job_data['data']
            else:
                print(f"❌ Failed to create batch job: {response.status_code}")
                print(f"Response: {response.text}")
                return None
                
        except Exception as e:
            print(f"❌ Error creating batch job: {e}")
            return None
    
    def start_batch_job(self, job_id: str) -> bool:
        """Start a batch job"""
        print(f"Starting batch job {job_id}...")
        
        try:
            response = self.session.post(
                f"{self.base_url}/batch/jobs/{job_id}/start",
                timeout=10
            )
            
            if response.status_code == 200:
                print(f"✅ Batch job {job_id} started successfully")
                return True
            else:
                print(f"❌ Failed to start batch job: {response.status_code}")
                print(f"Response: {response.text}")
                return False
                
        except Exception as e:
            print(f"❌ Error starting batch job: {e}")
            return False
    
    def get_job_progress(self, job_id: str) -> Optional[Dict]:
        """Get batch job progress"""
        try:
            response = self.session.get(
                f"{self.base_url}/batch/jobs/{job_id}/progress",
                timeout=10
            )
            
            if response.status_code == 200:
                return response.json()['data']
            else:
                print(f"❌ Failed to get job progress: {response.status_code}")
                return None
                
        except Exception as e:
            print(f"❌ Error getting job progress: {e}")
            return None
    
    def monitor_job_progress(self, job_id: str, timeout: int = 300) -> bool:
        """Monitor batch job progress until completion"""
        print(f"Monitoring batch job {job_id} progress...")
        
        start_time = time.time()
        last_progress = -1
        
        while time.time() - start_time < timeout:
            progress_data = self.get_job_progress(job_id)
            if not progress_data:
                time.sleep(5)
                continue
            
            status = progress_data.get('status', 'unknown')
            progress = progress_data.get('progress', 0)
            processed = progress_data.get('processed_files', 0)
            total = progress_data.get('total_files', 0)
            successful = progress_data.get('successful_files', 0)
            failed = progress_data.get('failed_files', 0)
            throughput = progress_data.get('throughput_fps', 0)
            current_file = progress_data.get('current_file', '')
            
            # Print progress update if changed
            if progress != last_progress:
                print(f"📊 Progress: {progress:.1f}% ({processed}/{total}) | "
                      f"✅ {successful} success | ❌ {failed} failed | "
                      f"⚡ {throughput:.2f} fps")
                
                if current_file:
                    print(f"   Currently processing: {current_file}")
                
                last_progress = progress
            
            # Check if completed
            if status in ['completed', 'failed', 'cancelled', 'partial']:
                if status == 'completed':
                    print(f"🎉 Batch job completed successfully!")
                    print(f"   Final stats: {successful}/{total} files processed successfully")
                elif status == 'partial':
                    print(f"⚠️  Batch job completed with some failures")
                    print(f"   Final stats: {successful}/{total} successful, {failed} failed")
                else:
                    print(f"❌ Batch job ended with status: {status}")
                
                return status in ['completed', 'partial']
            
            time.sleep(5)
        
        print(f"⏰ Timeout reached while monitoring job {job_id}")
        return False
    
    def get_job_details(self, job_id: str) -> Optional[Dict]:
        """Get detailed batch job information"""
        try:
            response = self.session.get(
                f"{self.base_url}/batch/jobs/{job_id}",
                timeout=10
            )
            
            if response.status_code == 200:
                return response.json()['data']
            else:
                print(f"❌ Failed to get job details: {response.status_code}")
                return None
                
        except Exception as e:
            print(f"❌ Error getting job details: {e}")
            return None
    
    def get_job_files(self, job_id: str) -> Optional[List[Dict]]:
        """Get files for a batch job"""
        try:
            response = self.session.get(
                f"{self.base_url}/batch/jobs/{job_id}/files",
                timeout=10
            )
            
            if response.status_code == 200:
                return response.json()['data']
            else:
                print(f"❌ Failed to get job files: {response.status_code}")
                return None
                
        except Exception as e:
            print(f"❌ Error getting job files: {e}")
            return None
    
    def list_batch_jobs(self, user_id: str = "test_user") -> Optional[List[Dict]]:
        """List batch jobs"""
        try:
            response = self.session.get(
                f"{self.base_url}/batch/jobs",
                params={"user_id": user_id, "limit": 20},
                timeout=10
            )
            
            if response.status_code == 200:
                return response.json()['data']
            else:
                print(f"❌ Failed to list batch jobs: {response.status_code}")
                return None
                
        except Exception as e:
            print(f"❌ Error listing batch jobs: {e}")
            return None
    
    def test_batch_cancellation(self) -> bool:
        """Test batch job cancellation"""
        print("\n🧪 Testing batch job cancellation...")
        
        # Create a small batch job
        test_files = self.test_files[:3] if len(self.test_files) >= 3 else self.test_files
        job_data = self.create_batch_job(
            name="Test Cancellation Job",
            file_paths=test_files
        )
        
        if not job_data:
            return False
        
        job_id = job_data['id']
        
        # Start the job
        if not self.start_batch_job(job_id):
            return False
        
        # Wait a moment then cancel
        time.sleep(2)
        
        try:
            response = self.session.post(
                f"{self.base_url}/batch/jobs/{job_id}/cancel",
                timeout=10
            )
            
            if response.status_code == 200:
                print(f"✅ Successfully cancelled job {job_id}")
                
                # Verify status
                progress = self.get_job_progress(job_id)
                if progress and progress.get('status') == 'cancelled':
                    print("✅ Job status confirmed as cancelled")
                    return True
                else:
                    print("❌ Job status not updated to cancelled")
                    return False
            else:
                print(f"❌ Failed to cancel job: {response.status_code}")
                return False
                
        except Exception as e:
            print(f"❌ Error cancelling job: {e}")
            return False
    
    def test_job_retry(self) -> bool:
        """Test batch job retry functionality"""
        print("\n🧪 Testing batch job retry...")
        
        # For this test, we'll create a job and simulate failure by using invalid file paths
        invalid_files = ["/nonexistent/file1.wav", "/nonexistent/file2.wav"]
        
        job_data = self.create_batch_job(
            name="Test Retry Job",
            file_paths=invalid_files
        )
        
        if not job_data:
            return False
        
        job_id = job_data['id']
        
        # Start the job (should fail)
        if not self.start_batch_job(job_id):
            return False
        
        # Wait for job to fail
        time.sleep(10)
        
        # Check if job failed
        progress = self.get_job_progress(job_id)
        if not progress or progress.get('status') not in ['failed', 'partial']:
            print("❌ Job didn't fail as expected")
            return False
        
        print(f"✅ Job failed as expected with status: {progress.get('status')}")
        
        # Now test retry
        try:
            response = self.session.post(
                f"{self.base_url}/batch/jobs/{job_id}/retry",
                timeout=10
            )
            
            if response.status_code == 200:
                print(f"✅ Successfully initiated retry for job {job_id}")
                return True
            else:
                print(f"❌ Failed to retry job: {response.status_code}")
                print(f"Response: {response.text}")
                return False
                
        except Exception as e:
            print(f"❌ Error retrying job: {e}")
            return False
    
    def cleanup_test_files(self):
        """Clean up test files"""
        print("Cleaning up test files...")
        
        for file_path in self.test_files:
            try:
                if os.path.exists(file_path):
                    os.remove(file_path)
            except Exception as e:
                print(f"Warning: Failed to remove {file_path}: {e}")
        
        try:
            if os.path.exists(TEST_FILES_DIR):
                os.rmdir(TEST_FILES_DIR)
        except Exception as e:
            print(f"Warning: Failed to remove directory {TEST_FILES_DIR}: {e}")
        
        print("✅ Cleanup completed")
    
    def run_comprehensive_test(self) -> bool:
        """Run comprehensive batch processing tests"""
        print("🚀 Starting comprehensive batch processing tests...\n")
        
        success_count = 0
        total_tests = 0
        
        try:
            # Test 1: Health check
            total_tests += 1
            if self.test_health_check():
                success_count += 1
            
            print()
            
            # Test 2: Create test files
            total_tests += 1
            if self.create_test_files():
                success_count += 1
                print("✅ Test files created successfully")
            else:
                print("❌ Failed to create test files")
                return False
            
            print()
            
            # Test 3: Create and run a full batch job
            total_tests += 1
            job_data = self.create_batch_job("Comprehensive Test Job")
            if job_data:
                job_id = job_data['id']
                
                # Start the job
                if self.start_batch_job(job_id):
                    # Monitor progress
                    if self.monitor_job_progress(job_id, timeout=600):  # 10 minute timeout
                        success_count += 1
                        print("✅ Full batch processing test completed successfully")
                        
                        # Get final job details
                        final_details = self.get_job_details(job_id)
                        if final_details:
                            print(f"📋 Final job details:")
                            print(f"   Total duration: {final_details.get('total_duration', 'N/A')}")
                            print(f"   Throughput: {final_details.get('throughput_fps', 0):.2f} fps")
                            print(f"   Success rate: {final_details.get('progress', 0):.1f}%")
                        
                        # Get file details
                        files = self.get_job_files(job_id)
                        if files:
                            completed_files = [f for f in files if f['status'] == 'completed']
                            print(f"   Completed files: {len(completed_files)}/{len(files)}")
                    else:
                        print("❌ Batch job monitoring failed")
                else:
                    print("❌ Failed to start batch job")
            else:
                print("❌ Failed to create batch job")
            
            print()
            
            # Test 4: List jobs
            total_tests += 1
            jobs = self.list_batch_jobs()
            if jobs:
                success_count += 1
                print(f"✅ Listed {len(jobs)} batch jobs")
            else:
                print("❌ Failed to list batch jobs")
            
            print()
            
            # Test 5: Cancellation test
            total_tests += 1
            if self.test_batch_cancellation():
                success_count += 1
            
            print()
            
            # Test 6: Retry test
            total_tests += 1
            if self.test_job_retry():
                success_count += 1
            
        finally:
            # Always cleanup
            self.cleanup_test_files()
        
        print(f"\n📊 Test Results: {success_count}/{total_tests} tests passed")
        
        if success_count == total_tests:
            print("🎉 All batch processing tests passed!")
            return True
        else:
            print("❌ Some batch processing tests failed")
            return False

def main():
    """Main test function"""
    print("PocWhisp Batch Processing Test Suite")
    print("=" * 50)
    
    tester = BatchProcessingTester()
    
    try:
        success = tester.run_comprehensive_test()
        exit_code = 0 if success else 1
    except KeyboardInterrupt:
        print("\n⚠️  Test interrupted by user")
        tester.cleanup_test_files()
        exit_code = 130
    except Exception as e:
        print(f"\n💥 Unexpected error: {e}")
        tester.cleanup_test_files()
        exit_code = 1
    
    exit(exit_code)

if __name__ == "__main__":
    main()
