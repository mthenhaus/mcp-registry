package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// SQSListener handles receiving and processing messages from SQS
type SQSListener struct {
	client          *sqs.Client
	queueURL        string
	s3Downloader    *S3Downloader
	targetFilePath  string
	reloadCallback  func() error
	stopChan        chan struct{}
	maxMessages     int32
	waitTimeSeconds int32
}

// SQSMessage represents the expected structure of messages from SQS
type SQSMessage struct {
	S3URI string `json:"s3_uri"` // S3 URI to download from (e.g., "s3://bucket/path/to/file.json")
}

// SQSListenerConfig holds configuration for the SQS listener
type SQSListenerConfig struct {
	QueueURL        string       // SQS queue URL
	TargetFilePath  string       // Local file path to write downloaded S3 file
	ReloadCallback  func() error // Function to call after file is updated
	MaxMessages     int32        // Maximum number of messages to retrieve per request (1-10)
	WaitTimeSeconds int32        // Long polling wait time in seconds (0-20)
}

// NewSQSListener creates a new SQS listener
func NewSQSListener(ctx context.Context, cfg SQSListenerConfig) (*SQSListener, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Downloader, err := NewS3Downloader(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 downloader: %w", err)
	}

	// Set defaults
	maxMessages := cfg.MaxMessages
	if maxMessages == 0 {
		maxMessages = 1
	}
	if maxMessages > 10 {
		maxMessages = 10
	}

	waitTimeSeconds := cfg.WaitTimeSeconds
	if waitTimeSeconds == 0 {
		waitTimeSeconds = 20 // Enable long polling by default
	}
	if waitTimeSeconds > 20 {
		waitTimeSeconds = 20
	}

	return &SQSListener{
		client:          sqs.NewFromConfig(awsCfg),
		queueURL:        cfg.QueueURL,
		s3Downloader:    s3Downloader,
		targetFilePath:  cfg.TargetFilePath,
		reloadCallback:  cfg.ReloadCallback,
		stopChan:        make(chan struct{}),
		maxMessages:     maxMessages,
		waitTimeSeconds: waitTimeSeconds,
	}, nil
}

// Start begins listening for messages from SQS in a goroutine
func (l *SQSListener) Start(ctx context.Context) {
	log.Printf("Starting SQS listener for queue: %s", l.queueURL)

	go l.pollMessages(ctx)
}

// Stop stops the SQS listener
func (l *SQSListener) Stop() {
	log.Println("Stopping SQS listener...")
	close(l.stopChan)
}

// pollMessages continuously polls for messages from SQS
func (l *SQSListener) pollMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("SQS listener context cancelled")
			return
		case <-l.stopChan:
			log.Println("SQS listener stopped")
			return
		default:
			// Poll for messages
			if err := l.receiveAndProcessMessages(ctx); err != nil {
				log.Printf("Error processing SQS messages: %v", err)
				// Wait before retrying
				time.Sleep(5 * time.Second)
			}
		}
	}
}

// receiveAndProcessMessages receives and processes messages from SQS
func (l *SQSListener) receiveAndProcessMessages(ctx context.Context) error {
	result, err := l.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(l.queueURL),
		MaxNumberOfMessages: l.maxMessages,
		WaitTimeSeconds:     l.waitTimeSeconds,
		MessageAttributeNames: []string{
			string(types.QueueAttributeNameAll),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to receive messages: %w", err)
	}

	// Process each message
	for _, msg := range result.Messages {
		if err := l.processMessage(ctx, msg); err != nil {
			log.Printf("Error processing message: %v", err)
			// Continue processing other messages even if one fails
			continue
		}

		// Delete the message after successful processing
		if err := l.deleteMessage(ctx, msg.ReceiptHandle); err != nil {
			log.Printf("Error deleting message: %v", err)
		}
	}

	return nil
}

// processMessage processes a single SQS message
func (l *SQSListener) processMessage(ctx context.Context, msg types.Message) error {
	log.Printf("Received SQS message: %s", aws.ToString(msg.MessageId))

	// Parse the message body
	var sqsMsg SQSMessage
	if err := json.Unmarshal([]byte(aws.ToString(msg.Body)), &sqsMsg); err != nil {
		return fmt.Errorf("failed to parse message body: %w", err)
	}

	if sqsMsg.S3URI == "" {
		return fmt.Errorf("message missing s3_uri field")
	}

	// Parse S3 URI
	bucket, key, err := ParseS3URI(sqsMsg.S3URI)
	if err != nil {
		return fmt.Errorf("invalid S3 URI: %w", err)
	}

	// Download the file from S3
	if err := l.s3Downloader.DownloadFile(ctx, bucket, key, l.targetFilePath); err != nil {
		return fmt.Errorf("failed to download file from S3: %w", err)
	}

	log.Printf("Successfully downloaded file from %s to %s", sqsMsg.S3URI, l.targetFilePath)

	// Call the reload callback to reload the database
	if l.reloadCallback != nil {
		log.Println("Reloading database from updated file...")
		if err := l.reloadCallback(); err != nil {
			return fmt.Errorf("failed to reload database: %w", err)
		}
		log.Println("Database reloaded successfully")
	}

	return nil
}

// deleteMessage deletes a message from the queue
func (l *SQSListener) deleteMessage(ctx context.Context, receiptHandle *string) error {
	_, err := l.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(l.queueURL),
		ReceiptHandle: receiptHandle,
	})
	if err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}

	log.Printf("Deleted message from queue")
	return nil
}
