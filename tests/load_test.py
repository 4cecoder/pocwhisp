#!/usr/bin/env python3
"""
Load Testing Suite for PocWhisp API using Locust
Comprehensive performance testing with realistic user scenarios
"""

import json
import random
import time
import io
import wave
import struct
from pathlib import Path

from locust import HttpUser, task, between, events
from locust.exception import StopUser


class PocWhispUser(HttpUser):
    """Simulates a user interacting with the PocWhisp API"""
    
    wait_time = between(1, 5)  # Wait 1-5 seconds between requests
    
    def __init__(self, environment):
        super().__init__(environment)
        self.token = None
        self.api_key = None
        self.user_id = None
        self.session_ids = []
        
    def on_start(self):
        """Called when a user starts - register and login"""
        self.register_user()
        self.login_user()
        self.generate_api_key()
    
    def register_user(self):
        """Register a new user"""
        user_id = random.randint(10000, 99999)
        self.user_data = {
            "username": f"loadtest_user_{user_id}",
            "email": f"loadtest_{user_id}@example.com",
            "password": "loadtest123",
            "roles": ["user"]
        }
        
        with self.client.post("/api/v1/auth/register", 
                            json=self.user_data,
                            catch_response=True) as response:
            if response.status_code == 201:
                response.success()
                self.user_id = user_id
            elif response.status_code == 409:
                # User already exists, that's okay
                response.success()
                self.user_id = user_id
            else:
                response.failure(f"Registration failed: {response.status_code}")
    
    def login_user(self):
        """Login and get JWT token"""
        login_data = {
            "username": self.user_data["username"],
            "password": self.user_data["password"]
        }
        
        with self.client.post("/api/v1/auth/login", 
                            json=login_data,
                            catch_response=True) as response:
            if response.status_code == 200:
                data = response.json()
                self.token = data["data"]["tokens"]["access_token"]
                response.success()
            else:
                response.failure(f"Login failed: {response.status_code}")
                raise StopUser()
    
    def generate_api_key(self):
        """Generate API key for the user"""
        if not self.token:
            return
            
        headers = {"Authorization": f"Bearer {self.token}"}
        
        with self.client.post("/api/v1/auth/api-key",
                            headers=headers,
                            catch_response=True) as response:
            if response.status_code == 200:
                data = response.json()
                self.api_key = data["data"]["api_key"]
                response.success()
            else:
                response.failure(f"API key generation failed: {response.status_code}")
    
    @task(10)
    def check_health(self):
        """Health check - high frequency"""
        with self.client.get("/api/v1/health", catch_response=True) as response:
            if response.status_code == 200:
                data = response.json()
                if data.get("status") in ["healthy", "degraded"]:
                    response.success()
                else:
                    response.failure(f"Unhealthy status: {data.get('status')}")
            else:
                response.failure(f"Health check failed: {response.status_code}")
    
    @task(5)
    def check_readiness(self):
        """Readiness check"""
        with self.client.get("/api/v1/ready", catch_response=True) as response:
            if response.status_code == 200:
                response.success()
            else:
                response.failure(f"Readiness check failed: {response.status_code}")
    
    @task(3)
    def get_profile(self):
        """Get user profile"""
        if not self.token:
            return
            
        headers = {"Authorization": f"Bearer {self.token}"}
        
        with self.client.get("/api/v1/auth/profile",
                          headers=headers,
                          catch_response=True) as response:
            if response.status_code == 200:
                response.success()
            else:
                response.failure(f"Profile fetch failed: {response.status_code}")
    
    @task(2)
    def upload_audio_jwt(self):
        """Upload audio file using JWT authentication"""
        if not self.token:
            return
            
        audio_data = self.create_test_audio()
        
        files = {
            'file': ('test_audio.wav', audio_data, 'audio/wav')
        }
        
        data = {
            'enable_summarization': random.choice(['true', 'false']),
            'quality': random.choice(['high', 'medium', 'low']),
            'channel_separation': random.choice(['true', 'false'])
        }
        
        headers = {"Authorization": f"Bearer {self.token}"}
        
        with self.client.post("/api/v1/transcribe",
                            files=files,
                            data=data,
                            headers=headers,
                            catch_response=True) as response:
            # In load testing, AI service might not be available
            # We expect either success or service unavailable
            if response.status_code in [200, 202, 503]:
                if response.status_code == 200:
                    # Store session ID if successful
                    try:
                        resp_data = response.json()
                        if "session_id" in resp_data:
                            self.session_ids.append(resp_data["session_id"])
                    except:
                        pass
                response.success()
            else:
                response.failure(f"Audio upload failed: {response.status_code}")
    
    @task(2)
    def upload_audio_api_key(self):
        """Upload audio file using API key authentication"""
        if not self.api_key:
            return
            
        audio_data = self.create_test_audio()
        
        files = {
            'file': ('test_audio.wav', audio_data, 'audio/wav')
        }
        
        data = {
            'enable_summarization': 'false',  # Faster processing
            'quality': 'medium'
        }
        
        headers = {"X-API-Key": self.api_key}
        
        with self.client.post("/api/v1/transcribe",
                            files=files,
                            data=data,
                            headers=headers,
                            catch_response=True) as response:
            if response.status_code in [200, 202, 503]:
                response.success()
            else:
                response.failure(f"Audio upload (API key) failed: {response.status_code}")
    
    @task(1)
    def get_transcription(self):
        """Get a previous transcription"""
        if not self.session_ids:
            return
            
        session_id = random.choice(self.session_ids)
        headers = {"Authorization": f"Bearer {self.token}"}
        
        with self.client.get(f"/api/v1/transcribe/{session_id}",
                           headers=headers,
                           catch_response=True) as response:
            if response.status_code in [200, 404]:  # 404 is okay if session doesn't exist
                response.success()
            else:
                response.failure(f"Get transcription failed: {response.status_code}")
    
    @task(1)
    def test_rate_limiting(self):
        """Test rate limiting by making rapid requests"""
        for i in range(3):
            with self.client.get("/api/v1/health", catch_response=True) as response:
                if response.status_code == 429:
                    # Rate limited - this is expected behavior
                    response.success()
                elif response.status_code == 200:
                    response.success()
                else:
                    response.failure(f"Unexpected status: {response.status_code}")
            time.sleep(0.1)  # Small delay between requests
    
    def create_test_audio(self):
        """Create a small test WAV file for upload testing"""
        # Create a very small WAV file (1 second, mono, 8kHz)
        sample_rate = 8000
        duration = 1.0
        frequency = 440  # A4 note
        
        frames = int(sample_rate * duration)
        audio_data = []
        
        for i in range(frames):
            # Generate a simple sine wave
            value = int(32767 * 0.1 * random.random())  # Low volume with noise
            audio_data.append(struct.pack('<h', value))
        
        # Create WAV file in memory
        wav_buffer = io.BytesIO()
        
        with wave.open(wav_buffer, 'wb') as wav_file:
            wav_file.setnchannels(1)  # Mono
            wav_file.setsampwidth(2)  # 16-bit
            wav_file.setframerate(sample_rate)
            wav_file.writeframes(b''.join(audio_data))
        
        wav_buffer.seek(0)
        return wav_buffer.getvalue()


