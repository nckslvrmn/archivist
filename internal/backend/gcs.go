package backend

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/nsilverman/archivist/internal/models"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// GCSBackend stores backups on Google Cloud Storage
type GCSBackend struct {
	client      *storage.Client
	bucket      string
	prefix      string
	storageTier string
}

// Initialize sets up the GCS backend
func (b *GCSBackend) Initialize(cfg map[string]interface{}, pathResolver PathResolver) error {
	// Extract configuration
	bucket, ok := cfg["bucket"].(string)
	if !ok || bucket == "" {
		return fmt.Errorf("GCS backend requires 'bucket' configuration")
	}
	b.bucket = bucket

	// Optional prefix
	if prefix, ok := cfg["prefix"].(string); ok {
		b.prefix = prefix
	}

	// Optional storage tier
	if tier, ok := cfg["storage_tier"].(string); ok && tier != "" {
		validTier, err := validateGCSStorageClass(tier)
		if err != nil {
			return err
		}
		b.storageTier = validTier
	} else {
		b.storageTier = "STANDARD"
	}

	// Create client
	ctx := context.Background()
	var client *storage.Client
	var err error

	// Check for service account key file
	if credentialsFile, ok := cfg["credentials_file"].(string); ok && credentialsFile != "" {
		// Resolve path relative to root
		resolvedPath := pathResolver.ResolvePath(credentialsFile)
		client, err = storage.NewClient(ctx, option.WithAuthCredentialsFile(option.ServiceAccount, resolvedPath))
	} else if credentialsJSON, ok := cfg["credentials_json"].(string); ok && credentialsJSON != "" {
		// Use JSON credentials directly
		client, err = storage.NewClient(ctx, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(credentialsJSON)))
	} else {
		// Use Application Default Credentials (ADC)
		client, err = storage.NewClient(ctx)
	}

	if err != nil {
		return fmt.Errorf("failed to create GCS client: %w", err)
	}

	b.client = client
	return nil
}

// Test checks if the backend is accessible
func (b *GCSBackend) Test() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to get bucket attributes
	bucket := b.client.Bucket(b.bucket)
	_, err := bucket.Attrs(ctx)
	if err != nil {
		return fmt.Errorf("cannot access bucket: %w", err)
	}

	return nil
}

// Upload uploads a file to GCS
func (b *GCSBackend) Upload(ctx context.Context, localPath string, remotePath string, progress ProgressCallback) error {
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
	key := remotePath
	if b.prefix != "" {
		key = b.prefix + "/" + remotePath
	}

	// Create object writer
	obj := b.client.Bucket(b.bucket).Object(key)
	writer := obj.NewWriter(ctx)

	// Set storage class if configured
	writer.StorageClass = b.storageTier

	// Wrap with progress reader
	progressReader := &progressReader{
		reader:   file,
		size:     fileSize,
		callback: progress,
	}

	// Copy data
	if _, err := io.Copy(writer, progressReader); err != nil {
		if closeErr := writer.Close(); closeErr != nil {
			log.Printf("Error closing writer after copy error: %v", closeErr)
		}
		return fmt.Errorf("failed to upload to GCS: %w", err)
	}

	// Close writer (this finalizes the upload)
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to finalize upload: %w", err)
	}

	return nil
}

// List returns all backups with a given prefix
func (b *GCSBackend) List(ctx context.Context, prefix string) ([]BackupInfo, error) {
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
	bucket := b.client.Bucket(b.bucket)
	query := &storage.Query{Prefix: fullPrefix}
	it := bucket.Objects(ctx, query)

	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}

		// Remove backend prefix from path for display
		displayPath := attrs.Name
		if b.prefix != "" && len(displayPath) > len(b.prefix)+1 {
			displayPath = displayPath[len(b.prefix)+1:]
		}

		backups = append(backups, BackupInfo{
			Path:         displayPath,
			Size:         attrs.Size,
			LastModified: attrs.Updated.Format(time.RFC3339),
			Hash:         fmt.Sprintf("md5:%x", attrs.MD5),
		})
	}

	return backups, nil
}

// Delete removes a backup file
func (b *GCSBackend) Delete(ctx context.Context, remotePath string) error {
	// Add prefix if configured
	key := remotePath
	if b.prefix != "" {
		key = b.prefix + "/" + remotePath
	}

	obj := b.client.Bucket(b.bucket).Object(key)
	if err := obj.Delete(ctx); err != nil {
		return fmt.Errorf("failed to delete from GCS: %w", err)
	}

	return nil
}

// GetUsage returns storage usage information
func (b *GCSBackend) GetUsage(ctx context.Context) (*models.StorageUsage, error) {
	// Calculate total size of objects with our prefix
	var totalSize int64

	fullPrefix := b.prefix
	bucket := b.client.Bucket(b.bucket)
	query := &storage.Query{Prefix: fullPrefix}
	it := bucket.Objects(ctx, query)

	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to calculate usage: %w", err)
		}

		totalSize += attrs.Size
	}

	return &models.StorageUsage{
		Used:  totalSize,
		Total: -1, // GCS has no fixed limit
	}, nil
}

// Close closes the backend connection
func (b *GCSBackend) Close() error {
	if b.client != nil {
		return b.client.Close()
	}
	return nil
}

// validateGCSStorageClass validates and returns a GCS storage class
func validateGCSStorageClass(tier string) (string, error) {
	// GCS storage classes (case-insensitive)
	validClasses := map[string]string{
		"STANDARD":         "STANDARD",
		"NEARLINE":         "NEARLINE",
		"COLDLINE":         "COLDLINE",
		"ARCHIVE":          "ARCHIVE",
		"REGIONAL":         "REGIONAL",         // Legacy, maps to STANDARD
		"MULTI_REGIONAL":   "MULTI_REGIONAL",   // Legacy, maps to STANDARD
		"DURABLE_REDUCED_AVAILABILITY": "DURABLE_REDUCED_AVAILABILITY", // Legacy
	}

	// Convert to uppercase for case-insensitive comparison
	upperTier := strings.ToUpper(tier)
	if normalizedTier, ok := validClasses[upperTier]; ok {
		return normalizedTier, nil
	}

	return "", fmt.Errorf("invalid GCS storage class: %s (valid options: STANDARD, NEARLINE, COLDLINE, ARCHIVE)", tier)
}
