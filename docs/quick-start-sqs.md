# Quick Start: AWS SQS Integration

A 5-minute guide to enable automatic registry updates from S3 via SQS.

## Prerequisites

- AWS account with S3 and SQS access
- AWS credentials configured (environment variables, ~/.aws/credentials, or IAM role)
- MCP Registry with JSON file database

## Quick Setup

### 1. Create S3 Bucket

```bash
aws s3 mb s3://my-registry-data
```

### 2. Create SQS Queue

```bash
aws sqs create-queue --queue-name registry-updates
```

Note the queue URL from the output.

### 3. Configure Registry

```bash
export MCP_REGISTRY_DATABASE_TYPE=jsonfile
export MCP_REGISTRY_JSON_FILE_PATH=data/registry.json
export MCP_REGISTRY_SQS_ENABLED=true
export MCP_REGISTRY_SQS_QUEUE_URL=https://sqs.REGION.amazonaws.com/ACCOUNT/registry-updates
```

### 4. Start Registry

```bash
./bin/registry
```

You should see:
```
Using JSON file database at data/registry.json
Initializing SQS listener for queue: https://sqs...
SQS listener started successfully
```

### 5. Trigger Update

Upload file to S3:
```bash
aws s3 cp data/registry.json s3://my-registry-data/registry.json
```

Send SQS message:
```bash
aws sqs send-message \
  --queue-url https://sqs.REGION.amazonaws.com/ACCOUNT/registry-updates \
  --message-body '{"s3_uri": "s3://my-registry-data/registry.json"}'
```

Check logs:
```
Received SQS message: abc-123
Downloading s3://my-registry-data/registry.json to data/registry.json
Successfully downloaded 12345 bytes from S3
Reloading database from updated file...
Database reloaded successfully
Deleted message from queue
```

## IAM Permissions

Minimum required permissions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage"
      ],
      "Resource": "arn:aws:sqs:REGION:ACCOUNT:registry-updates"
    },
    {
      "Effect": "Allow",
      "Action": "s3:GetObject",
      "Resource": "arn:aws:s3:::my-registry-data/*"
    }
  ]
}
```

## Docker Compose

```yaml
version: '3.8'
services:
  registry:
    build: .
    environment:
      - MCP_REGISTRY_DATABASE_TYPE=jsonfile
      - MCP_REGISTRY_SQS_ENABLED=true
      - MCP_REGISTRY_SQS_QUEUE_URL=${SQS_QUEUE_URL}
      - AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}
      - AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}
      - AWS_REGION=${AWS_REGION}
    volumes:
      - ./data:/data
    ports:
      - "8080:8080"
```

## Troubleshooting

**Registry not receiving messages?**
- Verify queue URL is correct
- Check AWS credentials have permissions
- Ensure `SQS_ENABLED=true`

**S3 download failing?**
- Verify S3 URI format: `s3://bucket/key`
- Check IAM permissions for S3
- Ensure bucket and object exist

**Database not reloading?**
- Check file permissions on `JSON_FILE_PATH`
- Verify JSON file is valid
- Review logs for errors

## Next Steps

- See [aws-sqs-s3-integration.md](./aws-sqs-s3-integration.md) for detailed documentation
- Set up CloudWatch monitoring
- Configure dead-letter queue for failed messages
- Implement automated CI/CD pipeline