class AdminUser(HttpUser):
    """Simulates an admin user with higher privileges"""
    
    wait_time = between(2, 8)
    weight = 1  # Lower weight than regular users
    
    def __init__(self, environment):
        super().__init__(environment)
        self.token = None
    
    def on_start(self):
        """Setup admin user"""
        self.register_admin()
        self.login_admin()
    
    def register_admin(self):
        """Register admin user"""
        self.admin_data = {
            "username": f"admin_loadtest_{random.randint(1000, 9999)}",
            "email": f"admin_loadtest_{random.randint(1000, 9999)}@example.com",
            "password": "admintest123",
            "roles": ["admin", "user"]
        }
        
        with self.client.post("/api/v1/auth/register",
                            json=self.admin_data,
                            catch_response=True) as response:
            if response.status_code in [201, 409]:
                response.success()
            else:
                response.failure(f"Admin registration failed: {response.status_code}")
    
    def login_admin(self):
        """Login admin user"""
        login_data = {
            "username": self.admin_data["username"],
            "password": self.admin_data["password"]
        }
        
        with self.client.post("/api/v1/auth/login",
                            json=login_data,
                            catch_response=True) as response:
            if response.status_code == 200:
                data = response.json()
                self.token = data["data"]["tokens"]["access_token"]
                response.success()
            else:
                response.failure(f"Admin login failed: {response.status_code}")
                raise StopUser()
    
    @task(5)
    def check_cache_stats(self):
        """Check cache statistics (admin endpoint)"""
        if not self.token:
            return
            
        headers = {"Authorization": f"Bearer {self.token}"}
        
        with self.client.get("/api/v1/cache/stats",
                           headers=headers,
                           catch_response=True) as response:
            if response.status_code in [200, 403]:  # 403 if not implemented or no access
                response.success()
            else:
                response.failure(f"Cache stats failed: {response.status_code}")
    
    @task(2)
    def warm_cache(self):
        """Warm up cache (admin operation)"""
        if not self.token:
            return
            
        headers = {"Authorization": f"Bearer {self.token}"}
        
        with self.client.post("/api/v1/cache/warmup",
                            headers=headers,
                            catch_response=True) as response:
            if response.status_code in [200, 202, 403]:
                response.success()
            else:
                response.failure(f"Cache warmup failed: {response.status_code}")


