package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nsilverman/archivist/internal/backend"
	"github.com/nsilverman/archivist/internal/models"
)

// ProgressCallback is called during sync to report progress
type ProgressCallback func(phase string, current, total int, currentFile string)

// FileInfo represents information about a file for comparison
type FileInfo struct {
	Path         string
	RelativePath string
	Size         int64
	ModTime      time.Time
	Hash         string // Only computed if using hash comparison
}

// SyncResult represents the result of a sync operation
type SyncResult struct {
	FilesScanned  int
	FilesUploaded int
	FilesDeleted  int
	FilesSkipped  int
	BytesTotal    int64
	BytesUploaded int64
	Errors        []error
}

// Syncer handles file-by-file synchronization
type Syncer struct {
	SourcePath string
	Backend    backend.StorageBackend
	RemotePath string
	Options    models.SyncOptions
	Progress   ProgressCallback
}

// NewSyncer creates a new syncer
func NewSyncer(sourcePath string, backend backend.StorageBackend, remotePath string, options models.SyncOptions, progress ProgressCallback) *Syncer {
	return &Syncer{
		SourcePath: sourcePath,
		Backend:    backend,
		RemotePath: remotePath,
		Options:    options,
		Progress:   progress,
	}
}

// Sync performs the file-by-file synchronization
func (s *Syncer) Sync(ctx context.Context) (*SyncResult, error) {
	result := &SyncResult{}

	// Step 1: Scan local files
	s.reportProgress("scanning_local", 0, 0, "")
	localFiles, err := s.scanLocalFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to scan local files: %w", err)
	}
	result.FilesScanned = len(localFiles)

	// Calculate total bytes
	for _, file := range localFiles {
		result.BytesTotal += file.Size
	}

	// Step 2: List remote files
	s.reportProgress("listing_remote", 0, 0, "")
	remoteFiles, err := s.listRemoteFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list remote files: %w", err)
	}

	// Create a map of remote files for easy lookup
	remoteFileMap := make(map[string]backend.BackupInfo)
	for _, rf := range remoteFiles {
		// Remove remote path prefix to get relative path
		relPath := rf.Path
		if s.RemotePath != "" && len(relPath) > len(s.RemotePath)+1 {
			relPath = relPath[len(s.RemotePath)+1:]
		}
		remoteFileMap[relPath] = rf
	}

	// Step 3: Compare and upload changed/new files
	s.reportProgress("syncing", 0, len(localFiles), "")
	for i, localFile := range localFiles {
		s.reportProgress("syncing", i, len(localFiles), localFile.RelativePath)

		remoteFile, exists := remoteFileMap[localFile.RelativePath]
		needsUpload := false

		if !exists {
			// File doesn't exist remotely, upload it
			needsUpload = true
		} else {
			// File exists, compare based on method
			needsUpload = s.needsUpload(localFile, remoteFile)
		}

		if needsUpload {
			// Upload file
			remotePath := filepath.Join(s.RemotePath, localFile.RelativePath)
			// Convert to forward slashes for remote paths
			remotePath = filepath.ToSlash(remotePath)

			// Create progress callback for this file
			uploadProgress := func(uploaded, total int64) {
				// Could report per-file progress here if needed
			}

			err := s.Backend.Upload(ctx, localFile.Path, remotePath, uploadProgress)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("failed to upload %s: %w", localFile.RelativePath, err))
			} else {
				result.FilesUploaded++
				result.BytesUploaded += localFile.Size
			}
		} else {
			result.FilesSkipped++
		}

		// Remove from remote map (we'll use the remaining entries for deletion)
		delete(remoteFileMap, localFile.RelativePath)
	}

	// Step 4: Delete remote files that don't exist locally (if enabled)
	if s.Options.DeleteRemote && len(remoteFileMap) > 0 {
		s.reportProgress("deleting", 0, len(remoteFileMap), "")
		i := 0
		for _, remoteFile := range remoteFileMap {
			s.reportProgress("deleting", i, len(remoteFileMap), remoteFile.Path)
			err := s.Backend.Delete(ctx, remoteFile.Path)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("failed to delete %s: %w", remoteFile.Path, err))
			} else {
				result.FilesDeleted++
			}
			i++
		}
	}

	s.reportProgress("completed", len(localFiles), len(localFiles), "")

	return result, nil
}

