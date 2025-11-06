# AWS SQS/S3 Integration - Implementation Summary

## Overview

This implementation adds AWS SQS and S3 integration to the MCP Registry, enabling automatic updates of the JSON file database when triggered by SQS messages. This allows external systems to publish updated registry files to S3 and notify the registry to reload the data.

## Changes Made

### 1. Dependencies Added

**File: `go.mod`**
- Added AWS SDK v2 packages:
  - `github.com/aws/aws-sdk-go-v2/config` - AWS configuration
  - `github.com/aws/aws-sdk-go-v2/service/s3` - S3 client
  - `github.com/aws/aws-sdk-go-v2/service/sqs` - SQS client

### 2. New AWS Package

**Created: `internal/aws/s3.go`**
- `S3Downloader` struct for downloading files from S3
- `NewS3Downloader()` - Creates S3 downloader with default AWS config
- `DownloadFile()` - Downloads S3 objects to local files with atomic writes
- `ParseS3URI()` - Parses S3 URIs (s3://bucket/key) into components

**Created: `internal/aws/sqs.go`**
- `SQSListener` struct for receiving and processing SQS messages
- `SQSListenerConfig` for configuration options
- `SQSMessage` struct defining expected message format
- `NewSQSListener()` - Creates SQS listener with configuration
- `Start()` - Starts listening for messages in a goroutine
- `Stop()` - Stops the listener gracefully
- Message processing pipeline:
  1. Long polling for messages (configurable wait time)
  2. Parse message body for S3 URI
  3. Download file from S3
  4. Reload database
  5. Delete message from queue

**Created: `internal/aws/s3_test.go`**
- Unit tests for `ParseS3URI()` function
- Tests for valid and invalid S3 URI formats
- Edge case coverage

### 3. Database Reload Functionality

**Modified: `internal/database/jsonfile.go`**
- Added `Reload()` method to `JSONFileDB` struct
- Thread-safe reload of data from file
- Allows hot-reloading without restarting the application

### 4. Configuration

**Modified: `internal/config/config.go`**
- Added `SQSEnabled` - Enable/disable SQS listener
- Added `SQSQueueURL` - SQS queue URL to listen to

**Modified: `.env.example`**
- Documented new environment variables:
  - `MCP_REGISTRY_SQS_ENABLED` (default: false)
  - `MCP_REGISTRY_SQS_QUEUE_URL` (example provided)
- Includes usage instructions and message format

### 5. Main Application Integration

**Modified: `cmd/registry/main.go`**
- Added AWS package import
- Initialized `jsonDB` variable to track JSON database instance
- Initialized `sqsListener` variable for SQS listener
- Added SQS listener initialization after database setup:
  - Only enabled when `SQSEnabled=true` and `DatabaseType=jsonfile`
  - Creates listener with reload callback
  - Starts listener in background goroutine
- Added graceful shutdown for SQS listener on application exit

### 6. Documentation

**Created: `docs/aws-sqs-s3-integration.md`**
Comprehensive documentation including:
- Overview and use cases
- Configuration instructions
- IAM permissions required
- SQS message format specification
- Step-by-step setup guide
- Docker Compose example
- Testing instructions
- Troubleshooting guide
- Security considerations
- Monitoring recommendations

**Created: `AWS_INTEGRATION_SUMMARY.md`** (this file)
- Summary of all changes made
- Component descriptions
- File listing

## Architecture

```
┌─────────────────┐
│  External System│
└────────┬────────┘
         │ 1. Upload registry.json
         ↓
    ┌────────┐
    │   S3   │
    └────────┘
         │ 2. Send message with S3 URI
         ↓
    ┌────────┐
    │  SQS   │
    └────┬───┘
         │ 3. Poll for messages
         ↓
┌──────────────────────────┐
│   MCP Registry           │
│                          │
│  ┌────────────────────┐  │
│  │  SQS Listener      │  │
│  │  - Receive msg     │  │
│  │  - Download S3     │  │
│  │  - Reload DB       │  │
│  │  - Delete msg      │  │
│  └────────┬───────────┘  │
│           │              │
│  ┌────────▼───────────┐  │
│  │  JSON File DB      │  │
│  │  - Load data       │  │
│  │  - Serve requests  │  │
│  └────────────────────┘  │
└──────────────────────────┘
```

## Message Flow

1. **External System**: Uploads updated `registry.json` to S3
2. **External System**: Sends SQS message: `{"s3_uri": "s3://bucket/key"}`
3. **SQS Listener**: Receives message via long polling
4. **SQS Listener**: Parses S3 URI from message body
5. **S3 Downloader**: Downloads file from S3 to temporary location
6. **S3 Downloader**: Atomically replaces target file
7. **Database**: Reloads data from updated file
8. **SQS Listener**: Deletes message from queue
9. **Registry**: Serves updated data to clients

## Security Features

- **IAM-based Authentication**: Uses AWS SDK default credential chain
- **Atomic File Operations**: Temporary files prevent partial reads
- **Thread-safe Reload**: Database reload is protected with mutex
- **Graceful Shutdown**: SQS listener stops cleanly on application exit
- **Error Handling**: Failed messages remain in queue for retry
- **Validation**: S3 URIs are validated before processing

## Configuration Example

```bash
# Enable SQS integration
export MCP_REGISTRY_SQS_ENABLED=true
export MCP_REGISTRY_SQS_QUEUE_URL=https://sqs.us-east-1.amazonaws.com/123456789012/mcp-registry-updates

# JSON file database (required for SQS)
export MCP_REGISTRY_DATABASE_TYPE=jsonfile
export MCP_REGISTRY_JSON_FILE_PATH=data/registry.json

# AWS credentials (if not using IAM roles)
export AWS_ACCESS_KEY_ID=your-access-key
export AWS_SECRET_ACCESS_KEY=your-secret-key
export AWS_REGION=us-east-1
```

## Testing

Unit tests verify:
- S3 URI parsing with various formats
- Error handling for invalid URIs
- Edge cases (empty strings, missing components)

Integration testing requires:
- AWS account with S3 and SQS
- Appropriate IAM permissions
- Running registry instance

## Limitations

- Only works with JSON file database (`DATABASE_TYPE=jsonfile`)
- PostgreSQL databases do not support this feature
- Sequential message processing (one at a time)
- No built-in file validation (assumes valid JSON)

## Future Enhancements

Potential improvements:
- S3 event notifications instead of SQS polling
- File content validation before reload
- Metrics/monitoring integration
- Batch message processing
- Support for other storage backends
- Automatic retry configuration

## Files Modified/Created

### New Files
- `internal/aws/s3.go` - S3 download functionality
- `internal/aws/sqs.go` - SQS listener implementation
- `internal/aws/s3_test.go` - Unit tests
- `docs/aws-sqs-s3-integration.md` - User documentation
- `AWS_INTEGRATION_SUMMARY.md` - Implementation summary

### Modified Files
- `go.mod` - Added AWS SDK dependencies
- `internal/config/config.go` - Added SQS configuration
- `internal/database/jsonfile.go` - Added Reload() method
- `cmd/registry/main.go` - Integrated SQS listener
- `.env.example` - Documented new environment variables

## Compatibility

- **Go Version**: 1.24.9+
- **AWS SDK**: v2 (latest)
- **Database Types**: JSON file only
- **Platforms**: Linux, macOS, Windows
- **Container**: Docker/Kubernetes compatible