class HealthCheckUser(HttpUser):
    """Dedicated user for health monitoring during load test"""
    
    wait_time = between(1, 2)
    weight = 1
    
    @task
    def continuous_health_check(self):
        """Continuous health monitoring"""
        with self.client.get("/api/v1/health", catch_response=True) as response:
            if response.status_code == 200:
                data = response.json()
                status = data.get("status", "unknown")
                uptime = data.get("uptime", 0)
                
                # Log health metrics
                print(f"Health: {status}, Uptime: {uptime:.2f}s")
                
                if status == "unhealthy":
                    response.failure(f"System is unhealthy: {data}")
                else:
                    response.success()
            else:
                response.failure(f"Health check failed: {response.status_code}")


# Event handlers for custom metrics and logging
@events.request.add_listener
def on_request(request_type, name, response_time, response_length, exception, context, **kwargs):
    """Custom request logging"""
    if exception:
        print(f"Request failed: {request_type} {name} - {exception}")
    elif response_time > 5000:  # Log slow requests (>5s)
        print(f"Slow request: {request_type} {name} - {response_time}ms")


@events.test_start.add_listener
def on_test_start(environment, **kwargs):
    """Log when test starts"""
    print("🚀 Starting PocWhisp API Load Test")
    print(f"Target: {environment.host}")
    print(f"Users: {environment.runner.target_user_count if hasattr(environment.runner, 'target_user_count') else 'N/A'}")


@events.test_stop.add_listener
def on_test_stop(environment, **kwargs):
    """Log when test stops"""
    print("🏁 PocWhisp API Load Test Completed")
    
    # Print summary statistics
    stats = environment.runner.stats
    print(f"Total requests: {stats.total.num_requests}")
    print(f"Failed requests: {stats.total.num_failures}")
    print(f"Average response time: {stats.total.avg_response_time:.2f}ms")
    print(f"Max response time: {stats.total.max_response_time:.2f}ms")
    print(f"Requests per second: {stats.total.total_rps:.2f}")


# Custom load test scenarios
class StressTestUser(HttpUser):
    """Stress testing with aggressive load"""
    
    wait_time = between(0.1, 0.5)  # Very fast requests
    weight = 1
    
    @task(20)
    def rapid_health_checks(self):
        """Rapid health checks to test system limits"""
        with self.client.get("/api/v1/health", catch_response=True) as response:
            if response.status_code in [200, 429, 503]:
                response.success()
            else:
                response.failure(f"Unexpected status: {response.status_code}")
    
    @task(5)
    def rapid_auth_attempts(self):
        """Rapid authentication attempts"""
        # Try to login with random credentials
        login_data = {
            "username": f"stress_user_{random.randint(1, 1000)}",
            "password": "wrongpassword"
        }
        
        with self.client.post("/api/v1/auth/login",
                            json=login_data,
                            catch_response=True) as response:
            # Expect failures due to wrong credentials or rate limiting
            if response.status_code in [401, 429]:
                response.success()
            else:
                response.failure(f"Unexpected status: {response.status_code}")


if __name__ == "__main__":
    """
    Run load test directly with Python
    For full Locust features, use: locust -f load_test.py --host=http://localhost:8080
    """
    import subprocess
    import sys
    
    print("🔥 PocWhisp Load Testing Suite")
    print("Available test scenarios:")
    print("1. Normal load test: locust -f load_test.py --host=http://localhost:8080")
    print("2. Stress test: locust -f load_test.py --host=http://localhost:8080 -u 100 -r 10")
    print("3. Spike test: locust -f load_test.py --host=http://localhost:8080 -u 200 -r 50 -t 60s")
    print("4. Endurance test: locust -f load_test.py --host=http://localhost:8080 -u 50 -r 5 -t 3600s")
    
    print("\nExample commands:")
    print("# Light load (10 users, 2/sec spawn rate)")
    print("locust -f load_test.py --host=http://localhost:8080 -u 10 -r 2")
    print()
    print("# Heavy load (100 users, 10/sec spawn rate)")
    print("locust -f load_test.py --host=http://localhost:8080 -u 100 -r 10")
    print()
    print("# Web UI mode (recommended)")
    print("locust -f load_test.py --host=http://localhost:8080")
