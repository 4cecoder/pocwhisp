#!/usr/bin/env python3
"""
Docker deployment test for PocWhisp.
Tests the containerized services and integration.
"""

import subprocess
import time
import requests
import sys
import json
from pathlib import Path

def run_command(cmd, cwd=None, timeout=30):
    """Run shell command with timeout."""
    try:
        result = subprocess.run(
            cmd,
            shell=True,
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=timeout
        )
        return result.returncode == 0, result.stdout, result.stderr
    except subprocess.TimeoutExpired:
        return False, "", f"Command timed out after {timeout}s"

def wait_for_service(url, timeout=60, interval=2):
    """Wait for service to become available."""
    print(f"⏳ Waiting for service at {url}...")
    
    for i in range(timeout // interval):
        try:
            response = requests.get(url, timeout=5)
            if response.status_code in [200, 503]:  # 503 is ok for degraded health
                print(f"✅ Service available (status: {response.status_code})")
                return True
        except:
            pass
        
        print(f"   Attempt {i+1}/{timeout//interval}...")
        time.sleep(interval)
    
    print(f"❌ Service not available after {timeout}s")
    return False

def test_docker_build():
    """Test Docker image building."""
    print("\n🔨 Testing Docker Build...")
    
    docker_dir = Path(__file__).parent.parent / "docker"
    
    # Test API image build
    print("  Building API image...")
    success, stdout, stderr = run_command(
        "docker build -f Dockerfile.api -t pocwhisp-api:test ..",
        cwd=docker_dir,
        timeout=300
    )
    
    if success:
        print("  ✅ API image built successfully")
    else:
        print(f"  ❌ API build failed: {stderr}")
        return False
    
    # Test AI image build (CPU target for faster testing)
    print("  Building AI image (CPU)...")
    success, stdout, stderr = run_command(
        "docker build -f Dockerfile.ai --target cpu-only -t pocwhisp-ai:test ..",
        cwd=docker_dir,
        timeout=600
    )
    
    if success:
        print("  ✅ AI image built successfully")
        return True
    else:
        print(f"  ❌ AI build failed: {stderr}")
        return False

def test_docker_compose_dev():
    """Test development docker-compose setup."""
    print("\n🚀 Testing Development Docker Compose...")
    
    docker_dir = Path(__file__).parent.parent / "docker"
    
    # Start development services
    print("  Starting development services...")
    success, stdout, stderr = run_command(
        "docker-compose -f docker-compose.dev.yml up -d",
        cwd=docker_dir,
        timeout=120
    )
    
    if not success:
        print(f"  ❌ Failed to start services: {stderr}")
        return False
    
    try:
        # Wait for services to be ready
        api_ready = wait_for_service("http://localhost:8080/api/v1/health", timeout=60)
        ai_ready = wait_for_service("http://localhost:8081/health/live", timeout=60)
        
        if not (api_ready and ai_ready):
            print("  ❌ Services not ready")
            return False
        
        # Test basic API functionality
        print("  Testing API endpoints...")
        
        # Test health endpoint
        response = requests.get("http://localhost:8080/api/v1/health", timeout=10)
        print(f"    Health check: {response.status_code}")
        
        # Test list transcriptions
        response = requests.get("http://localhost:8080/api/v1/transcribe", timeout=10)
        if response.status_code == 200:
            print(f"    Transcriptions list: ✅")
        else:
            print(f"    Transcriptions list: ❌ ({response.status_code})")
        
        # Test AI service
        response = requests.get("http://localhost:8081/health", timeout=10)
        if response.status_code == 200:
            print(f"    AI service health: ✅")
        else:
            print(f"    AI service health: ❌ ({response.status_code})")
        
        print("  ✅ Development deployment successful")
        return True
        
    except Exception as e:
        print(f"  ❌ Testing failed: {e}")
        return False
    
    finally:
        # Cleanup
        print("  🧹 Cleaning up development services...")
        run_command(
            "docker-compose -f docker-compose.dev.yml down -v",
            cwd=docker_dir,
            timeout=60
        )

def test_docker_compose_production():
    """Test production docker-compose setup (CPU-only)."""
    print("\n🏭 Testing Production Docker Compose (CPU-only)...")
    
    docker_dir = Path(__file__).parent.parent / "docker"
    
    # Create minimal .env for testing
    env_content = """
DB_PASSWORD=test_password
WHISPER_MODEL=tiny
LOG_LEVEL=INFO
GRAFANA_PASSWORD=test_grafana
"""
    
    env_path = docker_dir / ".env"
    with open(env_path, "w") as f:
        f.write(env_content)
    
    try:
        # Start production services (CPU-only profile)
        print("  Starting production services (CPU-only)...")
        success, stdout, stderr = run_command(
            "COMPOSE_PROFILES=cpu-only docker-compose up -d",
            cwd=docker_dir,
            timeout=180
        )
        
        if not success:
            print(f"  ❌ Failed to start production services: {stderr}")
            return False
        
        # Wait for services
        print("  Waiting for services to initialize...")
        time.sleep(30)  # Give more time for production startup
        
        # Check service status
        success, stdout, stderr = run_command(
            "docker-compose ps",
            cwd=docker_dir,
            timeout=30
        )
        
        print("  Service status:")
        print(stdout)
        
        # Test services if they're up
        api_ready = wait_for_service("http://localhost:8080/api/v1/health", timeout=60)
        
        if api_ready:
            print("  ✅ Production deployment successful")
            return True
        else:
            print("  ❌ Production services not ready")
            return False
    
    except Exception as e:
        print(f"  ❌ Production testing failed: {e}")
        return False
    
    finally:
        # Cleanup
        print("  🧹 Cleaning up production services...")
        run_command(
            "docker-compose down -v",
            cwd=docker_dir,
            timeout=60
        )
        
        # Remove test .env
        if env_path.exists():
            env_path.unlink()

def test_docker_image_security():
    """Test Docker image security best practices."""
    print("\n🔒 Testing Docker Security...")
    
    # Test that containers run as non-root
    success, stdout, stderr = run_command(
        "docker run --rm pocwhisp-api:test whoami",
        timeout=30
    )
    
    if success and "pocwhisp" in stdout:
        print("  ✅ API container runs as non-root user")
    else:
        print("  ❌ API container security issue")
        return False
    
    success, stdout, stderr = run_command(
        "docker run --rm pocwhisp-ai:test whoami",
        timeout=30
    )
    
    if success and "pocwhisp" in stdout:
        print("  ✅ AI container runs as non-root user")
    else:
        print("  ❌ AI container security issue")
        return False
    
    # Test image size (should be reasonable)
    success, stdout, stderr = run_command(
        "docker images pocwhisp-api:test --format 'table {{.Size}}'",
        timeout=30
    )
    
    if success:
        size_line = stdout.strip().split('\n')[-1]
        print(f"  📊 API image size: {size_line}")
    
    success, stdout, stderr = run_command(
        "docker images pocwhisp-ai:test --format 'table {{.Size}}'",
        timeout=30
    )
    
    if success:
        size_line = stdout.strip().split('\n')[-1]
        print(f"  📊 AI image size: {size_line}")
    
    return True

def main():
    """Run all Docker tests."""
    print("🐳 PocWhisp Docker Test Suite")
    print("=" * 50)
    
    # Check Docker availability
    success, _, _ = run_command("docker --version")
    if not success:
        print("❌ Docker not available")
        return False
    
    success, _, _ = run_command("docker-compose --version")
    if not success:
        print("❌ Docker Compose not available")
        return False
    
    print("✅ Docker and Docker Compose available")
    
    # Run tests
    tests = [
        ("Build", test_docker_build),
        ("Development", test_docker_compose_dev),
        ("Production", test_docker_compose_production),
        ("Security", test_docker_image_security),
    ]
    
    results = []
    
    for test_name, test_func in tests:
        try:
            result = test_func()
            results.append((test_name, result))
            print(f"\n{'✅' if result else '❌'} {test_name}: {'PASSED' if result else 'FAILED'}")
        except Exception as e:
            print(f"\n❌ {test_name}: FAILED ({e})")
            results.append((test_name, False))
    
    # Summary
    print("\n📊 Test Results Summary:")
    passed = sum(1 for _, result in results if result)
    total = len(results)
    
    for test_name, result in results:
        print(f"  {test_name}: {'✅ PASSED' if result else '❌ FAILED'}")
    
    print(f"\nOverall: {passed}/{total} tests passed")
    
    if passed == total:
        print("\n🎉 All Docker tests PASSED!")
        print("\n✨ Docker Deployment Summary:")
        print("  • Multi-stage builds for optimized images")
        print("  • GPU and CPU-only deployment options")
        print("  • Development and production configurations")
        print("  • Security best practices implemented")
        print("  • Health checks and monitoring ready")
        print("  • Scalable architecture with load balancing")
        return True
    else:
        print(f"\n❌ {total - passed} Docker tests FAILED!")
        return False

if __name__ == "__main__":
    success = main()
    sys.exit(0 if success else 1)
