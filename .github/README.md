# PocWhisp GitHub Automation

This directory contains comprehensive GitHub Actions workflows and configurations for automated testing, security monitoring, and deployment of the PocWhisp project.

## 🚀 Workflows Overview

### 1. **CI - Continuous Integration** (`ci.yml`)
**Triggers:** Push to main/develop, Pull Requests
**Purpose:** Code quality, testing, and validation

**Jobs:**
- **Code Quality & Security**: Linting, security scanning, dependency checks
- **Go Unit Tests**: API service testing with coverage
- **Python Unit Tests**: AI service testing with coverage  
- **Integration Tests**: Full system testing with PostgreSQL/Redis
- **Docker Build & Test**: Multi-service container builds
- **Docker Compose Integration**: End-to-end deployment testing
- **Performance Tests**: Benchmark validation (main branch only)
- **Build Summary**: Comprehensive status reporting

**Features:**
- ✅ Multi-language linting (Go, Python)
- 🔒 Security scanning (Trivy, Gosec, Bandit)
- 📊 Code coverage reporting (Codecov)
- 🐳 Docker image vulnerability scanning
- 🧪 Real database/cache integration testing
- ⚡ Performance regression detection

### 2. **CD - Continuous Deployment** (`cd.yml`)
**Triggers:** Push to main, Version tags, Manual dispatch
**Purpose:** Automated deployment to staging and production

**Jobs:**
- **Build & Push**: Multi-arch Docker images (AMD64, ARM64)
- **Security Scan**: Production image vulnerability assessment
- **Staging Deploy**: Automated staging environment deployment
- **Production Deploy**: Blue-green production deployment (tags only)
- **Rollback**: Automatic rollback on deployment failure
- **Post-Deployment**: End-to-end validation and load testing
- **Deployment Summary**: Status dashboard and notifications

**Features:**
- 🏗️ Multi-architecture container builds
- 🔄 Blue-green production deployments
- 🚨 Automatic rollback on failures
- 📊 Post-deployment validation
- 📢 Slack/email notifications
- 🎯 Manual deployment controls

### 3. **Security Monitoring** (`security.yml`)
**Triggers:** Daily schedule, Dependency changes, Manual dispatch
**Purpose:** Continuous security monitoring and vulnerability detection

**Jobs:**
- **Dependency Scan**: Go and Python vulnerability scanning
- **CodeQL Analysis**: Static code security analysis
- **Container Security**: Docker image security assessment
- **Secrets Scanning**: GitLeaks and TruffleHog detection
- **Infrastructure Scan**: Docker/IaC configuration security
- **Compliance Check**: Security best practices validation
- **Security Dashboard**: Centralized security reporting
- **Alert Management**: Automatic issue creation and notifications

**Features:**
- 🔍 Daily automated security scans
- 🚨 Critical vulnerability alerts
- 📋 Automatic security issue creation
- 🛡️ Comprehensive vulnerability reporting
- 📊 Security score dashboard
- 🔔 Security team notifications

### 4. **Performance Monitoring** (`performance.yml`)
**Triggers:** Weekly schedule, Code changes, Manual dispatch
**Purpose:** Performance benchmarking and regression detection

**Jobs:**
- **Load Testing**: API and full pipeline performance
- **Benchmark Testing**: Go and Python micro-benchmarks
- **Memory Profiling**: Memory usage analysis
- **GPU Performance**: GPU-accelerated performance testing
- **Scalability Testing**: Horizontal scaling validation
- **Regression Testing**: Performance comparison with previous versions
- **Performance Dashboard**: Centralized performance reporting
- **Performance Alerts**: Degradation notifications

**Features:**
- 📈 Automated performance benchmarking
- 🧠 Memory and GPU profiling
- 📊 Performance regression detection
- 🔄 Scalability testing
- 🎯 Custom performance thresholds
- 📢 Performance degradation alerts

### 5. **Release Management** (`release.yml`)
**Triggers:** Version tags, Manual dispatch
**Purpose:** Automated release creation and deployment

**Jobs:**
- **Release Validation**: Version format and tag validation
- **Build Artifacts**: Multi-platform release builds
- **Security Scan**: Release artifact security validation
- **Release Notes**: Automated changelog generation
- **GitHub Release**: Release creation with assets
- **Production Deploy**: Automated production deployment
- **Post-Release**: Documentation updates and notifications
- **Release Summary**: Comprehensive release reporting

**Features:**
- 🏷️ Semantic version validation
- 📝 Automated release notes generation
- 🐳 Multi-platform Docker releases
- 🚀 Production deployment automation
- 📊 Release dashboard and metrics
- 🎉 Team notifications and milestones

## 🛠️ Configuration Files

### **Dependabot** (`.github/dependabot.yml`)
- **Go Dependencies**: Weekly updates for `api/` directory
- **Python Dependencies**: Weekly updates for `ai/` directory  
- **Docker Images**: Weekly base image updates
- **GitHub Actions**: Weekly workflow updates
- **Security Labels**: Automatic security classification
- **Team Assignment**: Automated reviewer assignment

### **Issue Templates**
- **Bug Report**: Comprehensive bug reporting template
- **Feature Request**: Detailed feature proposal template
- **Security Issue**: Security vulnerability reporting
- **Documentation**: Documentation improvement requests

