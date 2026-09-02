package sync

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"strings"
	"sync"
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
	// Concurrency caps how many files are compared and uploaded at once.
	// Zero selects defaultUploadConcurrency.
	Concurrency int
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

	// Step 3: whatever exists locally is not a deletion candidate. This pass
	// is sequential and cheap (map operations only) so the upload pass below
	// can run concurrently without touching remoteFileMap.
	comparisons := make([]comparison, 0, len(localFiles))
	for _, localFile := range localFiles {
		remoteFile, exists := remoteFileMap[localFile.RelativePath]
		comparisons = append(comparisons, comparison{local: localFile, remote: remoteFile, remoteExists: exists})
		delete(remoteFileMap, localFile.RelativePath)
	}

	// Step 4: compare and upload, several files at a time. Both halves of the
	// per-file work benefit: hashing for the comparison is CPU/IO bound and
	// uploads are latency bound, and doing them one file at a time is the
	// difference between minutes and hours on a many-small-files source.
	if err := s.uploadAll(ctx, comparisons, result); err != nil {
		return result, err
	}

	// Step 5: Delete remote files that don't exist locally (if enabled)
	if s.Options.DeleteRemote && len(remoteFileMap) > 0 {
		// Guard against a source that exists but is empty — an unmounted
		// volume or a failed network mount looks exactly like "the user
		// deleted everything", and mirroring that would destroy the only
		// remaining copy.
		if len(localFiles) == 0 && len(remoteFiles) > 0 {
			return result, fmt.Errorf(
				"refusing to delete %d remote file(s): source %s contains no files (unmounted volume?)",
				len(remoteFiles), s.SourcePath)
		}

		s.reportProgress("deleting", 0, len(remoteFileMap), "")
		i := 0
		for _, remoteFile := range remoteFileMap {
			if err := ctx.Err(); err != nil {
				return result, err
			}
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

// comparison pairs a local file with its remote counterpart, if any.
type comparison struct {
	local        FileInfo
	remote       backend.BackupInfo
	remoteExists bool
}

// defaultUploadConcurrency is used when Concurrency is unset.
const defaultUploadConcurrency = 4

// uploadAll compares and uploads files with bounded concurrency, updating
// result as it goes. It returns an error only when the whole sync should stop
// (cancellation); per-file failures are collected into result.Errors so one
// bad file does not abandon the rest.
func (s *Syncer) uploadAll(ctx context.Context, comparisons []comparison, result *SyncResult) error {
	total := len(comparisons)
	s.reportProgress("syncing", 0, total, "")
	if total == 0 {
		return nil
	}

	workers := s.Concurrency
	if workers <= 0 {
		workers = defaultUploadConcurrency
	}
	if workers > total {
		workers = total
	}

	var (
		mu        sync.Mutex // guards result and the progress counter
		processed int
		wg        sync.WaitGroup
	)

	jobs := make(chan comparison)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}

				needsUpload := !job.remoteExists || s.needsUpload(job.local, job.remote)

				var uploadErr error
				if needsUpload {
					remotePath := filepath.ToSlash(filepath.Join(s.RemotePath, job.local.RelativePath))
					uploadErr = backend.UploadWithRetry(ctx, s.Backend, job.local.Path, remotePath,
						func(uploaded, total int64) {}, backend.DefaultUploadRetry)
				}

				mu.Lock()
				switch {
				case !needsUpload:
					result.FilesSkipped++
				case uploadErr != nil:
					result.Errors = append(result.Errors,
						fmt.Errorf("failed to upload %s: %w", job.local.RelativePath, uploadErr))
				default:
					result.FilesUploaded++
					result.BytesUploaded += job.local.Size
				}
				processed++
				current := processed
				mu.Unlock()

				s.reportProgress("syncing", current, total, job.local.RelativePath)
			}
		}()
	}

	for _, job := range comparisons {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		case jobs <- job:
		}
	}
	close(jobs)
	wg.Wait()

	return ctx.Err()
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

