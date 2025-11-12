# AWS SQS/S3 Integration for Registry Database Updates

This document describes how to configure the MCP Registry to automatically update its JSON file database from AWS S3 when triggered by SQS messages.

## Overview

When using the JSON file database (`DATABASE_TYPE=jsonfile`), the registry can listen to an AWS SQS queue for messages that trigger downloading an updated registry file from S3. This enables a publish-subscribe pattern where:

1. An updated `registry.json` file is uploaded to S3
2. A message is sent to an SQS queue with the S3 location
3. The registry application receives the message, downloads the file, and reloads the database

This is useful for:
- Synchronizing multiple registry instances with a centralized S3-stored registry
- Implementing CI/CD pipelines that update the registry data
- Enabling external systems to trigger registry updates

## Configuration

### Environment Variables

Add the following environment variables to enable SQS integration:

```bash
# Enable SQS listener
MCP_REGISTRY_SQS_ENABLED=true

# SQS queue URL
MCP_REGISTRY_SQS_QUEUE_URL=https://sqs.us-east-1.amazonaws.com/123456789012/mcp-registry-updates

# Ensure JSON file database is being used
MCP_REGISTRY_DATABASE_TYPE=jsonfile
MCP_REGISTRY_JSON_FILE_PATH=data/registry.json
```

### AWS Credentials

The registry uses the AWS SDK's default credential chain, which looks for credentials in this order:

1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
2. Shared credentials file (`~/.aws/credentials`)
3. IAM role for ECS tasks
4. IAM role for EC2 instances

**Required IAM Permissions:**

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:GetQueueAttributes"
      ],
      "Resource": "arn:aws:sqs:REGION:ACCOUNT_ID:QUEUE_NAME"
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject"
      ],
      "Resource": "arn:aws:s3:::BUCKET_NAME/*"
    }
  ]
}
```

## SQS Message Format

Messages sent to the SQS queue must be JSON with the following structure:

```json
{
  "s3_url": "https://bucket.s3.region.amazonaws.com/path/to/registry.json"
}
```

The `s3_url` field should contain an S3 Object URL in one of the following formats:

- **Virtual-hosted-style with region**: `https://bucket.s3.region.amazonaws.com/key`
- **Virtual-hosted-style (us-east-1)**: `https://bucket.s3.amazonaws.com/key`
- **Path-style with region**: `https://s3.region.amazonaws.com/bucket/key`
- **Path-style (us-east-1)**: `https://s3.amazonaws.com/bucket/key`

### Example Messages

```json
{
  "s3_url": "https://mthenhaus-mcp-registry.s3.us-east-1.amazonaws.com/registry.json"
}
```

```json
{
  "s3_url": "https://mcp-registry-data.s3.amazonaws.com/production/registry.json"
}
```

```json
{
  "s3_url": "https://s3.us-west-2.amazonaws.com/my-bucket/path/to/registry.json"
}
```

## Workflow

1. **Registry Startup**: When the registry starts with SQS enabled, it initializes an SQS listener that polls the configured queue
2. **Message Reception**: The listener uses long polling (20 seconds) to efficiently wait for messages
3. **File Download**: When a message is received:
   - The S3 Object URL is parsed to extract bucket and key
   - The file is downloaded from S3 to a temporary location
   - The temporary file atomically replaces the target file
4. **Database Reload**: After the file is successfully downloaded, the JSON database reloads its data
5. **Message Deletion**: The SQS message is deleted after successful processing
6. **Error Handling**: If any step fails, the message remains in the queue for retry

## Example Setup

### 1. Create S3 Bucket

```bash
aws s3 mb s3://mcp-registry-data
```

### 2. Create SQS Queue

```bash
aws sqs create-queue --queue-name mcp-registry-updates
```

### 3. Upload Registry File to S3

```bash
aws s3 cp data/registry.json s3://mcp-registry-data/registry.json
```

### 4. Send SQS Message

```bash
aws sqs send-message \
  --queue-url https://sqs.us-east-1.amazonaws.com/123456789012/mcp-registry-updates \
  --message-body '{"s3_url": "https://mcp-registry-data.s3.us-east-1.amazonaws.com/registry.json"}'
```

### 5. Start Registry with SQS Enabled

```bash
export MCP_REGISTRY_SQS_ENABLED=true
export MCP_REGISTRY_SQS_QUEUE_URL=https://sqs.us-east-1.amazonaws.com/123456789012/mcp-registry-updates
export MCP_REGISTRY_DATABASE_TYPE=jsonfile
export MCP_REGISTRY_JSON_FILE_PATH=data/registry.json

./bin/registry
```

## Testing

To test the integration:

1. Start the registry with SQS enabled
2. Upload a modified `registry.json` to S3
3. Send an SQS message with the S3 Object URL
4. Check the logs for confirmation:
   ```
   Received SQS message: <message-id>
   Downloading s3://bucket/key to data/registry.json
   Successfully downloaded <bytes> bytes from S3
   Reloading database from updated file...
   Database reloaded successfully
   Deleted message from queue
   ```

## Docker Compose Example

```yaml
version: '3.8'

services:
  registry:
    image: ghcr.io/modelcontextprotocol/registry:latest
    environment:
      - MCP_REGISTRY_DATABASE_TYPE=jsonfile
      - MCP_REGISTRY_JSON_FILE_PATH=/data/registry.json
      - MCP_REGISTRY_SQS_ENABLED=true
      - MCP_REGISTRY_SQS_QUEUE_URL=https://sqs.us-east-1.amazonaws.com/123456789012/mcp-registry-updates
      - AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}
      - AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}
      - AWS_REGION=us-east-1
    volumes:
      - ./data:/data
    ports:
      - "8080:8080"
```

## Limitations

- SQS integration only works with the JSON file database (`DATABASE_TYPE=jsonfile`)
- PostgreSQL databases do not support this feature
- The registry must have write permissions to the `JSON_FILE_PATH` location
- Message processing is sequential (one at a time)
- Failed downloads will cause messages to be retried based on the SQS queue's redrive policy

## Monitoring

The registry logs all SQS operations:

- Message reception
- File downloads from S3
- Database reloads
- Errors and retries

Use CloudWatch or your preferred logging solution to monitor these events.

## Security Considerations

1. **IAM Policies**: Use least-privilege IAM policies that only grant access to specific S3 buckets and SQS queues
2. **Encryption**: Enable encryption at rest for S3 buckets and SQS queues
3. **VPC Endpoints**: Use VPC endpoints for S3 and SQS to avoid internet traffic
4. **Message Validation**: The registry validates S3 URIs but does not validate the content of downloaded files
5. **File Permissions**: The downloaded file inherits permissions from the parent directory

## Troubleshooting

### Registry Not Receiving Messages

- Verify SQS queue URL is correct
- Check AWS credentials have the required permissions
- Ensure `SQS_ENABLED=true` is set
- Check the queue has messages using AWS Console or CLI

### S3 Download Failures

- Verify the S3 URI format is correct (`s3://bucket/key`)
- Check IAM permissions for S3 GetObject
- Ensure the S3 bucket and object exist
- Verify network connectivity to S3

### Database Not Reloading

- Check file write permissions on `JSON_FILE_PATH`
- Verify the downloaded file is valid JSON
- Review application logs for reload errors

### Messages Not Being Deleted

- Messages remain in the queue if processing fails
- Check for errors in the logs
- Verify the queue's visibility timeout is appropriate
- Consider setting up a dead-letter queue for failed messages