### **Pull Request Template**
- **Change Classification**: Bug fix, feature, breaking change
- **Testing Requirements**: Coverage and validation checklists
- **Security Review**: Security impact assessment
- **Performance Impact**: Performance consideration checklist
- **Documentation Updates**: Documentation requirement tracking

### **Security Configuration** (`.gitleaks.toml`)
- **Secret Detection**: AWS keys, JWT secrets, database passwords
- **Custom Rules**: Application-specific secret patterns
- **Allowlists**: Safe patterns and test data exclusions
- **File Exclusions**: Documentation and test file handling

## 🚦 Workflow Triggers

| Workflow | Push (main) | Push (develop) | PR | Tags | Schedule | Manual |
|----------|-------------|----------------|----|----- |----------|--------|
| **CI** | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ |
| **CD** | ✅ | ❌ | ❌ | ✅ | ❌ | ✅ |
| **Security** | ✅ | ❌ | ❌ | ❌ | ✅ Daily | ✅ |
| **Performance** | ✅ | ❌ | ❌ | ❌ | ✅ Weekly | ✅ |
| **Release** | ❌ | ❌ | ❌ | ✅ | ❌ | ✅ |

## 🔧 Required Secrets

### **GitHub Secrets**
```bash
# AWS Deployment
AWS_ACCESS_KEY_ID          # AWS credentials for ECS deployment
AWS_SECRET_ACCESS_KEY      # AWS secret key

# Container Registry  
GITHUB_TOKEN              # Automatic (GitHub provided)

# Security Scanning
SNYK_TOKEN                # Snyk security scanning
SECURITY_SLACK_WEBHOOK_URL # Security alerts channel

# Notifications
SLACK_WEBHOOK_URL         # General notifications
PERFORMANCE_SLACK_WEBHOOK_URL # Performance alerts
EMAIL_USERNAME            # Email notifications
EMAIL_PASSWORD            # Email credentials
SECURITY_TEAM_EMAIL       # Security team email

# Deployment
DEPLOY_WEBHOOK_URL        # Production deployment webhook
DEPLOY_TOKEN              # Deployment authentication
```

### **Environment Variables**
```bash
# Docker Registry
REGISTRY=ghcr.io
IMAGE_NAME=${{ github.repository }}

# Application Versions
GO_VERSION=1.21
PYTHON_VERSION=3.11

# Deployment Environments
STAGING_URL=https://staging.pocwhisp.com
PRODUCTION_URL=https://api.pocwhisp.com
```

## 📊 Monitoring & Dashboards

### **GitHub Actions Dashboard**
- ✅ Workflow success/failure rates
- ⏱️ Build time trends
- 🔄 Deployment frequency
- 🚨 Alert summaries

### **Security Dashboard**
- 🛡️ Vulnerability scan results
- 🔍 Dependency health scores
- 🚨 Security alert trends
- 📋 Compliance status

### **Performance Dashboard**
- 📈 Performance benchmarks
- 🧠 Memory usage trends
- ⚡ Response time metrics
- 🎯 Regression tracking

## 🚀 Getting Started

### **1. Enable Workflows**
```bash
# All workflows are enabled by default
# Configure required secrets in repository settings
```

### **2. Configure Notifications**
```bash
# Set up Slack webhooks for team notifications
# Configure email alerts for security team
# Set up AWS credentials for deployment
```

### **3. Customize Thresholds**
```bash
# Edit workflow files to adjust:
# - Performance regression thresholds (10% default)
# - Security scan sensitivity
# - Test timeout values
# - Deployment strategies
```

### **4. Monitor Results**
```bash
# Check Actions tab for workflow status
# Review Security tab for vulnerability reports
# Monitor deployment notifications
# Track performance trends
```

## 🎯 Best Practices

### **Branch Protection**
- ✅ Require status checks (CI workflow)
- ✅ Require up-to-date branches
- ✅ Include administrators
- ✅ Require linear history

### **Security**
- 🔒 Enable vulnerability alerts
- 🔍 Review Dependabot PRs promptly
- 🚨 Monitor security workflow failures
- 📋 Regular security team reviews

### **Performance**
- 📊 Monitor performance trends
- 🎯 Set realistic regression thresholds
- ⚡ Optimize based on benchmark results
- 🔄 Regular performance reviews

### **Deployment**
- 🚀 Use semantic versioning for releases
- 🧪 Validate staging deployments
- 📋 Review deployment notifications
- 🔄 Plan rollback procedures

## 🆘 Troubleshooting

### **Common Issues**

**Workflow Failures:**
- Check required secrets are configured
- Verify branch protection rules
- Review workflow logs for specific errors
- Ensure service dependencies are available

**Security Alerts:**
- Review vulnerability details
- Check if false positive (adjust allowlists)
- Create security issues for critical findings
- Update dependencies promptly

**Performance Degradation:**
- Compare with previous benchmarks
- Check resource usage patterns
- Review recent code changes
- Validate test environment consistency

**Deployment Issues:**
- Verify deployment credentials
- Check service health endpoints
- Review rollback procedures
- Monitor infrastructure status

### **Support Resources**
- 📚 GitHub Actions Documentation
- 🛠️ Workflow troubleshooting guides
- 🔧 Security scanning help
- 📊 Performance optimization tips

---

**🎉 The GitHub automation provides comprehensive CI/CD, security monitoring, and performance tracking for production-ready deployments!**
