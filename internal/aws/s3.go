package aws

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Downloader handles downloading files from S3
type S3Downloader struct {
	client *s3.Client
}

// NewS3Downloader creates a new S3 downloader with default AWS config
func NewS3Downloader(ctx context.Context) (*S3Downloader, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &S3Downloader{
		client: s3.NewFromConfig(cfg),
	}, nil
}

// DownloadFile downloads a file from S3 to a local path
// bucket: S3 bucket name
// key: S3 object key (path within bucket)
// localPath: local file path to write to
func (d *S3Downloader) DownloadFile(ctx context.Context, bucket, key, localPath string) error {
	log.Printf("Downloading s3://%s/%s to %s", bucket, key, localPath)

	// Get the object from S3
	result, err := d.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to get object from S3: %w", err)
	}
	defer result.Body.Close()

	// Ensure the directory exists
	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create temporary file
	tempFile := localPath + ".tmp"
	outFile, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer outFile.Close()

	// Copy the S3 object to the file
	written, err := io.Copy(outFile, result.Body)
	if err != nil {
		os.Remove(tempFile) // Clean up temp file on error
		return fmt.Errorf("failed to write file: %w", err)
	}

	// Close the file before renaming
	if err := outFile.Close(); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	// Atomically replace the target file
	if err := os.Rename(tempFile, localPath); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	log.Printf("Successfully downloaded %d bytes from S3", written)
	return nil
}

// ParseS3URL parses an S3 Object URL or S3 URI into bucket and key components
// Supports multiple URL formats:
// - S3 URI: s3://bucket/key (for backward compatibility)
// - Virtual-hosted-style: https://bucket.s3.region.amazonaws.com/key
// - Virtual-hosted-style (us-east-1): https://bucket.s3.amazonaws.com/key
// - Path-style: https://s3.region.amazonaws.com/bucket/key
// - Path-style (us-east-1): https://s3.amazonaws.com/bucket/key
func ParseS3URL(url string) (bucket, key string, err error) {
	// Support legacy S3 URI format (s3://bucket/key) for backward compatibility
	if len(url) >= 5 && url[:5] == "s3://" {
		return parseS3URI(url[5:])
	}

	// Parse HTTPS S3 Object URLs
	if len(url) >= 8 && url[:8] == "https://" {
		return parseHTTPSS3URL(url[8:])
	}

	return "", "", fmt.Errorf("invalid S3 URL: must start with s3:// or https://")
}

// parseS3URI parses the path portion of an S3 URI (s3://bucket/key)
func parseS3URI(path string) (bucket, key string, err error) {
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			return path[:i], path[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid S3 URI: missing key path")
}

// parseHTTPSS3URL parses an HTTPS S3 Object URL
func parseHTTPSS3URL(remaining string) (bucket, key string, err error) {
	// Find the first slash to separate host from path
	slashIdx := -1
	for i := 0; i < len(remaining); i++ {
		if remaining[i] == '/' {
			slashIdx = i
			break
		}
	}

	if slashIdx == -1 {
		return "", "", fmt.Errorf("invalid S3 URL: missing object key")
	}

	host := remaining[:slashIdx]
	path := remaining[slashIdx+1:]

	if path == "" {
		return "", "", fmt.Errorf("invalid S3 URL: missing object key")
	}

	// Try virtual-hosted-style first
	if bucket, key, ok := parseVirtualHostedStyle(host, path); ok {
		return bucket, key, nil
	}

	// Try path-style
	if bucket, key, ok := parsePathStyle(host, path); ok {
		return bucket, key, nil
	}

	return "", "", fmt.Errorf("invalid S3 URL: unrecognized S3 URL format")
}

// parseVirtualHostedStyle attempts to parse virtual-hosted-style URLs
func parseVirtualHostedStyle(host, path string) (bucket, key string, ok bool) {
	// Check for us-east-1 format: bucket.s3.amazonaws.com
	if len(host) > 17 && host[len(host)-17:] == ".s3.amazonaws.com" {
		return host[:len(host)-17], path, true
	}

	// Check for regional format: bucket.s3.region.amazonaws.com
	if dotS3Idx := findSubstring(host, ".s3."); dotS3Idx != -1 {
		if len(host) > 14 && host[len(host)-14:] == ".amazonaws.com" {
			return host[:dotS3Idx], path, true
		}
	}

	return "", "", false
}

// parsePathStyle attempts to parse path-style URLs
func parsePathStyle(host, path string) (bucket, key string, ok bool) {
	// Check for us-east-1 format: s3.amazonaws.com/bucket/key
	if host == "s3.amazonaws.com" {
		return extractBucketAndKey(path)
	}

	// Check for regional format: s3.region.amazonaws.com/bucket/key
	if len(host) >= 17 && host[:3] == "s3." && host[len(host)-14:] == ".amazonaws.com" {
		return extractBucketAndKey(path)
	}

	return "", "", false
}

// extractBucketAndKey splits path into bucket and key
func extractBucketAndKey(path string) (bucket, key string, ok bool) {
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			bucket = path[:i]
			key = path[i+1:]
			if key == "" {
				return "", "", false
			}
			return bucket, key, true
		}
	}
	return "", "", false
}

// findSubstring finds the first occurrence of substr in s and returns its index, or -1 if not found
func findSubstring(s, substr string) int {
	if len(substr) > len(s) {
		return -1
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
