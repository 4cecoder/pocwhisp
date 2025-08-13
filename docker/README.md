# PocWhisp Docker Deployment

This directory contains Docker configurations for deploying PocWhisp in various environments.

## Quick Start

### 1. Development Environment

```bash
# Start development environment with hot reloading
docker-compose -f docker-compose.dev.yml up

# Or with specific services
docker-compose -f docker-compose.dev.yml up api-dev ai-service-dev
```

### 2. Production Environment (CPU-only)

```bash
# Copy and configure environment
cp env.example .env
# Edit .env with your configuration

# Start CPU-only services
COMPOSE_PROFILES=cpu-only docker-compose up -d
```

### 3. Production Environment (GPU)

```bash
# Ensure NVIDIA Docker runtime is installed
# See: https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html

# Start with GPU support
docker-compose up -d
```

## Architecture

### Services

- **api**: Go Fiber web server
- **ai-service**: Python FastAPI with Whisper/Llama
- **database**: PostgreSQL for production data
- **redis**: Caching and job queue
- **nginx**: Reverse proxy and load balancer
- **prometheus**: Metrics collection (optional)
- **grafana**: Monitoring dashboard (optional)

### Profiles

- **default**: Full production stack with GPU
- **cpu-only**: CPU-only AI processing
- **monitoring**: Add Prometheus + Grafana
- **tools**: Development utilities

## Configuration

### Environment Variables

Key variables in `.env`:

```bash
# Model Selection
WHISPER_MODEL=base          # tiny|base|small|medium|large|large-v3
LLAMA_MODEL_PATH=           # Path to Llama model (optional)

# Performance
GPU_MEMORY_FRACTION=0.8     # GPU memory usage
MAX_AUDIO_LENGTH=1800       # Max audio seconds

# Security
DB_PASSWORD=secure_password
GRAFANA_PASSWORD=admin_pass
```

### GPU Requirements

For GPU deployment:
- NVIDIA GPU with CUDA support
- NVIDIA Container Runtime
- 6-8GB VRAM for Whisper Large V3
- 14GB+ VRAM for Llama-2-7B

## Deployment Scenarios

### Scenario 1: Development

```bash
# Hot reloading, debug logs, CPU-only
docker-compose -f docker-compose.dev.yml up
```

**Features:**
- Live code reloading
- Debug logging
- SQLite database
- CPU-only processing
- Port 8080 (API), 8081 (AI)

### Scenario 2: Staging/Testing

```bash
# Production-like with CPU fallback
COMPOSE_PROFILES=cpu-only docker-compose up -d
```

**Features:**
- PostgreSQL database
- Redis caching
- Nginx reverse proxy
- Health checks
- CPU-only AI (faster startup)

### Scenario 3: Production (CPU-only)

```bash
# High availability, no GPU required
COMPOSE_PROFILES=cpu-only,monitoring docker-compose up -d
```

**Features:**
- Full monitoring stack
- Rate limiting
- SSL termination
- Auto-restart
- Backup-ready volumes

### Scenario 4: Production (GPU)

```bash
# Maximum performance with GPU acceleration
COMPOSE_PROFILES=monitoring docker-compose up -d
```

**Features:**
- GPU-accelerated AI processing
- Load balancing
- Prometheus metrics
- Grafana dashboards
- Production security

## Volumes

### Persistent Data

- `postgres_data`: Database storage
- `redis_data`: Cache and queue data
- `ai_models`: Downloaded AI models
- `prometheus_data`: Metrics history
- `grafana_data`: Dashboard configuration

### Temporary Data

- `ai_temp`: Audio processing temporary files
- `api_temp`: API temporary uploads

## Networking

### Internal Network: `pocwhisp-network`

- Subnet: 172.20.0.0/16
- Isolated from host network
- Service discovery by name

### Port Mapping

- `80`: HTTP (redirects to HTTPS)
- `443`: HTTPS (production)
- `8080`: API (development)
- `8081`: AI Service (development)
- `3000`: Grafana dashboard
- `9090`: Prometheus metrics

## Health Checks

All services include comprehensive health checks:

```bash
# Check service status
docker-compose ps

# View service logs
docker-compose logs api
docker-compose logs ai-service

# Manual health check
curl http://localhost:8080/api/v1/health
```

## Scaling

### Horizontal Scaling

```yaml
# In docker-compose.override.yml
services:
  api:
    deploy:
      replicas: 3
  
  ai-service:
    deploy:
      replicas: 2
```

### Resource Limits

```yaml
services:
  ai-service:
    deploy:
      resources:
        limits:
          memory: 8G
          cpus: '4'
        reservations:
          memory: 4G
          cpus: '2'
```

## Monitoring

### Metrics Endpoints

- API: `http://localhost:8080/api/v1/metrics`
- AI Service: `http://localhost:8081/health/metrics`
- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3000`

### Key Metrics

- Request latency (P50, P95, P99)
- Audio processing time
- GPU memory usage
- Queue depth
- Error rates

## Security

### Production Security

1. **SSL/TLS**: Configure certificates in `/docker/ssl/`
2. **Secrets**: Use Docker secrets for passwords
3. **Firewall**: Restrict ports 5432, 6379, 9090
4. **Authentication**: Enable API keys
5. **Rate Limiting**: Configured in Nginx

### Development Security

- Default passwords (change for production)
- Open CORS policy
- Debug logging enabled
- No SSL termination

## Troubleshooting

### Common Issues

1. **GPU not detected**
   ```bash
   # Install NVIDIA Container Runtime
   sudo apt-get install nvidia-container-runtime
   sudo systemctl restart docker
   ```

2. **Out of memory**
   ```bash
   # Reduce GPU memory fraction
   echo "GPU_MEMORY_FRACTION=0.5" >> .env
   docker-compose restart ai-service
   ```

3. **Model download slow**
   ```bash
   # Pre-download models
   docker-compose exec ai-service python -c "import whisper; whisper.load_model('base')"
   ```

4. **Database connection issues**
   ```bash
   # Check database health
   docker-compose exec database pg_isready -U pocwhisp
   ```

### Log Analysis

```bash
# View all logs
docker-compose logs -f

# Filter by service
docker-compose logs -f api | grep ERROR

# Check health status
docker-compose exec api curl localhost:8080/api/v1/health
```

## Backup & Recovery

### Database Backup

```bash
# Create backup
docker-compose exec database pg_dump -U pocwhisp pocwhisp > backup.sql

# Restore backup
docker-compose exec -T database psql -U pocwhisp pocwhisp < backup.sql
```

### Volume Backup

```bash
# Backup all volumes
docker run --rm -v pocwhisp_postgres_data:/data -v $(pwd):/backup alpine tar czf /backup/postgres_backup.tar.gz -C /data .
```

## Updates

### Rolling Updates

```bash
# Update images
docker-compose pull

# Rolling restart
docker-compose up -d --force-recreate --no-deps api
docker-compose up -d --force-recreate --no-deps ai-service
```

### Zero-Downtime Deployment

```bash
# Scale up
docker-compose up -d --scale api=2

# Health check new instance
# Scale down old instance
docker-compose up -d --scale api=1
```
