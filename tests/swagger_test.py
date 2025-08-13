#!/usr/bin/env python3
"""
Test script for PocWhisp API Swagger Documentation
Tests the Swagger UI and OpenAPI specification endpoints
"""

import requests
import json
import time

BASE_URL = "http://localhost:8080"

def test_swagger_ui():
    """Test that Swagger UI is accessible"""
    print("🔍 Testing Swagger UI accessibility...")
    
    try:
        response = requests.get(f"{BASE_URL}/docs/", timeout=10)
        
        if response.status_code == 200:
            print("✅ Swagger UI is accessible")
            print(f"   Content-Type: {response.headers.get('content-type', 'unknown')}")
            print(f"   Content-Length: {response.headers.get('content-length', 'unknown')} bytes")
            return True
        else:
            print(f"❌ Swagger UI returned status {response.status_code}")
            return False
            
    except requests.exceptions.RequestException as e:
        print(f"❌ Failed to access Swagger UI: {e}")
        return False

def test_openapi_spec():
    """Test that OpenAPI specification is available"""
    print("\n🔍 Testing OpenAPI specification...")
    
    try:
        response = requests.get(f"{BASE_URL}/docs/doc.json", timeout=10)
        
        if response.status_code == 200:
            try:
                spec = response.json()
                print("✅ OpenAPI specification is accessible")
                print(f"   Swagger Version: {spec.get('swagger', 'unknown')}")
                print(f"   API Title: {spec.get('info', {}).get('title', 'unknown')}")
                print(f"   API Version: {spec.get('info', {}).get('version', 'unknown')}")
                print(f"   API Description: {spec.get('info', {}).get('description', 'No description')[:100]}...")
                
                # Check for key components
                paths = spec.get('paths', {})
                definitions = spec.get('definitions', {})
                security_definitions = spec.get('securityDefinitions', {})
                
                print(f"   Total Endpoints: {len(paths)}")
                print(f"   Total Models: {len(definitions)}")
                print(f"   Security Schemes: {len(security_definitions)}")
                
                # List some key endpoints
                print("\n📡 Available Endpoints:")
                for path, methods in list(paths.items())[:10]:  # Show first 10
                    method_list = list(methods.keys())
                    print(f"   {path}: {', '.join(method_list).upper()}")
                
                if len(paths) > 10:
                    print(f"   ... and {len(paths) - 10} more endpoints")
                
                # List models
                if definitions:
                    print("\n📋 Available Models:")
                    for model_name in list(definitions.keys())[:10]:  # Show first 10
                        print(f"   - {model_name}")
                    
                    if len(definitions) > 10:
                        print(f"   ... and {len(definitions) - 10} more models")
                
                # Check security
                if security_definitions:
                    print("\n🔐 Security Schemes:")
                    for scheme_name, scheme_info in security_definitions.items():
                        scheme_type = scheme_info.get('type', 'unknown')
                        print(f"   - {scheme_name}: {scheme_type}")
                
                return True
                
            except json.JSONDecodeError:
                print("❌ OpenAPI specification is not valid JSON")
                return False
                
        else:
            print(f"❌ OpenAPI specification returned status {response.status_code}")
            return False
            
    except requests.exceptions.RequestException as e:
        print(f"❌ Failed to access OpenAPI specification: {e}")
        return False

def test_api_root():
    """Test the API root endpoint for documentation links"""
    print("\n🔍 Testing API root endpoint...")
    
    try:
        response = requests.get(f"{BASE_URL}/", timeout=10)
        
        if response.status_code == 200:
            try:
                data = response.json()
                print("✅ API root endpoint is accessible")
                print(f"   Service: {data.get('service', 'unknown')}")
                print(f"   Version: {data.get('version', 'unknown')}")
                print(f"   Status: {data.get('status', 'unknown')}")
                
                # Check documentation links
                docs = data.get('documentation', {})
                if docs:
                    print("\n📚 Documentation Links:")
                    for doc_type, url in docs.items():
                        print(f"   {doc_type}: {url}")
                
                # Check features
                features = data.get('features', [])
                if features:
                    print("\n🎯 Available Features:")
                    for feature in features:
                        print(f"   - {feature}")
                
                return True
                
            except json.JSONDecodeError:
                print("❌ API root response is not valid JSON")
                return False
                
        else:
            print(f"❌ API root returned status {response.status_code}")
            return False
            
    except requests.exceptions.RequestException as e:
        print(f"❌ Failed to access API root: {e}")
        return False

