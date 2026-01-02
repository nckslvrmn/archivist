package executor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nsilverman/archivist/internal/archive"
	"github.com/nsilverman/archivist/internal/backend"
	"github.com/nsilverman/archivist/internal/config"
	"github.com/nsilverman/archivist/internal/models"
	"github.com/nsilverman/archivist/internal/storage"
	filesync "github.com/nsilverman/archivist/internal/sync"
)

// Executor handles backup task execution
type Executor struct {
	config   *config.Manager
	db       *storage.Database
	running  map[string]*RunningExecution
	mu       sync.RWMutex
	progress ProgressBroadcaster
}

// RunningExecution tracks a currently running execution
type RunningExecution struct {
	ID        string
	TaskID    string
	StartedAt time.Time
	Cancel    context.CancelFunc
}

// ProgressBroadcaster is an interface for broadcasting progress updates
type ProgressBroadcaster interface {
	BroadcastProgress(event models.ProgressEvent)
}

// NewExecutor creates a new backup executor
func NewExecutor(cfg *config.Manager, db *storage.Database) *Executor {
	return &Executor{
		config:  cfg,
		db:      db,
		running: make(map[string]*RunningExecution),
	}
}

// SetProgressBroadcaster sets the progress broadcaster
func (e *Executor) SetProgressBroadcaster(broadcaster ProgressBroadcaster) {
	e.progress = broadcaster
}

// Execute runs a backup task
func (e *Executor) Execute(taskID string) (string, error) {
	// Get task configuration
	task, err := e.config.GetTask(taskID)
	if err != nil {
		return "", fmt.Errorf("failed to get task: %w", err)
	}

	if !task.Enabled {
		return "", fmt.Errorf("task is disabled")
	}

	// Check if task is already running
	e.mu.RLock()
	if _, exists := e.running[taskID]; exists {
		e.mu.RUnlock()
		return "", fmt.Errorf("task is already running")
	}
	e.mu.RUnlock()

	// Create execution record
	executionID := uuid.New().String()
	execution := &models.Execution{
		ID:        executionID,
		TaskID:    taskID,
		TaskName:  task.Name,
		StartedAt: time.Now(),
		Status:    "running",
	}

	if err := e.db.CreateExecution(execution); err != nil {
		return "", fmt.Errorf("failed to create execution record: %w", err)
	}

	// Create cancellation context
	ctx, cancel := context.WithCancel(context.Background())

	// Track running execution
	e.mu.Lock()
	e.running[taskID] = &RunningExecution{
		ID:        executionID,
		TaskID:    taskID,
		StartedAt: execution.StartedAt,
		Cancel:    cancel,
	}
	e.mu.Unlock()

	// Broadcast execution started
	e.broadcastEvent(models.ProgressEvent{
		Type: "execution_started",
		Data: map[string]interface{}{
			"execution_id": executionID,
			"task_id":      taskID,
			"task_name":    task.Name,
			"started_at":   execution.StartedAt,
		},
	})

	// Run execution in background
	go func() {
		defer func() {
			e.mu.Lock()
			delete(e.running, taskID)
			e.mu.Unlock()
		}()

		if err := e.runExecution(ctx, task, execution); err != nil {
			log.Printf("Execution failed for task %s: %v", task.Name, err)
		}
	}()

	return executionID, nil
}