// getUploadReason explains why a file would be uploaded. Mirrors the
// decision tree in needsUpload so dry-run output cannot disagree with the
// actual sync behaviour.
func (s *Syncer) getUploadReason(local FileInfo, remote backend.BackupInfo) string {
	if local.Size != remote.Size {
		return "Size changed"
	}
	if s.compareMethod() == CompareHash && (remote.HashAlgo == "" || remote.Hash == "") {
		return "No remote hash available for hash comparison"
	}
	if s.compareMethod() == CompareMtime {
		return "Modified timestamp newer"
	}
	if remote.HashAlgo != "" && remote.Hash != "" {
		if localHex, err := backend.ComputeFileHash(local.Path, remote.HashAlgo); err == nil {
			if !strings.EqualFold(localHex, remote.Hash) {
				return "Content hash differs"
			}
			// Hash matched — caller only invokes us when needsUpload is true,
			// which in this branch means mtime-newer fallback fired.
			return "Modified timestamp newer"
		}
		// Hash compute failed; needsUpload fell back to mtime.
		return "Modified timestamp newer (hash unavailable)"
	}
	return "Modified timestamp newer"
}

// scanLocalFiles scans the source directory and returns a list of files.
//
// The source root is resolved through symlinks first: the documented setup
// points source_path at a symlink under sources/, and a walk of a symlinked
// root yields only the link itself. Entries that are not regular files
// (symlinks, sockets, devices, FIFOs) have no object representation on a
// storage backend and are skipped rather than failing the sync.
func (s *Syncer) scanLocalFiles() ([]FileInfo, error) {
	root, err := filepath.EvalSymlinks(s.SourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve source path %s: %w", s.SourcePath, err)
	}

	var files []FileInfo

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			log.Printf("sync: skipping %s: %v", path, walkErr)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			log.Printf("sync: skipping %s: %v", path, err)
			return nil
		}

		if !info.Mode().IsRegular() {
			log.Printf("sync: skipping non-regular file %s (mode %s)", path, info.Mode())
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			log.Printf("sync: skipping %s: %v", path, err)
			return nil
		}

		files = append(files, FileInfo{
			Path:         path,
			RelativePath: filepath.ToSlash(relPath),
			Size:         info.Size(),
			ModTime:      info.ModTime(),
		})
		return nil
	})

	return files, err
}

// listRemoteFiles lists all files in the remote directory
func (s *Syncer) listRemoteFiles(ctx context.Context) ([]backend.BackupInfo, error) {
	return s.Backend.List(ctx, s.RemotePath)
}

// Comparison methods accepted in SyncOptions.CompareMethod.
const (
	CompareAuto  = "auto"
	CompareHash  = "hash"
	CompareMtime = "mtime"
	CompareSize  = "size"
)

// compareMethod resolves the configured method, defaulting to auto.
func (s *Syncer) compareMethod() string {
	switch strings.ToLower(s.Options.CompareMethod) {
	case CompareHash:
		return CompareHash
	case CompareMtime:
		return CompareMtime
	case CompareSize:
		return CompareSize
	default:
		return CompareAuto
	}
}

// needsUpload decides whether a local file differs from its remote copy,
// according to the configured comparison method.
func (s *Syncer) needsUpload(local FileInfo, remote backend.BackupInfo) bool {
	// A size difference is conclusive under every method.
	if local.Size != remote.Size {
		return true
	}

	switch s.compareMethod() {
	case CompareSize:
		return false
	case CompareMtime:
		return s.mtimeNewer(local, remote)
	case CompareHash:
		// Explicit hash comparison: with no remote hash available there is no
		// evidence the copies match, so re-upload rather than assume.
		if remote.HashAlgo == "" || remote.Hash == "" {
			return true
		}
		localHex, err := backend.ComputeFileHash(local.Path, remote.HashAlgo)
		if err != nil {
			return true
		}
		return !strings.EqualFold(localHex, remote.Hash)
	default: // auto
		if remote.HashAlgo != "" && remote.Hash != "" {
			localHex, err := backend.ComputeFileHash(local.Path, remote.HashAlgo)
			if err != nil {
				return s.mtimeNewer(local, remote)
			}
			return !strings.EqualFold(localHex, remote.Hash)
		}
		return s.mtimeNewer(local, remote)
	}
}

func (s *Syncer) mtimeNewer(local FileInfo, remote backend.BackupInfo) bool {
	remoteModTime, err := time.Parse(time.RFC3339, remote.LastModified)
	if err != nil {
		return false
	}
	// 1s tolerance accounts for filesystem mtime resolution differences.
	return local.ModTime.After(remoteModTime.Add(time.Second))
}

// reportProgress reports sync progress
func (s *Syncer) reportProgress(phase string, current, total int, file string) {
	if s.Progress != nil {
		s.Progress(phase, current, total, file)
	}
}
