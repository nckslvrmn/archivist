package backend

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/nsilverman/archivist/internal/models"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// GDriveBackend stores backups on Google Drive
type GDriveBackend struct {
	service  *drive.Service
	folderID string
}

// Initialize sets up the Google Drive backend
func (b *GDriveBackend) Initialize(cfg map[string]interface{}, pathResolver PathResolver) error {
	ctx := context.Background()
	var service *drive.Service
	var err error

	// Check for service account key file (recommended for server-to-server)
	if credentialsFile, ok := cfg["credentials_file"].(string); ok && credentialsFile != "" {
		// Resolve path relative to root
		resolvedPath := pathResolver.ResolvePath(credentialsFile)
		service, err = drive.NewService(ctx, option.WithAuthCredentialsFile(option.ServiceAccount, resolvedPath))
	} else if credentialsJSON, ok := cfg["credentials_json"].(string); ok && credentialsJSON != "" {
		// Use JSON credentials directly
		service, err = drive.NewService(ctx, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(credentialsJSON)))
	} else {
		return fmt.Errorf("google Drive backend requires 'credentials_file' or 'credentials_json' configuration")
	}

	if err != nil {
		return fmt.Errorf("failed to create Drive service: %w", err)
	}
	b.service = service

	// Get or create folder
	folderName := "archivist-backups"
	if name, ok := cfg["folder_name"].(string); ok && name != "" {
		folderName = name
	}

	// Check if folder_id is provided
	if folderID, ok := cfg["folder_id"].(string); ok && folderID != "" {
		b.folderID = folderID
	} else {
		// Search for folder or create it
		folderID, err := b.findOrCreateFolder(ctx, folderName)
		if err != nil {
			return fmt.Errorf("failed to get/create folder: %w", err)
		}
		b.folderID = folderID
	}

	return nil
}

// findOrCreateFolder searches for a folder by name or creates it
func (b *GDriveBackend) findOrCreateFolder(ctx context.Context, name string) (string, error) {
	// Search for existing folder
	query := fmt.Sprintf("name='%s' and mimeType='application/vnd.google-apps.folder' and trashed=false", name)
	r, err := b.service.Files.List().Q(query).Spaces("drive").Fields("files(id, name)").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to search for folder: %w", err)
	}

	if len(r.Files) > 0 {
		return r.Files[0].Id, nil
	}

	// Create folder if it doesn't exist
	folder := &drive.File{
		Name:     name,
		MimeType: "application/vnd.google-apps.folder",
	}

	created, err := b.service.Files.Create(folder).Fields("id").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to create folder: %w", err)
	}

	return created.Id, nil
}

// Test checks if the backend is accessible
func (b *GDriveBackend) Test() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to get folder metadata
	_, err := b.service.Files.Get(b.folderID).Fields("id, name").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("cannot access folder: %w", err)
	}

	return nil
}

// Upload uploads a file to Google Drive
func (b *GDriveBackend) Upload(ctx context.Context, localPath string, remotePath string, progress ProgressCallback) error {
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

	// Check if file already exists (for updates)
	fileName := filepath.Base(remotePath)
	existingFileID, _ := b.findFileInFolder(ctx, fileName)

	// Wrap with progress reader
	progressReader := &progressReader{
		reader:   file,
		size:     fileSize,
		callback: progress,
	}

	driveFile := &drive.File{
		Name:    fileName,
		Parents: []string{b.folderID},
	}

	if existingFileID != "" {
		// Update existing file
		_, err = b.service.Files.Update(existingFileID, driveFile).Media(progressReader).Context(ctx).Do()
	} else {
		// Create new file
		_, err = b.service.Files.Create(driveFile).Media(progressReader).Context(ctx).Do()
	}

	if err != nil {
		return fmt.Errorf("failed to upload to Google Drive: %w", err)
	}

	return nil
}

// findFileInFolder searches for a file by name in the folder
func (b *GDriveBackend) findFileInFolder(ctx context.Context, fileName string) (string, error) {
	query := fmt.Sprintf("name='%s' and '%s' in parents and trashed=false", fileName, b.folderID)
	r, err := b.service.Files.List().Q(query).Spaces("drive").Fields("files(id)").Context(ctx).Do()
	if err != nil {
		return "", err
	}

	if len(r.Files) > 0 {
		return r.Files[0].Id, nil
	}

	return "", nil
}

// List returns all backups in the folder
func (b *GDriveBackend) List(ctx context.Context, prefix string) ([]BackupInfo, error) {
	var backups []BackupInfo

	// List all files in the folder
	query := fmt.Sprintf("'%s' in parents and trashed=false", b.folderID)
	if prefix != "" {
		// Google Drive doesn't support prefix search well, so we filter in-memory
		query = fmt.Sprintf("'%s' in parents and trashed=false and name contains '%s'", b.folderID, prefix)
	}

	pageToken := ""
	for {
		call := b.service.Files.List().
			Q(query).
			Spaces("drive").
			Fields("nextPageToken, files(id, name, size, modifiedTime)").
			PageSize(100).
			Context(ctx)

		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		r, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("failed to list files: %w", err)
		}

		for _, file := range r.Files {
			modTime, _ := time.Parse(time.RFC3339, file.ModifiedTime)
			backups = append(backups, BackupInfo{
				Path:         file.Name,
				Size:         file.Size,
				LastModified: modTime.Format(time.RFC3339),
				Hash:         file.Md5Checksum,
			})
		}

		pageToken = r.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return backups, nil
}

// Delete removes a backup file
func (b *GDriveBackend) Delete(ctx context.Context, remotePath string) error {
	fileName := filepath.Base(remotePath)

	// Find file ID
	fileID, err := b.findFileInFolder(ctx, fileName)
	if err != nil {
		return fmt.Errorf("failed to find file: %w", err)
	}
	if fileID == "" {
		return fmt.Errorf("file not found: %s", remotePath)
	}

	// Delete file
	if err := b.service.Files.Delete(fileID).Context(ctx).Do(); err != nil {
		return fmt.Errorf("failed to delete from Google Drive: %w", err)
	}

	return nil
}

// GetUsage returns storage usage information
func (b *GDriveBackend) GetUsage(ctx context.Context) (*models.StorageUsage, error) {
	// Calculate total size of files in folder
	var totalSize int64

	query := fmt.Sprintf("'%s' in parents and trashed=false", b.folderID)
	pageToken := ""

	for {
		call := b.service.Files.List().
			Q(query).
			Spaces("drive").
			Fields("nextPageToken, files(size)").
			PageSize(100).
			Context(ctx)

		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		r, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("failed to calculate usage: %w", err)
		}

		for _, file := range r.Files {
			totalSize += file.Size
		}

		pageToken = r.NextPageToken
		if pageToken == "" {
			break
		}
	}

	// Get account-wide quota
	about, err := b.service.About.Get().Fields("storageQuota").Context(ctx).Do()
	if err != nil {
		return &models.StorageUsage{
			Used:  totalSize,
			Total: -1,
		}, nil
	}

	return &models.StorageUsage{
		Used:  totalSize,
		Total: about.StorageQuota.Limit,
	}, nil
}

// Close closes the backend connection
func (b *GDriveBackend) Close() error {
	// Drive service doesn't need explicit cleanup
	return nil
}
