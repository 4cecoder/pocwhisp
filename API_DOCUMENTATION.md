# 📚 PocWhisp API Documentation

## 🚀 **Interactive API Explorer**

### **Access Swagger UI**
```bash
http://localhost:8080/docs/
```

### **OpenAPI Specification**
```bash
http://localhost:8080/docs/doc.json
```

---

## 🎯 **Quick Start Guide**

### **1. Start the API Server**
```bash
cd api
go run .
```

### **2. Register a New User**
```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "testuser",
    "email": "test@example.com",
    "password": "securepassword123"
  }'
```

### **3. Login and Get JWT Token**
```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "testuser",
    "password": "securepassword123"
  }'
```

### **4. Upload and Transcribe Audio**
```bash
curl -X POST http://localhost:8080/api/v1/transcribe \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -F "file=@test.wav" \
  -F "enable_summarization=true" \
  -F "channel_separation=true"
```

---

## 🔐 **Authentication & Authorization**

### **JWT Token Authentication**
```http
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

### **API Key Authentication**
```http
X-API-Key: pk_1234567890abcdef...
```

### **User Roles & Permissions**
- **`admin`**: Full system access
- **`user`**: Standard transcription access
- **`batch_user`**: Batch processing permissions
- **`api_user`**: Service-to-service access
- **`read_only`**: Read-only access

### **Scopes**
- **`api:read`**: Read API access
- **`api:write`**: Write API access
- **`transcribe`**: Transcription services
- **`batch`**: Batch processing
- **`websocket`**: Real-time streaming
- **`admin`**: Administrative functions

---

## 📡 **Core API Endpoints**

### **🎵 Audio Transcription**

#### **Upload Audio File**
```http
POST /api/v1/transcribe
Content-Type: multipart/form-data

Parameters:
- file: Audio file (WAV, MP3, FLAC, M4A)
- enable_summarization: boolean (optional)
- quality: string [high, medium, low] (optional)
- language: string (optional)
- channel_separation: boolean (optional)
```

**Response:**
```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "transcript": {
    "segments": [
      {
        "speaker": "left",
        "start_time": 0.0,
        "end_time": 3.5,
        "text": "Hello, this is a test conversation."
      }
    ]
  },
  "summary": {
    "text": "This is a brief conversation about testing.",
    "key_points": ["Testing", "Conversation"],
    "sentiment": "neutral"
  },
  "metadata": {
    "file_size": 1048576,
    "duration": 30.5,
    "channels": 2,
    "sample_rate": 44100
  },
  "processing_time": 2.3
}
```

#### **Get Transcription**
```http
GET /api/v1/transcribe/{sessionId}
```

### **🔄 Real-time Streaming**

#### **WebSocket Connection**
```javascript
const ws = new WebSocket('ws://localhost:8080/api/v1/stream');

// Send audio chunks
ws.send(audioChunk);

// Receive transcription results
ws.onmessage = (event) => {
  const result = JSON.parse(event.data);
  console.log('Transcription:', result.text);
};
```

### **📦 Batch Processing**

#### **Create Batch Job**
```http
POST /api/v1/batch/jobs

{
  "name": "Audio Collection Processing",
  "type": "full_processing",
  "file_paths": ["/path/to/audio1.wav", "/path/to/audio2.wav"],
  "config": {
    "enable_summarization": true,
    "transcription_quality": "high",
    "max_concurrent_files": 3
  }
}
```

#### **Get Batch Job Status**
```http
GET /api/v1/batch/jobs/{jobId}
```

#### **Monitor Progress**
```http
GET /api/v1/batch/jobs/{jobId}/progress
```

### **👤 User Management**

#### **User Profile**
```http
GET /api/v1/auth/profile
Authorization: Bearer TOKEN
```

#### **Generate API Key**
```http
POST /api/v1/auth/api-key
Authorization: Bearer TOKEN
```

### **💾 Cache Management**

#### **Cache Statistics**
```http
GET /api/v1/cache/stats
```

#### **Cache Health**
```http
GET /api/v1/cache/health
```

#### **Invalidate Cache**
```http
DELETE /api/v1/cache/key/{key}
DELETE /api/v1/cache/session/{sessionId}
POST /api/v1/cache/invalidate/pattern
```

---

## 🔒 **Rate Limiting**

### **Default Limits**
- **General API**: 1000 requests/hour per IP
- **Transcription**: 50 requests/hour per user
- **Batch Processing**: 10 requests/24 hours per user
- **WebSocket**: 100 connections/minute per IP

### **Rate Limit Headers**
```http
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 999
X-RateLimit-Reset: 1634567890
Retry-After: 3600
```

---

## 🏥 **Health & Monitoring**

### **Health Check**
```http
GET /api/v1/health
```

**Response:**
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "uptime": 3600.5,
  "components": {
    "database": "healthy",
    "redis": "healthy",
    "ai_service": "healthy"
  },
  "system": {
    "memory_usage": 1024000000,
    "cpu_usage": 15.5,
    "goroutines": 42
  }
}
```

### **Readiness Check**
```http
GET /api/v1/ready
```

### **Liveness Check**
```http
GET /api/v1/live
```

---

## 📊 **Response Formats**

### **Success Response**
```json
{
  "status": "success",
  "data": { ... },
  "message": "Operation completed successfully",
  "request_id": "req_12345",
  "timestamp": "2023-10-15T14:30:45Z"
}
```

### **Error Response**
```json
{
  "error": "validation_failed",
  "message": "Invalid file format",
  "code": 400,
  "request_id": "req_12345",
  "timestamp": "2023-10-15T14:30:45Z"
}
```