// DryRun performs sync analysis without making changes
func (s *Syncer) DryRun(ctx context.Context) (*models.SyncDetails, error) {
	details := &models.SyncDetails{
		FilesToUpload: make([]models.FileDetail, 0),
		FilesToDelete: make([]string, 0),
		FilesToSkip:   make([]models.FileDetail, 0),
	}

	// Scan local files
	localFiles, err := s.scanLocalFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to scan local files: %w", err)
	}

	// List remote files
	remoteFiles, err := s.listRemoteFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list remote files: %w", err)
	}

	// Create remote file map
	remoteFileMap := make(map[string]backend.BackupInfo)
	for _, rf := range remoteFiles {
		relPath := rf.Path
		if s.RemotePath != "" && len(relPath) > len(s.RemotePath)+1 {
			relPath = relPath[len(s.RemotePath)+1:]
		}
		remoteFileMap[relPath] = rf
	}

	// Analyze what would happen
	for _, localFile := range localFiles {
		remoteFile, exists := remoteFileMap[localFile.RelativePath]

		fileDetail := models.FileDetail{
			RelativePath: localFile.RelativePath,
			Size:         localFile.Size,
			ModTime:      localFile.ModTime,
			Hash:         localFile.Hash,
		}

		if !exists {
			fileDetail.Reason = "New file"
			details.FilesToUpload = append(details.FilesToUpload, fileDetail)
			details.UploadCount++
			details.BytesToUpload += localFile.Size
		} else if s.needsUpload(localFile, remoteFile) {
			fileDetail.Reason = s.getUploadReason(localFile, remoteFile)
			details.FilesToUpload = append(details.FilesToUpload, fileDetail)
			details.UploadCount++
			details.BytesToUpload += localFile.Size
		} else {
			fileDetail.Reason = "Unchanged"
			details.FilesToSkip = append(details.FilesToSkip, fileDetail)
			details.SkipCount++
		}

		delete(remoteFileMap, localFile.RelativePath)
	}

	// Files remaining in remote map would be deleted
	if s.Options.DeleteRemote {
		for _, remoteFile := range remoteFileMap {
			details.FilesToDelete = append(details.FilesToDelete, remoteFile.Path)
			details.DeleteCount++
		}
	}

	return details, nil
}

// getUploadReason explains why a file would be uploaded
func (s *Syncer) getUploadReason(local FileInfo, remote backend.BackupInfo) string {
	if local.Size != remote.Size {
		return "Size changed"
	}

	return "Modified timestamp newer"
}

// scanLocalFiles scans the source directory and returns a list of files
func (s *Syncer) scanLocalFiles() ([]FileInfo, error) {
	var files []FileInfo

	err := filepath.Walk(s.SourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(s.SourcePath, path)
		if err != nil {
			return err
		}

		fileInfo := FileInfo{
			Path:         path,
			RelativePath: relPath,
			Size:         info.Size(),
			ModTime:      info.ModTime(),
		}

		files = append(files, fileInfo)
		return nil
	})

	return files, err
}

// listRemoteFiles lists all files in the remote directory
func (s *Syncer) listRemoteFiles(ctx context.Context) ([]backend.BackupInfo, error) {
	return s.Backend.List(ctx, s.RemotePath)
}

// needsUpload determines if a file needs to be uploaded based on size and modification time
func (s *Syncer) needsUpload(local FileInfo, remote backend.BackupInfo) bool {
	// Compare size first (fast check)
	if local.Size != remote.Size {
		return true
	}

	// Parse remote modification time
	remoteModTime, err := time.Parse(time.RFC3339, remote.LastModified)
	if err != nil {
		// If we can't parse time, assume unchanged since size matches
		return false
	}

	// Upload if local is newer (with 1 second tolerance for filesystem differences)
	return local.ModTime.After(remoteModTime.Add(time.Second))
}

// reportProgress reports sync progress
func (s *Syncer) reportProgress(phase string, current, total int, file string) {
	if s.Progress != nil {
		s.Progress(phase, current, total, file)
	}
}