def test_health_endpoint():
    """Test the health endpoint to ensure API is running"""
    print("\n🔍 Testing health endpoint...")
    
    try:
        response = requests.get(f"{BASE_URL}/api/v1/health", timeout=10)
        
        if response.status_code == 200:
            try:
                data = response.json()
                print("✅ Health endpoint is accessible")
                print(f"   Status: {data.get('status', 'unknown')}")
                print(f"   Uptime: {data.get('uptime', 'unknown')} seconds")
                
                # Check components
                components = data.get('components', {})
                if components:
                    print("   Components:")
                    for component, status in components.items():
                        status_icon = "✅" if status == "healthy" else "⚠️"
                        print(f"     {status_icon} {component}: {status}")
                
                return True
                
            except json.JSONDecodeError:
                print("❌ Health response is not valid JSON")
                return False
                
        else:
            print(f"❌ Health endpoint returned status {response.status_code}")
            return False
            
    except requests.exceptions.RequestException as e:
        print(f"❌ Failed to access health endpoint: {e}")
        return False

def validate_swagger_completeness(spec):
    """Validate that the Swagger spec has good coverage"""
    print("\n🔍 Validating Swagger completeness...")
    
    paths = spec.get('paths', {})
    definitions = spec.get('definitions', {})
    
    # Expected endpoints
    expected_endpoints = [
        '/health', '/ready', '/live',  # Health
        '/transcribe',  # Core transcription
        '/auth/login', '/auth/register',  # Authentication
        '/batch/jobs',  # Batch processing
        '/cache/stats',  # Cache management
    ]
    
    # Check endpoint coverage
    found_endpoints = []
    missing_endpoints = []
    
    for expected in expected_endpoints:
        found = False
        for path in paths.keys():
            if expected in path:
                found_endpoints.append(path)
                found = True
                break
        if not found:
            missing_endpoints.append(expected)
    
    print(f"📊 Endpoint Coverage: {len(found_endpoints)}/{len(expected_endpoints)}")
    
    if found_endpoints:
        print("✅ Found endpoints:")
        for endpoint in found_endpoints:
            print(f"   - {endpoint}")
    
    if missing_endpoints:
        print("⚠️ Missing endpoints:")
        for endpoint in missing_endpoints:
            print(f"   - {endpoint}")
    
    # Expected models
    expected_models = [
        'ErrorResponse', 'TranscriptionResponse', 'HealthResponse',
        'AsyncResponse', 'LoginRequest', 'AuthResponse'
    ]
    
    found_models = []
    missing_models = []
    
    for expected in expected_models:
        if expected in definitions:
            found_models.append(expected)
        else:
            missing_models.append(expected)
    
    print(f"\n📋 Model Coverage: {len(found_models)}/{len(expected_models)}")
    
    if found_models:
        print("✅ Found models:")
        for model in found_models:
            print(f"   - {model}")
    
    if missing_models:
        print("⚠️ Missing models:")
        for model in missing_models:
            print(f"   - {model}")
    
    # Overall completeness score
    total_expected = len(expected_endpoints) + len(expected_models)
    total_found = len(found_endpoints) + len(found_models)
    completeness = (total_found / total_expected) * 100 if total_expected > 0 else 0
    
    print(f"\n🎯 Overall Completeness: {completeness:.1f}%")
    
    return completeness >= 80  # 80% or higher is considered good

def main():
    """Run all Swagger documentation tests"""
    print("🚀 PocWhisp API Swagger Documentation Test Suite")
    print("=" * 60)
    
    tests = [
        ("API Health Check", test_health_endpoint),
        ("API Root Endpoint", test_api_root),
        ("Swagger UI", test_swagger_ui),
        ("OpenAPI Specification", test_openapi_spec),
    ]
    
    results = []
    
    for test_name, test_func in tests:
        print(f"\n🧪 Running: {test_name}")
        print("-" * 40)
        
        try:
            success = test_func()
            results.append((test_name, success))
            
            if success:
                print(f"✅ {test_name}: PASSED")
            else:
                print(f"❌ {test_name}: FAILED")
                
        except Exception as e:
            print(f"💥 {test_name}: ERROR - {e}")
            results.append((test_name, False))
    
    # Additional validation if OpenAPI spec was successfully retrieved
    try:
        response = requests.get(f"{BASE_URL}/docs/doc.json", timeout=10)
        if response.status_code == 200:
            spec = response.json()
            completeness_passed = validate_swagger_completeness(spec)
            results.append(("Swagger Completeness", completeness_passed))
    except:
        results.append(("Swagger Completeness", False))
    
    # Summary
    print("\n" + "=" * 60)
    print("📊 TEST SUMMARY")
    print("=" * 60)
    
    passed = sum(1 for _, success in results if success)
    total = len(results)
    
    for test_name, success in results:
        status = "✅ PASSED" if success else "❌ FAILED"
        print(f"{test_name:<30} {status}")
    
    print(f"\nResults: {passed}/{total} tests passed")
    
    if passed == total:
        print("🎉 All tests passed! Swagger documentation is working correctly.")
        return True
    else:
        print("⚠️ Some tests failed. Check the API server and documentation.")
        return False

if __name__ == "__main__":
    success = main()
    exit(0 if success else 1)
