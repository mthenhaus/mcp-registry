# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Essential Commands

### Building
- `make build` - Build the registry application with version info
- `make publisher` - Build the publisher CLI tool (`bin/mcp-publisher`)
- `ko build --local --base-import-paths --sbom=none ./cmd/registry` - Build container image with ko

### Development
- `make dev-compose` (or `make dev-up`) - Start full development environment (builds with ko and starts Docker Compose)
- `make dev-down` - Stop development environment
- `make ko-rebuild` - Rebuild image with ko and restart registry container
- Registry runs at `localhost:8080` by default

### Testing
- `make test` - Run unit tests only
- `make test-unit` - Run unit tests with coverage (requires PostgreSQL via Docker)
- `make test-integration` - Run integration tests
- `make test-all` - Run both unit and integration tests
- `make test-endpoints` - Test API endpoints (requires running server)
- `make test-publish` - Test publish endpoint (requires BEARER_TOKEN env var)

### Code Quality
- `make lint` - Run golangci-lint (timeout 5m)
- `make lint-fix` - Run linter with auto-fix
- `make validate` - Run all validation checks (schemas + examples)
- `make validate-schemas` - Validate JSON schemas
- `make validate-examples` - Validate examples against schemas
- `make check` - Run all checks (lint, validate, test-all) and ensure dev environment is down

### Schema Management
- `make generate-schema` - Generate server.schema.json from openapi.yaml
- `make check-schema` - Verify server.schema.json is in sync with openapi.yaml

## Architecture Overview

### High-Level Structure

The MCP Registry is a **Go-based HTTP API** that serves as an app store for MCP (Model Context Protocol) servers. It provides:
- **REST API** for browsing and publishing servers
- **Multiple authentication methods** (GitHub OAuth, GitHub OIDC, DNS/HTTP verification)
- **Dual database support** (JSON file or PostgreSQL)
- **Package registry validation** (npm, PyPI, NuGet, OCI, MCPB)
- **AWS integration** (optional S3/SQS for JSON file sync)

### Core Components

**Entry Points** (`cmd/`):
- `cmd/registry/main.go` - Main server application with graceful shutdown
- `cmd/publisher/` - CLI tool for publishing servers (`mcp-publisher`)

**API Layer** (`internal/api/`):
- `server.go` - HTTP server setup with CORS and middleware
- `router/` - Huma v2-based API routing
- `handlers/v0/` - API v0 handlers (servers, publish, edit, auth, health)
  - `auth/` - Authentication handlers (GitHub OAuth, GitHub OIDC, DNS, HTTP)

**Business Logic** (`internal/service/`):
- `registry_service.go` - Core registry operations (CRUD for servers)
- `versioning.go` - Semantic versioning logic
- Abstracts database operations and enforces business rules

**Data Layer** (`internal/database/`):
- `database.go` - Database interface
- `postgres.go` - PostgreSQL implementation
- `jsonfile.go` - JSON file-based implementation with `Reload()` for hot-reloading
- `migrate.go` - Database migrations

**Authentication** (`internal/auth/`):
- `jwt.go` - JWT token generation and validation (Ed25519)
- `types.go` - Authentication types and namespace permissions
- `blocks.go` - Namespace blocking functionality

**Validation** (`internal/validators/`):
- `validators.go` - Server.json validation against JSON schema
- `registries/` - Package registry validators (npm, PyPI, NuGet, OCI, MCPB)
- Validates that packages exist before accepting server submissions

**AWS Integration** (`internal/aws/`):
- `s3.go` - S3 file downloader with atomic writes
- `sqs.go` - SQS listener for triggering JSON file reloads
- Only works with JSON file database (not PostgreSQL)

**Configuration** (`internal/config/`):
- `config.go` - Environment-based configuration with `MCP_REGISTRY_` prefix
- Uses `github.com/caarlos0/env/v11` for parsing

**Telemetry** (`internal/telemetry/`):
- `metrics.go` - Prometheus metrics via OpenTelemetry

**Public Packages** (`pkg/`):
- `api/v0/` - API v0 types for external consumers
- `model/` - Data models for server.json format

**Importer** (`internal/importer/`):
- `importer.go` - Imports seed data from files or URLs (HTTP/S3)

### Key Architectural Patterns

