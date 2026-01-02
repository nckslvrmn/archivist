package backend

import (
	"context"
	"fmt"

	"github.com/nsilverman/archivist/internal/models"
)

// ProgressCallback is called during upload to report progress
type ProgressCallback func(bytesUploaded, totalBytes int64)

// StorageBackend defines the interface for all storage backends
type StorageBackend interface {
	// Initialize connection with config
	Initialize(config map[string]interface{}, pathResolver PathResolver) error

	// Test connection
	Test() error

	// Upload archive to backend
	Upload(ctx context.Context, localPath string, remotePath string, progress ProgressCallback) error

	// List backups with a given prefix
	List(ctx context.Context, prefix string) ([]BackupInfo, error)

	// Delete a backup
	Delete(ctx context.Context, remotePath string) error

	// Get backend storage usage
	GetUsage(ctx context.Context) (*models.StorageUsage, error)

	// Close the backend connection
	Close() error
}

// BackupInfo represents information about a stored backup
type BackupInfo struct {
	Path         string
	Size         int64
	LastModified string
	Hash         string
}

// PathResolver resolves paths relative to a root directory
type PathResolver interface {
	ResolvePath(path string) string
}

// Factory creates a backend from a backend configuration
func Factory(backend *models.Backend, pathResolver PathResolver) (StorageBackend, error) {
	switch backend.Type {
	case "local":
		b := &LocalBackend{}
		if err := b.Initialize(backend.Config, pathResolver); err != nil {
			return nil, err
		}
		return b, nil
	case "s3":
		b := &S3Backend{}
		if err := b.Initialize(backend.Config, pathResolver); err != nil {
			return nil, err
		}
		return b, nil
	case "gcs":
		b := &GCSBackend{}
		if err := b.Initialize(backend.Config, pathResolver); err != nil {
			return nil, err
		}
		return b, nil
	case "gdrive":
		b := &GDriveBackend{}
		if err := b.Initialize(backend.Config, pathResolver); err != nil {
			return nil, err
		}
		return b, nil
	case "azure":
		b := &AzureBackend{}
		if err := b.Initialize(backend.Config, pathResolver); err != nil {
			return nil, err
		}
		return b, nil
	case "b2":
		b := &B2Backend{}
		if err := b.Initialize(backend.Config, pathResolver); err != nil {
			return nil, err
		}
		return b, nil
	default:
		return nil, fmt.Errorf("unknown backend type: %s", backend.Type)
	}
}
