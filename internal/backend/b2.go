package backend

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/kurin/blazer/b2"
	"github.com/nsilverman/archivist/internal/models"
)

// B2Backend stores backups on Backblaze B2
type B2Backend struct {
	client *b2.Client
	bucket *b2.Bucket
	prefix string
}

// Initialize sets up the B2 backend
func (b *B2Backend) Initialize(cfg map[string]interface{}, pathResolver PathResolver) error {
	// Extract configuration
	bucketName, ok := cfg["bucket"].(string)
	if !ok || bucketName == "" {
		return fmt.Errorf("B2 backend requires 'bucket' configuration")
	}

	// Optional prefix
	if prefix, ok := cfg["prefix"].(string); ok {
		b.prefix = prefix
	}

	// Get credentials
	keyID, ok := cfg["key_id"].(string)
	if !ok || keyID == "" {
		return fmt.Errorf("B2 backend requires 'key_id' configuration")
	}

	applicationKey, ok := cfg["application_key"].(string)
	if !ok || applicationKey == "" {
		return fmt.Errorf("B2 backend requires 'application_key' configuration")
	}

	// Create client
	ctx := context.Background()
	client, err := b2.NewClient(ctx, keyID, applicationKey)
	if err != nil {
		return fmt.Errorf("failed to create B2 client: %w", err)
	}
	b.client = client

	// Get bucket
	bucket, err := client.Bucket(ctx, bucketName)
	if err != nil {
		return fmt.Errorf("failed to access bucket: %w", err)
	}
	b.bucket = bucket

	return nil
}

// Test checks if the backend is accessible
func (b *B2Backend) Test() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to list files (limit to 1 to test connectivity)
	iter := b.bucket.List(ctx, b2.ListPrefix(b.prefix))
	for iter.Next() {
		// If we can iterate at least once, bucket is accessible
		break
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("cannot access bucket: %w", err)
	}

	return nil
}

// Upload uploads a file to B2
func (b *B2Backend) Upload(ctx context.Context, localPath string, remotePath string, progress ProgressCallback) error {
	// Open local file
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Error closing file: %v", err)
		}
	}()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}
	fileSize := stat.Size()

	// Add prefix if configured
	fileName := remotePath
	if b.prefix != "" {
		fileName = b.prefix + "/" + remotePath
	}

	// Wrap with progress reader
	progressReader := &progressReader{
		reader:   file,
		size:     fileSize,
		callback: progress,
	}

	// Upload file
	obj := b.bucket.Object(fileName)
	writer := obj.NewWriter(ctx)

	if _, err := io.Copy(writer, progressReader); err != nil {
		if closeErr := writer.Close(); closeErr != nil {
			log.Printf("Error closing writer after copy error: %v", closeErr)
		}
		return fmt.Errorf("failed to upload to B2: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to finalize upload: %w", err)
	}

	return nil
}

// List returns all backups with a given prefix
func (b *B2Backend) List(ctx context.Context, prefix string) ([]BackupInfo, error) {
	// Combine backend prefix with query prefix
	fullPrefix := prefix
	if b.prefix != "" {
		if prefix != "" {
			fullPrefix = b.prefix + "/" + prefix
		} else {
			fullPrefix = b.prefix
		}
	}

	var backups []BackupInfo
	iter := b.bucket.List(ctx, b2.ListPrefix(fullPrefix))

	for iter.Next() {
		obj := iter.Object()
		attrs, err := obj.Attrs(ctx)
		if err != nil {
			continue // Skip objects we can't get attrs for
		}

		// Remove backend prefix from path for display
		displayPath := obj.Name()
		if b.prefix != "" && len(displayPath) > len(b.prefix)+1 {
			displayPath = displayPath[len(b.prefix)+1:]
		}

		backups = append(backups, BackupInfo{
			Path:         displayPath,
			Size:         attrs.Size,
			LastModified: attrs.UploadTimestamp.Format(time.RFC3339),
			Hash:         attrs.SHA1,
		})
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	return backups, nil
}

// Delete removes a backup file
func (b *B2Backend) Delete(ctx context.Context, remotePath string) error {
	// Add prefix if configured
	fileName := remotePath
	if b.prefix != "" {
		fileName = b.prefix + "/" + remotePath
	}

	obj := b.bucket.Object(fileName)
	if err := obj.Delete(ctx); err != nil {
		return fmt.Errorf("failed to delete from B2: %w", err)
	}

	return nil
}

// GetUsage returns storage usage information
func (b *B2Backend) GetUsage(ctx context.Context) (*models.StorageUsage, error) {
	// Calculate total size of objects with our prefix
	var totalSize int64

	fullPrefix := b.prefix
	iter := b.bucket.List(ctx, b2.ListPrefix(fullPrefix))

	for iter.Next() {
		obj := iter.Object()
		attrs, err := obj.Attrs(ctx)
		if err != nil {
			continue
		}

		totalSize += attrs.Size
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to calculate usage: %w", err)
	}

	return &models.StorageUsage{
		Used:  totalSize,
		Total: -1, // B2 has no fixed limit
	}, nil
}

// Close closes the backend connection
func (b *B2Backend) Close() error {
	// B2 client doesn't need explicit cleanup
	return nil
}
