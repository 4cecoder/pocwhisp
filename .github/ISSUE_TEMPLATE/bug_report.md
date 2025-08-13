---
name: 🐛 Bug Report
about: Create a report to help us improve
title: '[BUG] '
labels: ['bug', 'needs-triage']
assignees: ''
---

## 🐛 Bug Description

<!-- A clear and concise description of what the bug is -->

## 🔄 Reproduction Steps

Steps to reproduce the behavior:

1. Go to '...'
2. Click on '....'
3. Scroll down to '....'
4. See error

## ✅ Expected Behavior

<!-- A clear and concise description of what you expected to happen -->

## ❌ Actual Behavior

<!-- A clear and concise description of what actually happened -->

## 📸 Screenshots

<!-- If applicable, add screenshots to help explain your problem -->

## 🌍 Environment

**System Information:**
- OS: [e.g. Ubuntu 20.04, macOS 12.0, Windows 11]
- Browser: [e.g. Chrome 96, Firefox 95, Safari 15] (if applicable)
- Node.js Version: [e.g. 16.14.0] (if applicable)
- Docker Version: [e.g. 20.10.12] (if applicable)

**Application Environment:**
- Deployment Method: [Local Development / Docker / Production]
- API Version: [e.g. v1.0.0]
- AI Service Version: [e.g. v1.0.0]
- Configuration: [CPU / GPU]

## 📋 Audio File Information (if applicable)

**Audio Details:**
- File Format: [e.g. WAV, MP3]
- Duration: [e.g. 5 minutes]
- Channels: [Mono / Stereo]
- Sample Rate: [e.g. 16kHz]
- File Size: [e.g. 10MB]
- Bit Depth: [e.g. 16-bit]

## 📊 Request/Response Details

**API Request:**
```bash
# Paste your curl command or API request here
curl -X POST -F "audio=@example.wav" http://localhost:8080/api/v1/transcribe
```

**Response:**
```json
{
  "error": "Error message here",
  "status": 500
}
```

## 📝 Logs

**API Logs:**
```
[2024-01-01 12:00:00] ERROR: Something went wrong
```

**AI Service Logs:**
```
[2024-01-01 12:00:00] ERROR: Model loading failed
```

**Browser Console (if applicable):**
```
Error: Network request failed
```

## 🔍 Additional Context

<!-- Add any other context about the problem here -->

## 🛠️ Possible Solution

<!-- If you have ideas on how to fix the issue, please describe them here -->

## 🔗 Related Issues

<!-- Link any related issues using #issue_number -->

## ☑️ Checklist

<!-- Please check all applicable items -->

- [ ] I have searched existing issues to ensure this is not a duplicate
- [ ] I have provided all requested information
- [ ] I have tested this with the latest version
- [ ] I have included relevant logs and error messages
- [ ] I have provided steps to reproduce the issue
- [ ] I have included audio file details (if applicable)

---

**Priority Level:**
- [ ] 🔥 Critical (system completely broken)
- [ ] 🚨 High (major functionality broken)
- [ ] 🟡 Medium (some functionality affected)
- [ ] 🟢 Low (minor issue or cosmetic)

**Impact:**
- [ ] 🏢 Affects production environment
- [ ] 🧪 Affects testing/staging environment  
- [ ] 💻 Affects development environment
- [ ] 📚 Documentation issue
- [ ] 🔒 Security concern
