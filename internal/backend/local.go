package backend

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nsilverman/archivist/internal/models"
)

// LocalBackend stores backups on the local filesystem
type LocalBackend struct {
	basePath string
}

// Initialize sets up the local backend
func (l *LocalBackend) Initialize(config map[string]interface{}, pathResolver PathResolver) error {
	path, ok := config["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("local backend requires 'path' configuration")
	}

	// Resolve path relative to root directory if needed
	l.basePath = pathResolver.ResolvePath(path)

	// Create base directory if it doesn't exist
	if err := os.MkdirAll(l.basePath, 0755); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	return nil
}

// Test checks if the backend is accessible
func (l *LocalBackend) Test() error {
	// Check if directory exists and is writable
	info, err := os.Stat(l.basePath)
	if err != nil {
		return fmt.Errorf("cannot access path: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path is not a directory")
	}

	// Try to create a test file
	testFile := filepath.Join(l.basePath, ".archivist_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("directory is not writable: %w", err)
	}
	if err := os.Remove(testFile); err != nil {
		log.Printf("Warning: failed to remove test file: %v", err)
	}

	return nil
}

// Upload copies a file to the local backend
func (l *LocalBackend) Upload(ctx context.Context, localPath string, remotePath string, progress ProgressCallback) error {
	// Open source file
	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		if err := src.Close(); err != nil {
			log.Printf("Error closing source file: %v", err)
		}
	}()

	// Get source file info
	srcInfo, err := src.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}
	totalSize := srcInfo.Size()

	// Create destination path
	destPath := filepath.Join(l.basePath, remotePath)
	destDir := filepath.Dir(destPath)

	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Create destination file
	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		if err := dst.Close(); err != nil {
			log.Printf("Error closing destination file: %v", err)
		}
	}()

	// Copy with progress
	var bytesWritten int64
	buf := make([]byte, 32*1024) // 32KB buffer

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := src.Read(buf)
		if n > 0 {
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("failed to write to destination: %w", writeErr)
			}
			bytesWritten += int64(n)

			// Report progress
			if progress != nil {
				progress(bytesWritten, totalSize)
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read source file: %w", err)
		}
	}

	return nil
}

// List returns all backups with a given prefix
func (l *LocalBackend) List(ctx context.Context, prefix string) ([]BackupInfo, error) {
	searchPath := filepath.Join(l.basePath, prefix)
	searchDir := filepath.Dir(searchPath)
	pattern := filepath.Base(searchPath)

	var backups []BackupInfo

	// If pattern contains wildcard or is a directory, walk it
	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip paths we can't access
			return nil
		}

		if info.IsDir() {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(l.basePath, path)
		if err != nil {
			return nil
		}

		// Check if it matches prefix
		if pattern != "" && !matchesPrefix(relPath, prefix) {
			return nil
		}

		backups = append(backups, BackupInfo{
			Path:         relPath,
			Size:         info.Size(),
			LastModified: info.ModTime().Format(time.RFC3339),
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}

	return backups, nil
}

// Delete removes a backup file
func (l *LocalBackend) Delete(ctx context.Context, remotePath string) error {
	fullPath := filepath.Join(l.basePath, remotePath)

	if err := os.Remove(fullPath); err != nil {
		return fmt.Errorf("failed to delete backup: %w", err)
	}

	return nil
}

// GetUsage returns storage usage information
func (l *LocalBackend) GetUsage(ctx context.Context) (*models.StorageUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(l.basePath, &stat); err != nil {
		return nil, fmt.Errorf("failed to get filesystem stats: %w", err)
	}

	// Calculate used and total space
	total := stat.Blocks * uint64(stat.Bsize)
	available := stat.Bavail * uint64(stat.Bsize)
	used := total - available

	return &models.StorageUsage{
		Used:  int64(used),
		Total: int64(total),
	}, nil
}

// Close closes the backend (no-op for local)
func (l *LocalBackend) Close() error {
	return nil
}

// matchesPrefix checks if a path matches a prefix pattern
func matchesPrefix(path, prefix string) bool {
	// Simple prefix matching
	// In a more complete implementation, could support wildcards
	return len(path) >= len(prefix) && path[:len(prefix)] == prefix
}
