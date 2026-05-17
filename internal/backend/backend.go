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
	Hash         string // hex digest, no algorithm prefix
	HashAlgo     string // "md5", "sha1", "sha256", or "" if unavailable
}

// HashedBackend is an optional interface implemented by backends that can
// return a hash for a single remote object, allowing callers to skip uploads
// whose local content matches what is already stored remotely.
//
// algo is one of "md5", "sha1", "sha256". hexDigest is the lowercase hex
// digest. If the remote object exists but no usable hash is available
// (e.g. an S3 multipart ETag with no sha256 metadata), both algo and
// hexDigest are returned empty with a nil error. If the object does not
// exist, ErrRemoteObjectNotFound is returned.
type HashedBackend interface {
	RemoteHash(ctx context.Context, remotePath string) (algo string, hexDigest string, err error)
}

// ErrRemoteObjectNotFound signals that the remote object does not exist.
var ErrRemoteObjectNotFound = fmt.Errorf("remote object not found")

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
