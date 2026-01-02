package backend

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/nsilverman/archivist/internal/models"
)

// S3Backend stores backups on AWS S3 or S3-compatible storage
type S3Backend struct {
	client      *s3.Client
	uploader    *manager.Uploader
	bucket      string
	prefix      string
	storageTier types.StorageClass
}

// Initialize sets up the S3 backend
func (b *S3Backend) Initialize(cfg map[string]interface{}, pathResolver PathResolver) error {
	// Extract configuration
	bucket, ok := cfg["bucket"].(string)
	if !ok || bucket == "" {
		return fmt.Errorf("S3 backend requires 'bucket' configuration")
	}
	b.bucket = bucket

	// Optional prefix for organizing backups within bucket
	if prefix, ok := cfg["prefix"].(string); ok {
		b.prefix = prefix
	}

	region, ok := cfg["region"].(string)
	if !ok || region == "" {
		region = "us-east-1" // Default region
	}

	// Build AWS config
	var awsCfg aws.Config
	var err error

	// Check for static credentials
	accessKeyID, hasAccessKey := cfg["access_key_id"].(string)
	secretAccessKey, hasSecretKey := cfg["secret_access_key"].(string)

	if hasAccessKey && hasSecretKey && accessKeyID != "" && secretAccessKey != "" {
		// Use static credentials
		awsCfg, err = config.LoadDefaultConfig(context.Background(),
			config.WithRegion(region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				accessKeyID,
				secretAccessKey,
				"",
			)),
		)
	} else {
		// Use default credential chain (IAM role, env vars, etc.)
		awsCfg, err = config.LoadDefaultConfig(context.Background(),
			config.WithRegion(region),
		)
	}

	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Support custom endpoint for S3-compatible storage (MinIO, DigitalOcean Spaces, etc.)
	if endpoint, ok := cfg["endpoint"].(string); ok && endpoint != "" {
		b.client = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true // Required for MinIO and some S3-compatible services
		})
	} else {
		b.client = s3.NewFromConfig(awsCfg)
	}

	// Create uploader for efficient multipart uploads
	b.uploader = manager.NewUploader(b.client)

	// Extract and validate storage tier (optional)
	if storageTierStr, ok := cfg["storage_tier"].(string); ok && storageTierStr != "" {
		storageTier, err := validateS3StorageClass(storageTierStr)
		if err != nil {
			return err
		}
		b.storageTier = storageTier
	} else {
		// Default to STANDARD
		b.storageTier = types.StorageClassStandard
	}

	return nil
}

// Test checks if the backend is accessible
func (b *S3Backend) Test() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to head the bucket
	_, err := b.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(b.bucket),
	})
	if err != nil {
		return fmt.Errorf("cannot access bucket: %w", err)
	}

	return nil
}

// Upload uploads a file to S3
func (b *S3Backend) Upload(ctx context.Context, localPath string, remotePath string, progress ProgressCallback) error {
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

	// Get file size for progress reporting
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

	// Create a progress reader
	progressReader := &progressReader{
		reader:   file,
		size:     fileSize,
		callback: progress,
	}

	// Upload with multipart support
	_, err = b.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(b.bucket),
		Key:          aws.String(key),
		Body:         progressReader,
		StorageClass: b.storageTier,
	})

	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// List returns all backups with a given prefix
func (b *S3Backend) List(ctx context.Context, prefix string) ([]BackupInfo, error) {
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
	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.bucket),
		Prefix: aws.String(fullPrefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range page.Contents {
			// Remove backend prefix from path for display
			displayPath := *obj.Key
			if b.prefix != "" && len(displayPath) > len(b.prefix)+1 {
				displayPath = displayPath[len(b.prefix)+1:]
			}

			backups = append(backups, BackupInfo{
				Path:         displayPath,
				Size:         *obj.Size,
				LastModified: obj.LastModified.Format(time.RFC3339),
				Hash:         "", // S3 ETag is not a standard hash
			})
		}
	}

	return backups, nil
}

// Delete removes a backup file
func (b *S3Backend) Delete(ctx context.Context, remotePath string) error {
	// Add prefix if configured
	key := remotePath
	if b.prefix != "" {
		key = b.prefix + "/" + remotePath
	}

	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	return nil
}

// GetUsage returns storage usage information
func (b *S3Backend) GetUsage(ctx context.Context) (*models.StorageUsage, error) {
	// Calculate total size of objects in bucket with our prefix
	var totalSize int64

	fullPrefix := b.prefix
	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.bucket),
		Prefix: aws.String(fullPrefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate usage: %w", err)
		}

		for _, obj := range page.Contents {
			totalSize += *obj.Size
		}
	}

	return &models.StorageUsage{
		Used:  totalSize,
		Total: -1, // S3 has no fixed limit
	}, nil
}

// Close closes the backend connection
func (b *S3Backend) Close() error {
	// S3 client doesn't need explicit cleanup
	return nil
}

// progressReader wraps an io.Reader to report progress
type progressReader struct {
	reader   io.Reader
	size     int64
	read     int64
	callback ProgressCallback
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.read += int64(n)

	if pr.callback != nil {
		pr.callback(pr.read, pr.size)
	}

	return n, err
}

// validateS3StorageClass validates and returns the S3 storage class
func validateS3StorageClass(tier string) (types.StorageClass, error) {
	tier = strings.ToUpper(tier)
	valid := map[string]types.StorageClass{
		"STANDARD":            types.StorageClassStandard,
		"REDUCED_REDUNDANCY":  types.StorageClassReducedRedundancy,
		"STANDARD_IA":         types.StorageClassStandardIa,
		"ONEZONE_IA":          types.StorageClassOnezoneIa,
		"INTELLIGENT_TIERING": types.StorageClassIntelligentTiering,
		"GLACIER":             types.StorageClassGlacier,
		"GLACIER_IR":          types.StorageClassGlacierIr,
		"DEEP_ARCHIVE":        types.StorageClassDeepArchive,
	}
	if sc, ok := valid[tier]; ok {
		return sc, nil
	}
	validKeys := make([]string, 0, len(valid))
	for k := range valid {
		validKeys = append(validKeys, k)
	}
	return "", fmt.Errorf("invalid S3 storage class: %s. Valid values: %v", tier, validKeys)
}