1. **Interface-Based Database Abstraction**: `Database` interface allows switching between PostgreSQL and JSON file storage
2. **Service Layer Pattern**: `RegistryService` separates business logic from HTTP handlers
3. **Environment-Based Configuration**: All config via `MCP_REGISTRY_*` environment variables
4. **Schema-Driven API**: Uses Huma v2 with OpenAPI schema generation
5. **Hot-Reloadable JSON Storage**: JSON file database supports `Reload()` for zero-downtime updates
6. **Build-Time Version Injection**: Version info injected via `-ldflags` during build

### Database Modes

**JSON File** (default):
- Simple file-based storage at `data/registry.json`
- No external dependencies
- Supports hot-reloading via `Reload()` method
- Can sync from S3 via SQS messages
- Suitable for read-heavy workloads

**PostgreSQL**:
- Production-grade relational storage
- Requires PostgreSQL 16+
- Schema migrations in `internal/database/migrate.go`
- Connection pooling via `pgx/v5`

### Authentication Flows

**Publishing requires namespace ownership proof:**
- `io.github.{user}/*` - GitHub OAuth or GitHub Actions OIDC
- `{domain}/*` - DNS TXT record or HTTP challenge
- `io.modelcontextprotocol.anonymous/*` - Anonymous auth (dev only, disabled in prod)

**Admin operations:**
- OIDC authentication for `@modelcontextprotocol.io` accounts
- Configurable edit/publish permissions

### Container Image Building

**Uses ko** (https://ko.build):
- `ko build` creates minimal container images for Go apps
- Images loaded into local Docker daemon for development
- Base image configurable via `KO_DEFAULTBASEIMAGE` (default: static, tests use alpine)
- Configured in `.ko.yaml`

### Seeding Behavior

By default, registry seeds from production API (`https://registry.modelcontextprotocol.io/v0/servers`) with:
- Filtered subset of servers (fast startup)
- Validation enabled

For offline development:
```bash
MCP_REGISTRY_SEED_FROM=data/seed.json MCP_REGISTRY_ENABLE_REGISTRY_VALIDATION=false make dev-compose
```

### AWS Integration (Optional)

When `MCP_REGISTRY_SQS_ENABLED=true` and using JSON file database:
1. External system uploads `registry.json` to S3
2. Sends SQS message: `{"s3_uri": "s3://bucket/path"}`
3. Registry downloads from S3 and calls `jsonDB.Reload()`
4. Zero-downtime updates without restarting

See `AWS_INTEGRATION_SUMMARY.md` and `docs/aws-sqs-s3-integration.md` for details.

## Development Workflow

### Local Setup
1. Install prerequisites: Docker, Go 1.24.x, ko, golangci-lint v2.4.0
2. Configure environment variables (see `.env.example`)
3. Run `make dev-compose` to start services
4. Registry available at `http://localhost:8080`

### Testing Strategy
- **Unit tests**: Test business logic with PostgreSQL via Docker
- **Integration tests**: Full end-to-end tests with Docker Compose
- **Validation tests**: JSON schema and example validation
- **Configuration tests**: Verify Appwrite permissions (legacy, may be removable)

### Publisher CLI
```bash
make publisher
./bin/mcp-publisher --help
./bin/mcp-publisher init     # Create server.json interactively
./bin/mcp-publisher login    # Authenticate
./bin/mcp-publisher publish  # Publish server
```

### Code Quality Requirements
- golangci-lint with extensive linters (see `.golangci.yml`)
- gofmt for formatting
- All linters must pass before merge
- Test coverage tracked in `coverage.html`

## Important Conventions

### Version Information
Build-time injection via ldflags in `Makefile`:
```go
var Version = "dev"
var BuildTime = "unknown"
var GitCommit = "unknown"
```

### Configuration
All environment variables prefixed with `MCP_REGISTRY_`:
- `MCP_REGISTRY_SERVER_ADDRESS` (default: `:8080`)
- `MCP_REGISTRY_DATABASE_TYPE` (`jsonfile` or `postgres`)
- `MCP_REGISTRY_JWT_PRIVATE_KEY` (32-byte Ed25519 seed in hex)
- See `internal/config/config.go` for full list

### API Versioning
- Current API: v0 (frozen, no breaking changes)
- Handlers in `internal/api/handlers/v0/`
- Types in `pkg/api/v0/`

### Common Pitfalls
- Integration tests use Alpine base image (for wget in health checks), production uses static
- SQS integration only works with JSON file database, not PostgreSQL
- GitHub OAuth credentials in `.env.example` are for local dev only (not sensitive)
- Always run `make check` before committing to catch lint/test failures
