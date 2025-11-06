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

// ParseS3URI parses an S3 URI (s3://bucket/key) into bucket and key components
func ParseS3URI(uri string) (bucket, key string, err error) {
	if len(uri) < 5 || uri[:5] != "s3://" {
		return "", "", fmt.Errorf("invalid S3 URI: must start with s3://")
	}

	path := uri[5:]
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			return path[:i], path[i+1:], nil
		}
	}

	return "", "", fmt.Errorf("invalid S3 URI: missing key path")
}