### **Async Response**
```json
{
  "job_id": "job_12345",
  "status": "queued",
  "message": "Audio file queued for processing",
  "estimated_eta": "30s",
  "check_url": "/api/v1/transcribe/job_12345",
  "request_id": "req_12345",
  "timestamp": "2023-10-15T14:30:45Z"
}
```

---

## 🛠️ **Client Examples**

### **Python Client**
```python
import requests
import json

# Authentication
auth_response = requests.post('http://localhost:8080/api/v1/auth/login', 
    json={'username': 'testuser', 'password': 'password123'})
token = auth_response.json()['data']['tokens']['access_token']

# Upload audio
headers = {'Authorization': f'Bearer {token}'}
files = {'file': open('audio.wav', 'rb')}
data = {'enable_summarization': 'true'}

response = requests.post('http://localhost:8080/api/v1/transcribe',
    headers=headers, files=files, data=data)

print(json.dumps(response.json(), indent=2))
```

### **JavaScript Client**
```javascript
// Authentication
const authResponse = await fetch('http://localhost:8080/api/v1/auth/login', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ username: 'testuser', password: 'password123' })
});
const auth = await authResponse.json();
const token = auth.data.tokens.access_token;

// Upload audio
const formData = new FormData();
formData.append('file', audioFile);
formData.append('enable_summarization', 'true');

const response = await fetch('http://localhost:8080/api/v1/transcribe', {
  method: 'POST',
  headers: { 'Authorization': `Bearer ${token}` },
  body: formData
});

const result = await response.json();
console.log(result);
```

### **cURL Examples**
```bash
# Authentication
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"testuser","password":"password123"}' | \
  jq -r '.data.tokens.access_token')

# Upload audio
curl -X POST http://localhost:8080/api/v1/transcribe \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@test.wav" \
  -F "enable_summarization=true"

# Get user profile
curl -X GET http://localhost:8080/api/v1/auth/profile \
  -H "Authorization: Bearer $TOKEN"

# Create batch job
curl -X POST http://localhost:8080/api/v1/batch/jobs \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test Batch",
    "type": "full_processing",
    "file_paths": ["/path/to/audio1.wav", "/path/to/audio2.wav"]
  }'
```

---

## 🎮 **WebSocket API**

### **Connection**
```javascript
const ws = new WebSocket('ws://localhost:8080/api/v1/stream');
```

### **Send Audio**
```javascript
// Send configuration
ws.send(JSON.stringify({
  type: 'config',
  sample_rate: 16000,
  encoding: 'wav',
  enable_summarization: true
}));

// Send audio chunks
ws.send(audioChunkBuffer);
```

### **Receive Results**
```javascript
ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  
  switch(data.type) {
    case 'partial':
      console.log('Partial:', data.text);
      break;
    case 'final':
      console.log('Final:', data.text);
      break;
    case 'summary':
      console.log('Summary:', data.summary);
      break;
  }
};
```

---

## 🚨 **Error Codes & Handling**

### **Common Error Codes**
- **400**: Bad Request - Invalid parameters
- **401**: Unauthorized - Invalid or missing authentication
- **403**: Forbidden - Insufficient permissions
- **404**: Not Found - Resource doesn't exist
- **413**: Payload Too Large - File size exceeds limit
- **415**: Unsupported Media Type - Invalid file format
- **429**: Too Many Requests - Rate limit exceeded
- **500**: Internal Server Error - Server-side error
- **503**: Service Unavailable - Service temporarily down

### **Error Response Structure**
```json
{
  "error": "rate_limit_exceeded",
  "message": "API rate limit exceeded",
  "code": 429,
  "request_id": "req_12345",
  "timestamp": "2023-10-15T14:30:45Z"
}
```

---

## 🔧 **Configuration**

### **Environment Variables**
```bash
# Server Configuration
PORT=8080
AI_SERVICE_URL=http://localhost:8081

# Database
DATABASE_URL=sqlite://data.db

# Redis
REDIS_URL=redis://localhost:6379

# Authentication
JWT_SECRET=your-secret-key
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL=7d

# Rate Limiting
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS=1000
RATE_LIMIT_WINDOW=1h

# Swagger
SWAGGER_HOST=localhost:8080
```

---

## 📈 **API Metrics**

The API exposes Prometheus metrics at `/api/v1/metrics`:

### **Key Metrics**
- **`http_requests_total`**: Total HTTP requests
- **`http_request_duration_seconds`**: Request duration
- **`audio_processing_total`**: Audio processing count
- **`queue_jobs_total`**: Job queue metrics
- **`cache_operations_total`**: Cache operation count
- **`jwt_tokens_issued_total`**: JWT tokens issued

---

## 🧪 **Testing**

### **Test Suite**
```bash
# Run tests
cd tests
python test_api.py
python integration_test.py
python websocket_test.py
python batch_test.py
python cache_test.py
```

### **Load Testing**
```bash
# Install dependencies
pip install locust

# Run load tests
locust -f load_test.py --host=http://localhost:8080
```

---

## 📚 **Additional Resources**

- **[OpenAPI Specification](http://localhost:8080/docs/doc.json)**
- **[Interactive API Explorer](http://localhost:8080/docs/)**
- **[Repository](https://github.com/fource/pocwhisp)**
- **[Issues & Support](https://github.com/fource/pocwhisp/issues)**

---

## 🎯 **Production Deployment**

### **Docker Deployment**
```bash
# Build and run
docker-compose up -d

# Check health
curl http://localhost:8080/api/v1/health
```

### **Kubernetes Deployment**
```bash
# Apply manifests
kubectl apply -f k8s/

# Check status
kubectl get pods
kubectl logs -f deployment/pocwhisp-api
```

---

**🎉 Ready to transcribe! Visit [http://localhost:8080/docs/](http://localhost:8080/docs/) to explore the interactive API documentation.**
