# PocWhisp API Testing Guide

This comprehensive testing guide covers all aspects of testing the PocWhisp API, from unit tests to load testing and continuous integration.

## Table of Contents

1. [Overview](#overview)
2. [Test Suite Structure](#test-suite-structure)
3. [Running Tests](#running-tests)
4. [Test Categories](#test-categories)
5. [Writing Tests](#writing-tests)
6. [Continuous Integration](#continuous-integration)
7. [Performance Testing](#performance-testing)
8. [Security Testing](#security-testing)
9. [Test Coverage](#test-coverage)
10. [Troubleshooting](#troubleshooting)

## Overview

The PocWhisp API testing suite provides comprehensive coverage across multiple layers:

- **Unit Tests**: Test individual components and functions
- **Integration Tests**: Test component interactions and API endpoints
- **Load Tests**: Test system performance under various loads
- **Security Tests**: Validate security measures and vulnerabilities
- **Benchmark Tests**: Measure performance characteristics

## Test Suite Structure

```
pocwhisp/
├── api/
│   ├── models/
│   │   └── response_test.go          # Model unit tests
│   ├── utils/
│   │   └── audio_test.go            # Utility function tests
│   ├── services/
│   │   ├── auth_test.go             # Authentication service tests
│   │   └── cache_test.go            # Cache service tests
│   ├── handlers/
│   │   └── *_test.go                # Handler unit tests
│   ├── middleware/
│   │   └── *_test.go                # Middleware tests
│   └── tests/
│       └── integration_test.go      # API integration tests
└── tests/
    ├── run_tests.py                 # Test runner script
    ├── load_test.py                 # Locust load tests
    ├── test_api.py                  # Python API tests
    ├── integration_test.py          # Python integration tests
    ├── websocket_test.py            # WebSocket tests
    ├── cache_test.py                # Cache endpoint tests
    ├── batch_test.py                # Batch processing tests
    └── swagger_test.py              # API documentation tests
```

## Running Tests

### Quick Start

Run all tests with the comprehensive test runner:

```bash
# Run all tests (unit, integration, load, security)
./tests/run_tests.py

# Run specific test categories
./tests/run_tests.py --unit-only
./tests/run_tests.py --integration-only
./tests/run_tests.py --load-only
./tests/run_tests.py --benchmark-only

# Skip certain test types
./tests/run_tests.py --no-load --no-security

# Custom load test parameters
./tests/run_tests.py --load-users 50 --load-duration 120
```

### Go Tests

```bash
# Run all Go tests with coverage
cd api
go test -v -race -cover ./...

# Run tests for specific package
go test -v -race -cover ./models
go test -v -race -cover ./services
go test -v -race -cover ./handlers

# Generate coverage report
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Run benchmarks
go test -bench=. -benchmem ./...

# Run integration tests
go test -v -race -tags=integration ./tests
```

### Python Tests

```bash
# Run individual Python tests
cd tests
python3 test_api.py
python3 integration_test.py
python3 websocket_test.py
python3 cache_test.py
python3 batch_test.py
python3 swagger_test.py

# Run all Python tests
for test in *.py; do
    if [[ $test == test_* ]] || [[ $test == *_test.py ]]; then
        echo "Running $test..."
        python3 "$test"
    fi
done
```

### Load Testing

```bash
# Basic load test
locust -f tests/load_test.py --host=http://localhost:8080

# Headless load test
locust -f tests/load_test.py --host=http://localhost:8080 \
       --users 10 --spawn-rate 2 --run-time 60s --headless

# Heavy load test
locust -f tests/load_test.py --host=http://localhost:8080 \
       --users 100 --spawn-rate 10 --run-time 300s --headless

# Stress test
locust -f tests/load_test.py --host=http://localhost:8080 \
       --users 200 --spawn-rate 50 --run-time 60s --headless
```

## Test Categories

### 1. Unit Tests

**Purpose**: Test individual functions and components in isolation

**Location**: `api/*/test.go` files

**Coverage**:
- Model serialization/deserialization
- Audio processing utilities
- Authentication service
- Cache operations
- Input validation
- Error handling

**Example**:
```go
func TestTranscriptSegment(t *testing.T) {
    segment := TranscriptSegment{
        Speaker:   "left",
        StartTime: 0.5,
        EndTime:   3.2,
        Text:      "Hello, this is a test.",
    }
    
    assert.Equal(t, "left", segment.Speaker)
    assert.True(t, segment.EndTime > segment.StartTime)
}
```

### 2. Integration Tests

**Purpose**: Test API endpoints and component interactions

**Location**: `api/tests/integration_test.go`, `tests/*_test.py`

**Coverage**:
- HTTP API endpoints
- Authentication flows
- File upload handling
- Database operations
- Cache integration
- Error responses

**Example**:
```go
func (suite *IntegrationTestSuite) TestHealthEndpoints() {
    response := suite.expect.GET("/api/v1/health").
        Expect().
        Status(http.StatusOK).
        JSON().Object()
    
    response.Value("status").String().NotEmpty()
    response.Value("components").Object().NotEmpty()
}
```

### 3. Load Tests

**Purpose**: Test system performance under various load conditions

**Location**: `tests/load_test.py`

**Scenarios**:
- Normal user behavior simulation
- Admin operations
- Stress testing
- Spike testing
- Endurance testing

**Metrics Tracked**:
- Response times
- Throughput (requests/second)
- Error rates
- Resource utilization
- Concurrent user handling

### 4. Security Tests

**Purpose**: Validate security measures and identify vulnerabilities

**Tools Used**:
- gosec (Go security scanner)
- Custom authentication tests
- Rate limiting validation
- Input sanitization tests

**Coverage**:
- JWT token validation
- API key security
- Rate limiting
- Input validation
- SQL injection protection
- XSS prevention

### 5. Benchmark Tests

**Purpose**: Measure performance characteristics of critical functions

**Location**: Benchmark functions in `*_test.go` files

**Metrics**:
- Operations per second
- Memory allocations
- CPU usage
- Latency percentiles

**Example**:
```go
func BenchmarkGenerateTokenPair(b *testing.B) {
    authService := NewAuthService(DefaultAuthConfig())
    testUser := UserInfo{...}
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, err := authService.GenerateTokenPair(testUser)
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

## Writing Tests

### Go Test Guidelines

1. **Use testify**: Leverage `stretchr/testify` for assertions
2. **Table-driven tests**: Use for multiple test cases
3. **Setup/teardown**: Use `TestMain` for global setup
4. **Mocking**: Use interfaces and dependency injection
5. **Race detection**: Always run with `-race` flag
6. **Coverage**: Aim for >80% code coverage

### Test Structure

```go
func TestComponentName(t *testing.T) {
    t.Run("TestCase1", func(t *testing.T) {
        // Arrange
        input := setupTestData()
        
        // Act
        result := functionUnderTest(input)
        
        // Assert
        assert.Equal(t, expected, result)
    })
}
```

### Python Test Guidelines

1. **Use requests**: For HTTP API testing
2. **Use pytest**: For test discovery and fixtures
3. **Use faker**: For generating test data
4. **Async testing**: Use asyncio for WebSocket tests
5. **Error handling**: Test both success and failure cases

## Continuous Integration

### GitHub Actions Workflow

The project includes comprehensive CI/CD workflows:

- **Testing**: Run all test suites on PR and push
- **Security**: Security scanning and vulnerability checks
- **Performance**: Benchmark regression testing
- **Coverage**: Code coverage reporting

### Pre-commit Hooks

Recommended pre-commit hooks:

```yaml
repos:
  - repo: https://github.com/golangci/golangci-lint
    rev: v1.54.2
    hooks:
      - id: golangci-lint
  - repo: https://github.com/securecodewarrior/github-action-add-sarif
    rev: v1
    hooks:
      - id: gosec
```

## Performance Testing

### Load Test Scenarios

1. **Light Load**: 10 users, 2/s spawn rate, 60s duration
2. **Normal Load**: 50 users, 5/s spawn rate, 300s duration
3. **Heavy Load**: 100 users, 10/s spawn rate, 600s duration
4. **Stress Test**: 200 users, 50/s spawn rate, 60s duration
5. **Spike Test**: 500 users, 100/s spawn rate, 30s duration

### Performance Targets

- **Response Time**: 95th percentile < 500ms
- **Throughput**: > 100 requests/second
- **Error Rate**: < 1% under normal load
- **CPU Usage**: < 80% under normal load
- **Memory Usage**: < 1GB under normal load

### Monitoring During Tests

```bash
# Monitor system resources
htop
iostat -x 1
free -h

# Monitor Go runtime
go tool pprof http://localhost:8080/debug/pprof/profile
go tool pprof http://localhost:8080/debug/pprof/heap
```

## Security Testing

### Automated Security Scans

```bash
# Go security scanner
gosec ./...

# Dependency vulnerability check
go list -json -deps ./... | nancy sleuth

# Docker image scanning
trivy image pocwhisp:latest
```

### Manual Security Tests

1. **Authentication bypass attempts**
2. **Authorization escalation tests**
3. **Input validation fuzzing**
4. **Rate limiting verification**
5. **CORS policy validation**

## Test Coverage

### Coverage Targets

- **Overall**: > 80%
- **Critical paths**: > 95%
- **New code**: > 90%

### Coverage Reports

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# View coverage by function
go tool cover -func=coverage.out

# Coverage exclusions (add to files as needed)
//go:coverage ignore
```

### Coverage Analysis

Areas requiring special attention:
- Error handling paths
- Edge cases
- Integration points
- Security-critical functions

## Troubleshooting

### Common Issues

1. **Tests failing due to timing**:
   - Use proper synchronization
   - Increase timeouts for slow operations
   - Use eventually assertions

2. **Flaky tests**:
   - Identify race conditions
   - Use deterministic test data
   - Implement proper cleanup

3. **Memory leaks in tests**:
   - Clean up resources in teardown
   - Use context cancellation
   - Monitor goroutine leaks

4. **Database state pollution**:
   - Use transactions that rollback
   - Use separate test databases
   - Clean state between tests

### Debug Test Failures

```bash
# Run with verbose output
go test -v ./...

# Run specific test
go test -v -run TestSpecificFunction ./package

# Run with race detection
go test -race ./...

# Debug with delve
dlv test -- -test.run TestSpecificFunction
```

### Environment Variables for Testing

```bash
export POCWHISP_ENV=test
export POCWHISP_LOG_LEVEL=debug
export POCWHISP_DB_URL=sqlite::memory:
export POCWHISP_REDIS_URL=redis://localhost:6379/1
export POCWHISP_AI_SERVICE_URL=http://localhost:8081
```

## Test Data Management

### Test Fixtures

```go
// Use testfixtures for database seeding
fixtures, err := testfixtures.New(
    testfixtures.Database(db),
    testfixtures.Dialect("sqlite"),
    testfixtures.Directory("fixtures"),
)
```

### Mock Data Generation

```python
# Python faker for test data
from faker import Faker
fake = Faker()

test_user = {
    "username": fake.user_name(),
    "email": fake.email(),
    "password": fake.password()
}
```

## Best Practices

1. **Test Isolation**: Each test should be independent
2. **Deterministic**: Tests should produce consistent results
3. **Fast**: Unit tests should run quickly
4. **Maintainable**: Tests should be easy to understand and modify
5. **Comprehensive**: Cover both happy path and error cases
6. **Realistic**: Use realistic test data and scenarios

## Reporting

### Test Results

The test runner generates comprehensive reports:

- **JSON Report**: `tests/test_report.json`
- **HTML Coverage**: `api/coverage.html`
- **Load Test Report**: `tests/load_test_report.html`
- **Security Report**: Generated by gosec and other tools

### Metrics Tracking

Key metrics to track over time:
- Test execution time
- Code coverage percentage
- Number of tests
- Flaky test rate
- Performance benchmarks

---

For questions or issues with testing, please refer to the project documentation or create an issue in the repository.