// ExecuteDryRun performs a dry run analysis without making changes
func (e *Executor) ExecuteDryRun(taskID string, backendIDs []string) (*models.DryRunResult, error) {
	startTime := time.Now()

	// Get task configuration
	task, err := e.config.GetTask(taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	// Resolve paths
	sourcePath := e.config.ResolvePath(task.SourcePath)

	// Verify source exists
	if _, err := os.Stat(sourcePath); err != nil {
		return nil, fmt.Errorf("source path not accessible: %w", err)
	}

	result := &models.DryRunResult{
		TaskID:     taskID,
		TaskName:   task.Name,
		SourcePath: sourcePath,
		AnalyzedAt: startTime,
	}

	// Use task backends if none specified
	if len(backendIDs) == 0 {
		backendIDs = task.BackendIDs
	}

	// Determine mode and execute appropriate dry run
	if task.ArchiveOptions.Format == "sync" {
		result.Mode = "sync"
		if err := e.dryRunSync(task, sourcePath, backendIDs, result); err != nil {
			return nil, err
		}
	} else {
		result.Mode = "archive"
		if err := e.dryRunArchive(task, sourcePath, result); err != nil {
			return nil, err
		}
	}

	// Analyze backends
	result.BackendPlans = e.analyzeBackends(task, backendIDs)

	result.DurationMs = time.Since(startTime).Milliseconds()
	return result, nil
}

// dryRunArchive analyzes what an archive operation would do
func (e *Executor) dryRunArchive(task *models.Task, sourcePath string, result *models.DryRunResult) error {
	// Scan source directory
	summary, err := e.scanSourceDirectory(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to scan source: %w", err)
	}
	result.FilesSummary = *summary

	// Generate archive name
	builder := archive.NewBuilder(sourcePath, "", task.ArchiveOptions, nil)
	archiveName, err := builder.GenerateFilename(task.Name)
	if err != nil {
		return fmt.Errorf("failed to generate archive name: %w", err)
	}

	// Estimate compression (use heuristic: ~30% reduction for gzip on typical data)
	compressionRatio := 0.7
	if task.ArchiveOptions.Compression == "none" {
		compressionRatio = 1.0
	}

	result.ArchiveDetails = &models.ArchiveDetails{
		EstimatedArchiveSize: int64(float64(summary.TotalSize) * compressionRatio),
		CompressionRatio:     compressionRatio,
		Format:               task.ArchiveOptions.Format,
		ArchiveName:          archiveName,
	}

	return nil
}

// dryRunSync analyzes what a sync operation would do
func (e *Executor) dryRunSync(task *models.Task, sourcePath string, backendIDs []string, result *models.DryRunResult) error {
	ctx := context.Background()

	// Scan local files
	summary, err := e.scanSourceDirectory(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to scan source: %w", err)
	}
	result.FilesSummary = *summary

	// For sync mode, we need to analyze against specified backends
	// We'll use the first available backend for the analysis
	var syncDetails *models.SyncDetails

	for _, backendID := range backendIDs {
		backendCfg, err := e.config.GetBackend(backendID)
		if err != nil {
			continue
		}

		backendInstance, err := backend.Factory(backendCfg, e.config)
		if err != nil {
			continue
		}
		defer func() {
		if err := backendInstance.Close(); err != nil {
			log.Printf("Error closing backend instance: %v", err)
		}
	}()

		// Generate remote path (use task name as folder)
		remotePath := task.Name

		// Add backend prefix if configured (same as actual sync execution)
		if prefix, ok := backendCfg.Config["prefix"].(string); ok && prefix != "" {
			remotePath = filepath.Join(prefix, remotePath)
		}

		// Perform dry run sync analysis
		syncer := filesync.NewSyncer(sourcePath, backendInstance, remotePath,
			task.ArchiveOptions.SyncOptions, nil)
		syncDetails, err = syncer.DryRun(ctx)
		if err == nil {
			break // Successfully got sync details
		}
	}

	if syncDetails != nil {
		result.SyncDetails = syncDetails
	} else {
		// If we couldn't get sync details, create empty one
		result.SyncDetails = &models.SyncDetails{
			FilesToUpload: make([]models.FileDetail, 0),
			FilesToDelete: make([]string, 0),
			FilesToSkip:   make([]models.FileDetail, 0),
		}
	}

	return nil
}

// scanSourceDirectory scans a directory and returns summary
func (e *Executor) scanSourceDirectory(sourcePath string) (*models.FilesSummary, error) {
	summary := &models.FilesSummary{
		FileTypes: make(map[string]int),
		TopFiles:  make([]models.FileDetail, 0),
	}

	var allFiles []models.FileDetail

	err := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			summary.TotalDirs++
			return nil
		}

		summary.TotalFiles++
		summary.TotalSize += info.Size()

		// Track file types
		ext := filepath.Ext(path)
		if ext == "" {
			ext = "[no extension]"
		}
		summary.FileTypes[ext]++

		// Track largest file
		if info.Size() > summary.LargestFileSize {
			summary.LargestFileSize = info.Size()
			relPath, _ := filepath.Rel(sourcePath, path)
			summary.LargestFile = relPath
		}

		// Collect for top files
		relPath, _ := filepath.Rel(sourcePath, path)
		allFiles = append(allFiles, models.FileDetail{
			RelativePath: relPath,
			Size:         info.Size(),
			ModTime:      info.ModTime(),
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort and get top 10 files by size
	sort.Slice(allFiles, func(i, j int) bool {
		return allFiles[i].Size > allFiles[j].Size
	})
	if len(allFiles) > 10 {
		summary.TopFiles = allFiles[:10]
	} else {
		summary.TopFiles = allFiles
	}

	return summary, nil
}

// analyzeBackends checks which backends are available
func (e *Executor) analyzeBackends(task *models.Task, backendIDs []string) []models.BackendPlan {
	plans := make([]models.BackendPlan, 0, len(backendIDs))

	for _, backendID := range backendIDs {
		plan := models.BackendPlan{
			BackendID: backendID,
		}

		backendCfg, err := e.config.GetBackend(backendID)
		if err != nil {
			plan.Available = false
			plan.ErrorMessage = "Backend not found"
			plans = append(plans, plan)
			continue
		}

		plan.BackendName = backendCfg.Name
		plan.BackendType = backendCfg.Type

		// Test backend connectivity
		backendInstance, err := backend.Factory(backendCfg, e.config)
		if err != nil {
			plan.Available = false
			plan.ErrorMessage = fmt.Sprintf("Failed to initialize: %v", err)
			plans = append(plans, plan)
			continue
		}
		defer func() {
		if err := backendInstance.Close(); err != nil {
			log.Printf("Error closing backend instance: %v", err)
		}
	}()

		if err := backendInstance.Test(); err != nil {
			plan.Available = false
			plan.ErrorMessage = fmt.Sprintf("Connection test failed: %v", err)
		} else {
			plan.Available = true
		}

		// Determine remote path
		if task.ArchiveOptions.Format == "sync" {
			plan.RemotePath = task.Name
		} else {
			// Would be the archive filename
			builder := archive.NewBuilder("", "", task.ArchiveOptions, nil)
			filename, _ := builder.GenerateFilename(task.Name)
			plan.RemotePath = filename
		}

		plans = append(plans, plan)
	}

	return plans
}

// runExecution performs the actual backup execution
func (e *Executor) runExecution(ctx context.Context, task *models.Task, execution *models.Execution) error {
	startTime := time.Now()

	// Get settings
	settings := e.config.GetSettings()

	// Resolve paths relative to root directory first
	sourcePath := e.config.ResolvePath(task.SourcePath)
	tempDir := e.config.ResolvePath(settings.TempDir)

	// Verify source path exists
	if _, err := os.Stat(sourcePath); err != nil {
		execution.Status = "failed"
		execution.ErrorMessage = fmt.Sprintf("Source path not accessible: %v", err)
		now := time.Now()
		execution.CompletedAt = &now
		execution.DurationMs = time.Since(startTime).Milliseconds()
		if dbErr := e.db.UpdateExecution(execution); dbErr != nil {
			log.Printf("Error updating execution: %v", dbErr)
		}
		e.broadcastExecutionFailed(execution)
		return err
	}

	// Check if this is sync mode or archive mode
	if task.ArchiveOptions.Format == "sync" {
		// Sync mode: upload files directly without creating archive
		return e.runSyncExecution(ctx, task, execution, sourcePath, startTime)
	}

	// Archive mode: create archive then upload
	// Create archive
	log.Printf("Creating archive for task: %s (source: %s)", task.Name, sourcePath)
	builder := archive.NewBuilder(
		sourcePath,
		tempDir,
		task.ArchiveOptions,
		func(current, total int64, file string) {
			// Broadcast archive progress
			e.broadcastEvent(models.ProgressEvent{
				Type: "archive_progress",
				Data: models.ArchiveProgress{
					ExecutionID:     execution.ID,
					Phase:           "creating_archive",
					ProgressPercent: float64(current) / float64(total) * 100,
					CurrentFile:     file,
					BytesProcessed:  current,
					BytesTotal:      total,
				},
			})
		},
	)

	archivePath, hash, size, err := builder.Build(task.Name)
	if err != nil {
		execution.Status = "failed"
		execution.ErrorMessage = fmt.Sprintf("Failed to create archive: %v", err)
		now := time.Now()
		execution.CompletedAt = &now
		execution.DurationMs = time.Since(startTime).Milliseconds()
		if dbErr := e.db.UpdateExecution(execution); dbErr != nil {
			log.Printf("Error updating execution: %v", dbErr)
		}
		e.broadcastExecutionFailed(execution)
		return err
	}

	// Update execution with archive info
	execution.ArchiveSize = size
	execution.ArchiveHash = hash

	// Clean up archive on completion
	defer func() {
		if err := os.Remove(archivePath); err != nil {
			log.Printf("Error removing archive file: %v", err)
		}
	}()

	// Upload to all configured backends
	log.Printf("Uploading to %d backend(s)", len(task.BackendIDs))
	var backendResults []models.BackendResult
	var uploadErrors []error

	for _, backendID := range task.BackendIDs {
		result := e.uploadToBackend(ctx, backendID, task, archivePath, execution)
		backendResults = append(backendResults, result)

		// Store backend upload result
		if dbErr := e.db.AddBackendUpload(execution.ID, &result); dbErr != nil {
			log.Printf("Error adding backend upload: %v", dbErr)
		}

		if result.Status == "failed" {
			uploadErrors = append(uploadErrors, fmt.Errorf("backend %s: %s", result.BackendName, result.ErrorMessage))
		}
	}

	execution.BackendResults = backendResults

	// Determine overall status
	if len(uploadErrors) == len(task.BackendIDs) {
		// All uploads failed
		execution.Status = "failed"
		// Include detailed error messages
		errorDetails := make([]string, len(uploadErrors))
		for i, err := range uploadErrors {
			errorDetails[i] = err.Error()
		}
		execution.ErrorMessage = fmt.Sprintf("All backend uploads failed: %s", strings.Join(errorDetails, "; "))
	} else if len(uploadErrors) > 0 {
		// Some uploads failed
		execution.Status = "success"
		errorDetails := make([]string, len(uploadErrors))
		for i, err := range uploadErrors {
			errorDetails[i] = err.Error()
		}
		execution.ErrorMessage = fmt.Sprintf("%d of %d backends failed: %s", len(uploadErrors), len(task.BackendIDs), strings.Join(errorDetails, "; "))
	} else {
		// All succeeded
		execution.Status = "success"
	}

	// Complete execution
	now := time.Now()
	execution.CompletedAt = &now
	execution.DurationMs = time.Since(startTime).Milliseconds()
	if dbErr := e.db.UpdateExecution(execution); dbErr != nil {
		log.Printf("Error updating execution: %v", dbErr)
	}

	// Update task's last run time
	if err := e.config.UpdateTaskSchedule(task.ID, &now, nil); err != nil {
		log.Printf("Error updating task schedule: %v", err)
	}

	// Apply retention policy if configured
	if task.RetentionPolicy.KeepLast > 0 {
		e.applyRetentionPolicy(ctx, task, backendResults)
	}

	// Broadcast completion
	e.broadcastEvent(models.ProgressEvent{
		Type: "execution_completed",
		Data: map[string]interface{}{
			"execution_id":       execution.ID,
			"task_id":            task.ID,
			"status":             execution.Status,
			"completed_at":       execution.CompletedAt,
			"duration_ms":        execution.DurationMs,
			"archive_size":       execution.ArchiveSize,
			"backends_succeeded": len(task.BackendIDs) - len(uploadErrors),
			"backends_failed":    len(uploadErrors),
		},
	})

	return nil
}

// runSyncExecution performs file-by-file sync execution
func (e *Executor) runSyncExecution(ctx context.Context, task *models.Task, execution *models.Execution, sourcePath string, startTime time.Time) error {
	log.Printf("Starting sync for task: %s (source: %s)", task.Name, sourcePath)

	// Sync to all configured backends
	var backendResults []models.BackendResult
	var syncErrors []error
	var totalBytesUploaded int64

	for _, backendID := range task.BackendIDs {
		result := e.syncToBackend(ctx, backendID, task, sourcePath, execution)
		backendResults = append(backendResults, result)

		// Store backend upload result
		if dbErr := e.db.AddBackendUpload(execution.ID, &result); dbErr != nil {
			log.Printf("Error adding backend upload: %v", dbErr)
		}

		if result.Status == "failed" {
			syncErrors = append(syncErrors, fmt.Errorf("backend %s: %s", result.BackendName, result.ErrorMessage))
		} else {
			totalBytesUploaded += result.Size
		}
	}

	execution.BackendResults = backendResults
	execution.ArchiveSize = totalBytesUploaded // Use total synced size

	// Determine overall status
	if len(syncErrors) == len(task.BackendIDs) {
		// All syncs failed
		execution.Status = "failed"
		errorDetails := make([]string, len(syncErrors))
		for i, err := range syncErrors {
			errorDetails[i] = err.Error()
		}
		execution.ErrorMessage = fmt.Sprintf("All backend syncs failed: %s", strings.Join(errorDetails, "; "))
	} else if len(syncErrors) > 0 {
		// Some syncs failed
		execution.Status = "success"
		errorDetails := make([]string, len(syncErrors))
		for i, err := range syncErrors {
			errorDetails[i] = err.Error()
		}
		execution.ErrorMessage = fmt.Sprintf("%d of %d backends failed: %s", len(syncErrors), len(task.BackendIDs), strings.Join(errorDetails, "; "))
	} else {
		// All succeeded
		execution.Status = "success"
	}

	// Complete execution
	now := time.Now()
	execution.CompletedAt = &now
	execution.DurationMs = time.Since(startTime).Milliseconds()
	if dbErr := e.db.UpdateExecution(execution); dbErr != nil {
		log.Printf("Error updating execution: %v", dbErr)
	}

	// Update task's last run time
	if err := e.config.UpdateTaskSchedule(task.ID, &now, nil); err != nil {
		log.Printf("Error updating task schedule: %v", err)
	}

	// Note: Retention policy doesn't apply to sync mode

	// Broadcast completion
	e.broadcastEvent(models.ProgressEvent{
		Type: "execution_completed",
		Data: map[string]interface{}{
			"execution_id":       execution.ID,
			"task_id":            task.ID,
			"status":             execution.Status,
			"completed_at":       execution.CompletedAt,
			"duration_ms":        execution.DurationMs,
			"archive_size":       execution.ArchiveSize,
			"backends_succeeded": len(task.BackendIDs) - len(syncErrors),
			"backends_failed":    len(syncErrors),
		},
	})

	return nil
}

// syncToBackend syncs files to a specific backend
func (e *Executor) syncToBackend(ctx context.Context, backendID string, task *models.Task, sourcePath string, execution *models.Execution) models.BackendResult {
	result := models.BackendResult{
		BackendID: backendID,
	}

	// Get backend configuration
	backendCfg, err := e.config.GetBackend(backendID)
	if err != nil {
		result.Status = "failed"
		result.ErrorMessage = fmt.Sprintf("Backend not found: %v", err)
		return result
	}

	result.BackendName = backendCfg.Name

	// Create backend instance
	backendInstance, err := backend.Factory(backendCfg, e.config)
	if err != nil {
		result.Status = "failed"
		result.ErrorMessage = fmt.Sprintf("Failed to create backend: %v", err)
		return result
	}
	defer func() {
		if err := backendInstance.Close(); err != nil {
			log.Printf("Error closing backend instance: %v", err)
		}
	}()

	// Generate remote path (use task name as folder)
	remotePath := task.Name

	// Add backend prefix if configured
	if prefix, ok := backendCfg.Config["prefix"].(string); ok && prefix != "" {
		remotePath = filepath.Join(prefix, remotePath)
	}

	// Create syncer
	log.Printf("Syncing to backend: %s (remote path: %s)", backendCfg.Name, remotePath)
	syncer := filesync.NewSyncer(
		sourcePath,
		backendInstance,
		remotePath,
		task.ArchiveOptions.SyncOptions,
		func(phase string, current, total int, file string) {
			// Broadcast sync progress
			percent := 0.0
			if total > 0 {
				percent = float64(current) / float64(total) * 100
			}

			e.broadcastEvent(models.ProgressEvent{
				Type: "sync_progress",
				Data: map[string]interface{}{
					"execution_id":     execution.ID,
					"backend_id":       backendID,
					"backend_name":     backendCfg.Name,
					"phase":            phase,
					"progress_percent": percent,
					"current_file":     file,
					"files_processed":  current,
					"files_total":      total,
				},
			})
		},
	)

	// Perform sync
	syncResult, err := syncer.Sync(ctx)
	if err != nil {
		result.Status = "failed"
		result.ErrorMessage = err.Error()
		return result
	}

	// Check for errors during sync
	if len(syncResult.Errors) > 0 {
		result.Status = "failed"
		errorMsgs := make([]string, len(syncResult.Errors))
		for i, err := range syncResult.Errors {
			errorMsgs[i] = err.Error()
		}
		result.ErrorMessage = strings.Join(errorMsgs, "; ")
		return result
	}

	// Success
	now := time.Now()
	result.Status = "success"
	result.UploadedAt = &now
	result.Size = syncResult.BytesUploaded
	result.RemotePath = remotePath

	log.Printf("Successfully synced to backend: %s (%d files uploaded, %d deleted, %d skipped)",
		backendCfg.Name, syncResult.FilesUploaded, syncResult.FilesDeleted, syncResult.FilesSkipped)
	return result
}

// uploadToBackend uploads the archive to a specific backend
func (e *Executor) uploadToBackend(ctx context.Context, backendID string, task *models.Task, archivePath string, execution *models.Execution) models.BackendResult {
	result := models.BackendResult{
		BackendID: backendID,
	}

	// Get backend configuration
	backendCfg, err := e.config.GetBackend(backendID)
	if err != nil {
		result.Status = "failed"
		result.ErrorMessage = fmt.Sprintf("Backend not found: %v", err)
		return result
	}

	result.BackendName = backendCfg.Name

	// Create backend instance
	backendInstance, err := backend.Factory(backendCfg, e.config)
	if err != nil {
		result.Status = "failed"
		result.ErrorMessage = fmt.Sprintf("Failed to create backend: %v", err)
		return result
	}
	defer func() {
		if err := backendInstance.Close(); err != nil {
			log.Printf("Error closing backend instance: %v", err)
		}
	}()

	// Generate remote path (base filename only - backends handle their own prefixes)
	remotePath := filepath.Base(archivePath)

	// Upload with progress
	log.Printf("Uploading to backend: %s", backendCfg.Name)
	err = backendInstance.Upload(ctx, archivePath, remotePath, func(uploaded, total int64) {
		e.broadcastEvent(models.ProgressEvent{
			Type: "upload_progress",
			Data: models.UploadProgress{
				ExecutionID:     execution.ID,
				BackendID:       backendID,
				BackendName:     backendCfg.Name,
				ProgressPercent: float64(uploaded) / float64(total) * 100,
				BytesUploaded:   uploaded,
				BytesTotal:      total,
			},
		})
	})

	if err != nil {
		result.Status = "failed"
		result.ErrorMessage = err.Error()
		return result
	}

	// Success
	now := time.Now()
	result.Status = "success"
	result.UploadedAt = &now
	result.Size = execution.ArchiveSize
	result.RemotePath = remotePath

	log.Printf("Successfully uploaded to backend: %s", backendCfg.Name)
	return result
}

// applyRetentionPolicy removes old backups according to retention policy
func (e *Executor) applyRetentionPolicy(ctx context.Context, task *models.Task, backendResults []models.BackendResult) {
	for _, result := range backendResults {
		if result.Status != "success" {
			continue
		}

		// Get backend
		backendCfg, err := e.config.GetBackend(result.BackendID)
		if err != nil {
			continue
		}

		backendInstance, err := backend.Factory(backendCfg, e.config)
		if err != nil {
			continue
		}
		defer func() {
		if err := backendInstance.Close(); err != nil {
			log.Printf("Error closing backend instance: %v", err)
		}
	}()

		// List backups for this task
		prefix := result.RemotePath
		// Get the directory part for listing similar files
		prefix = filepath.Dir(prefix)
		if prefix == "." {
			prefix = ""
		}

		allFiles, err := backendInstance.List(ctx, prefix)
		if err != nil {
			log.Printf("Failed to list backups for retention: %v", err)
			continue
		}

		// Filter to only include files matching this task's backup pattern
		// Backup files follow pattern: <taskname>_YYYYMMDD_HHMMSS.tar.gz
		var backups []backend.BackupInfo
		taskPrefix := task.Name + "_"
		for _, file := range allFiles {
			fileName := filepath.Base(file.Path)
			// Only consider files that start with task name and end with .tar.gz
			if len(fileName) > len(taskPrefix) &&
				fileName[:len(taskPrefix)] == taskPrefix &&
				filepath.Ext(fileName) == ".gz" {
				backups = append(backups, file)
			}
		}

		// If we have more than KeepLast, delete oldest
		if len(backups) > task.RetentionPolicy.KeepLast {
			// Sort by last modified (oldest first)
			// For now, delete excess backups
			toDelete := len(backups) - task.RetentionPolicy.KeepLast
			for i := 0; i < toDelete; i++ {
				if err := backendInstance.Delete(ctx, backups[i].Path); err != nil {
					log.Printf("Failed to delete old backup %s: %v", backups[i].Path, err)
				} else {
					log.Printf("Deleted old backup: %s", backups[i].Path)
				}
			}
		}
	}
}

// Cancel cancels a running execution
func (e *Executor) Cancel(executionID string) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, running := range e.running {
		if running.ID == executionID {
			running.Cancel()
			return nil
		}
	}

	return fmt.Errorf("execution not found or not running")
}

// IsRunning checks if a task is currently running
func (e *Executor) IsRunning(taskID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, exists := e.running[taskID]
	return exists
}

// GetRunningExecutions returns all running executions
func (e *Executor) GetRunningExecutions() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var ids []string
	for _, running := range e.running {
		ids = append(ids, running.ID)
	}
	return ids
}

// broadcastEvent broadcasts a progress event
func (e *Executor) broadcastEvent(event models.ProgressEvent) {
	if e.progress != nil {
		e.progress.BroadcastProgress(event)
	}
}

// broadcastExecutionFailed broadcasts an execution failed event
func (e *Executor) broadcastExecutionFailed(execution *models.Execution) {
	e.broadcastEvent(models.ProgressEvent{
		Type: "execution_failed",
		Data: map[string]interface{}{
			"execution_id":  execution.ID,
			"task_id":       execution.TaskID,
			"status":        execution.Status,
			"completed_at":  execution.CompletedAt,
			"error_message": execution.ErrorMessage,
		},
	})
}
