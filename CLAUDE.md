# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

GATC (Google Account Token Collector) is a Go-based automated system for managing Google Cloud Platform (GCP) accounts and virtual machines to obtain Gemini API tokens. The system uses a "white account" (stable GCP account) to create VMs and manages multiple user accounts through an automated registration and project creation workflow.

## Architecture

### High-Level Components

**Core Services:**
- `service/gcp_account_service.go`: Manages GCP account registration, authentication, and lifecycle
- `service/vm_service.go`: Handles VM creation, deletion, and management on GCP
- `service/project_service.go`: Manages GCP project creation, billing, and token generation
- `service/gcloud/`: Contains specialized gcloud CLI automation components

**Data Layer:**
- `dao/gcp_account.go`: GCP account database operations with complex status tracking
- `dao/vm_instance.go`: VM instance database operations
- Database models support account-project relationships, billing status, token status, and VM associations

**Infrastructure:**
- `base/zlog/`: Structured logging with request ID tracking and caller skip configuration
- `base/config/`: GCP configuration management including SSH keys and project settings
- `handlers/`: HTTP API endpoints for VM and account management

### Key Data Models

**GCPAccount**: Tracks account email, project ID, billing status, token status, VM association, and authentication status. Uses composite unique index on email+project for multi-project per account support.

**VMInstance**: Manages VM lifecycle with SSH connectivity, SOCKS5 proxy configuration, and GCP integration verification.

### VM Integration Pattern

The system implements intelligent VM reuse:
- `ForceCreateVm` flag controls VM creation strategy (default: false for smart reuse)
- VM validation includes both database status and real GCP existence verification via gcloud CLI
- Automatic cleanup of invalid VM associations and account data synchronization
- New VMs are created with SOCKS5 proxy and gcloud CLI via initialization script

### Authentication Workflow

The account registration follows a sophisticated multi-step process:
1. VM selection/creation with validation
2. gcloud auth login session management with retry logic
3. Account status verification with multiple authentication states
4. Project creation and billing attachment automation
5. API token generation and storage

## Development Commands

### Building and Running

**Local Development:**
```bash
# Setup development environment
make dev-setup

# Build the application
make build

# Run locally
make dev

# Run with auto-reload (requires 'air' tool)
make dev-watch

# Run tests
make test
```

**Direct Go commands:**
```bash
go build -o gatc .
go run .
go mod tidy
```

### Configuration Requirements

The application expects configuration files in `./conf/`:
- `conf.yaml`: Application settings (port: 5401)
- `resource.yaml`: Resource configuration
- `gcp/sa-key0.json`: Service account key for the white account
- `gcp/gatc_rsa` and `gcp/gatc_rsa.pub`: SSH key pair for VM access

Environment detection through `env.DevLocalEnv` loads dev-specific configs from `./conf/dev/`.

### Database Operations

The application uses GORM with MySQL for data persistence. Database tables are auto-migrated on startup:
- `gcp_accounts`: Account and project tracking
- `vm_instances`: VM lifecycle management

Key status constants are defined in `dao/gcp_account.go` for billing, token, and authentication states.

### VM Management

VMs are created with standardized configuration:
- Zone: `us-central1-a`
- Machine type: `e2-small`
- Initialization script: `./scripts/vm_init.sh` (installs gcloud CLI and SOCKS5 proxy)
- SSH access via generated key pairs
- SOCKS5 proxy on port 1080

### API Endpoints

**VM Management (`/api/v1/vm/`):**
- `POST /create`: Create new VM instance
- `POST /delete`: Delete VM instance  
- `GET /list`: List VMs with pagination
- `GET /get`: Get specific VM details
- `POST /refresh-ip`: Update VM external IP

**Account Management (`/api/v1/account/`):**
- `GET /start-registration`: Initiate account registration flow
- `GET /submit-auth-key`: Complete authentication with user-provided key
- `GET /list`: List accounts with filtering
- `GET /process-projects-v2`: Execute project processing workflow
- `POST|GET /set-token-invalid`: Mark tokens as invalid
- `GET /emails-with-unbound-projects`: Get emails needing billing setup

### Logging

The system uses structured logging via zap with:
- Request ID correlation across service calls
- Context-aware logging methods (`InfoWithCtx`, `ErrorWithCtx`)
- Caller information properly configured with skip levels

### gcloud CLI Integration

The `service/gcloud/` package provides sophisticated automation:
- SSH-based command execution on remote VMs
- Authentication session management with state tracking
- Project creation and billing automation
- API token extraction and validation

Critical workflow components:
- `WorkCtx`: Encapsulates session, email, VM instance, and Gin context
- Account status detection: `active`, `inactive`, `not_login`
- Retry mechanisms for VM initialization delays
- Session caching for multi-step authentication flows

## Containerization

### Docker Setup
The application is containerized with multi-stage builds:
- **Builder stage**: Uses `golang:1.22-alpine` for compilation
- **Runtime stage**: Uses `debian:12-slim` with gcloud CLI pre-installed
- **Security**: Runs as non-root user with proper file permissions
- **Health checks**: Built-in health monitoring on port 5401

### CI/CD Pipeline
GitHub Actions workflow includes:
- **Testing**: Go vet, unit tests, and build verification
- **Security**: Trivy vulnerability scanning with SARIF upload
- **Container Registry**: Automatic image builds pushed to GitHub Container Registry
- **Multi-platform**: Supports both linux/amd64 and linux/arm64
- **Environment Deployment**: Separate staging and production deployment jobs

### Deployment Options
- **Local development**: Direct Go execution with local configuration
- **Production deployment**: Docker containers with volume-mounted configuration
- **Kubernetes ready**: Health checks and proper signal handling

### Production Configuration
Host directory structure for production deployment:
```
/opt/gatc/
├── conf/                    # Mounted to /app/conf in container
│   ├── conf.yaml           # Application settings
│   ├── resource.yaml       # Database and resource config  
│   ├── gcp/                # GCP credentials (permissions 700)
│   │   ├── sa-key0.json    # Service account key
│   │   ├── gatc_rsa        # SSH private key (permissions 600)
│   │   └── gatc_rsa.pub    # SSH public key
│   └── dev/                # Development overrides
└── mysql/                   # MySQL data directory
```

### Deployment Commands
```bash
# Initial setup
make setup-host              # Creates /opt/gatc structure
make deploy-prod             # Deploy with docker-compose.prod.yml

# Operations  
make prod-logs               # View logs
make prod-restart            # Restart application
make prod-stop               # Stop all services
```

### Health Monitoring
- **Application**: `GET /health` endpoint returns service status
- **Container**: Built-in Docker health checks
- **Database**: Connection validation through application