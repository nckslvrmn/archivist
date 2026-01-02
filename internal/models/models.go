package models

import "time"

// Config represents the complete application configuration
type Config struct {
	Version  string    `json:"version"`
	Backends []Backend `json:"backends"`
	Tasks    []Task    `json:"tasks"`
	Settings Settings  `json:"settings"`
}

// Backend represents a storage backend configuration
type Backend struct {
	ID             string                 `json:"id"`
	Type           string                 `json:"type"` // s3, gcs, gdrive, azure, b2, local
	Name           string                 `json:"name"`
	Config         map[string]interface{} `json:"config"`
	Enabled        bool                   `json:"enabled"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	LastTest       *time.Time             `json:"last_test,omitempty"`
	LastTestStatus string                 `json:"last_test_status,omitempty"`
}

// Task represents a backup task configuration
type Task struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	SourcePath      string          `json:"source_path"`
	BackendIDs      []string        `json:"backend_ids"`
	Schedule        Schedule        `json:"schedule"`
	ArchiveOptions  ArchiveOptions  `json:"archive_options"`
	RetentionPolicy RetentionPolicy `json:"retention_policy"`
	Enabled         bool            `json:"enabled"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	LastRun         *time.Time      `json:"last_run,omitempty"`
	NextRun         *time.Time      `json:"next_run,omitempty"`
}

// Schedule represents a task schedule configuration
type Schedule struct {
	Type       string `json:"type"`                  // simple, cron, manual
	SimpleType string `json:"simple_type,omitempty"` // hourly, daily, weekly, monthly
	CronExpr   string `json:"cron_expr,omitempty"`
}

// ArchiveOptions represents archive creation options
type ArchiveOptions struct {
	Format       string      `json:"format"`        // tar.gz, tar.bz2, tar.xz, tar.zst, zip, sync
	Compression  string      `json:"compression"`   // none, gzip, bzip2, xz, zstd
	NamePattern  string      `json:"name_pattern"`  // e.g., "{task}_{timestamp}.tar.gz" or "{task}_latest.tar.gz"
	UseTimestamp bool        `json:"use_timestamp"` // If false, creates static filename (mirror strategy)
	SyncOptions  SyncOptions `json:"sync_options"`  // Options for sync mode
}

// SyncOptions represents file-by-file sync options
type SyncOptions struct {
	CompareMethod string `json:"compare_method"` // hash, mtime (hash = slower/accurate, mtime = faster/less accurate)
	DeleteRemote  bool   `json:"delete_remote"`  // If true, delete remote files not in source (true mirror)
}

// RetentionPolicy represents backup retention configuration
type RetentionPolicy struct {
	KeepLast int `json:"keep_last"` // Number of backups to keep (0 = unlimited)
}

// Settings represents application settings
type Settings struct {
	TempDir            string `json:"temp_dir"`
	SourcesDir         string `json:"sources_dir"`
	MaxConcurrentTasks int    `json:"max_concurrent_tasks"`
	LogLevel           string `json:"log_level"`
}

// Execution represents a backup task execution record
type Execution struct {
	ID             string          `json:"id"`
	TaskID         string          `json:"task_id"`
	TaskName       string          `json:"task_name"`
	StartedAt      time.Time       `json:"started_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	Status         string          `json:"status"` // running, success, failed, cancelled
	ArchiveSize    int64           `json:"archive_size,omitempty"`
	ArchiveHash    string          `json:"archive_hash,omitempty"`
	BackendResults []BackendResult `json:"backend_results,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	DurationMs     int64           `json:"duration_ms,omitempty"`
}

// BackendResult represents the result of uploading to a backend
type BackendResult struct {
	BackendID    string     `json:"backend_id"`
	BackendName  string     `json:"backend_name"`
	Status       string     `json:"status"` // success, failed
	UploadedAt   *time.Time `json:"uploaded_at,omitempty"`
	Size         int64      `json:"size,omitempty"`
	RemotePath   string     `json:"remote_path,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
}

// TaskStats represents statistics for a task
type TaskStats struct {
	TotalExecutions     int    `json:"total_executions"`
	SuccessCount        int    `json:"success_count"`
	FailureCount        int    `json:"failure_count"`
	LastExecutionStatus string `json:"last_execution_status"`
	AverageDurationMs   int64  `json:"average_duration_ms"`
	LastArchiveSize     int64  `json:"last_archive_size"`
}

// SourceInfo represents information about a source directory
type SourceInfo struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Type       string `json:"type"` // symlink, directory
	Target     string `json:"target,omitempty"`
	Size       int64  `json:"size"`
	FileCount  int    `json:"file_count"`
	Accessible bool   `json:"accessible"`
}

// StorageUsage represents storage usage information
type StorageUsage struct {
	Used  int64 `json:"used"`
	Total int64 `json:"total"` // -1 if unlimited
}

// SystemStats represents system statistics
type SystemStats struct {
	Tasks      TasksStats      `json:"tasks"`
	Backends   BackendsStats   `json:"backends"`
	Executions ExecutionsStats `json:"executions"`
	Storage    StorageStats    `json:"storage"`
	System     SystemInfo      `json:"system"`
}

// TasksStats represents task statistics
type TasksStats struct {
	Total    int `json:"total"`
	Enabled  int `json:"enabled"`
	Disabled int `json:"disabled"`
}

// BackendsStats represents backend statistics
type BackendsStats struct {
	Total    int `json:"total"`
	Enabled  int `json:"enabled"`
	Disabled int `json:"disabled"`
}

// ExecutionsStats represents execution statistics
type ExecutionsStats struct {
	Total   int `json:"total"`
	Success int `json:"success"`
	Failed  int `json:"failed"`
	Running int `json:"running"`
	Last24h int `json:"last_24h"`
}

// StorageStats represents storage statistics
type StorageStats struct {
	TempUsed      int64 `json:"temp_used"`
	TempAvailable int64 `json:"temp_available"`
}

// SystemInfo represents system information
type SystemInfo struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryUsed  int64   `json:"memory_used"`
	MemoryTotal int64   `json:"memory_total"`
	Goroutines  int     `json:"goroutines"`
}

// ProgressEvent represents a progress update event
type ProgressEvent struct {
	Type string      `json:"type"` // execution_started, archive_progress, upload_progress, execution_completed, execution_failed
	Data interface{} `json:"data"`
}

// ArchiveProgress represents archive creation progress
type ArchiveProgress struct {
	ExecutionID     string  `json:"execution_id"`
	Phase           string  `json:"phase"` // creating_archive
	ProgressPercent float64 `json:"progress_percent"`
	CurrentFile     string  `json:"current_file"`
	FilesProcessed  int     `json:"files_processed"`
	FilesTotal      int     `json:"files_total"`
	BytesProcessed  int64   `json:"bytes_processed"`
	BytesTotal      int64   `json:"bytes_total"`
}

// UploadProgress represents upload progress to a backend
type UploadProgress struct {
	ExecutionID      string  `json:"execution_id"`
	BackendID        string  `json:"backend_id"`
	BackendName      string  `json:"backend_name"`
	ProgressPercent  float64 `json:"progress_percent"`
	BytesUploaded    int64   `json:"bytes_uploaded"`
	BytesTotal       int64   `json:"bytes_total"`
	SpeedBytesPerSec int64   `json:"speed_bytes_per_sec"`
}

// DryRunResult represents the result of a dry run operation
type DryRunResult struct {
	TaskID         string          `json:"task_id"`
	TaskName       string          `json:"task_name"`
	Mode           string          `json:"mode"` // archive or sync
	SourcePath     string          `json:"source_path"`
	FilesSummary   FilesSummary    `json:"files_summary"`
	ArchiveDetails *ArchiveDetails `json:"archive_details,omitempty"`
	SyncDetails    *SyncDetails    `json:"sync_details,omitempty"`
	BackendPlans   []BackendPlan   `json:"backend_plans"`
	AnalyzedAt     time.Time       `json:"analyzed_at"`
	DurationMs     int64           `json:"duration_ms"`
	Errors         []string        `json:"errors,omitempty"`
}

// FilesSummary summarizes files to be backed up
type FilesSummary struct {
	TotalFiles      int            `json:"total_files"`
	TotalSize       int64          `json:"total_size"`
	TotalDirs       int            `json:"total_dirs"`
	LargestFile     string         `json:"largest_file"`
	LargestFileSize int64          `json:"largest_file_size"`
	FileTypes       map[string]int `json:"file_types"` // extension -> count
	TopFiles        []FileDetail   `json:"top_files"`  // Top 10 largest files
}

// ArchiveDetails provides details about archive that would be created
type ArchiveDetails struct {
	EstimatedArchiveSize int64   `json:"estimated_archive_size"`
	CompressionRatio     float64 `json:"compression_ratio"`
	Format               string  `json:"format"`
	ArchiveName          string  `json:"archive_name"`
}

// SyncDetails provides details about what would be synced
type SyncDetails struct {
	FilesToUpload []FileDetail `json:"files_to_upload"`
	FilesToDelete []string     `json:"files_to_delete"`
	FilesToSkip   []FileDetail `json:"files_to_skip"`
	BytesToUpload int64        `json:"bytes_to_upload"`
	UploadCount   int          `json:"upload_count"`
	DeleteCount   int          `json:"delete_count"`
	SkipCount     int          `json:"skip_count"`
}

// FileDetail describes a file operation
type FileDetail struct {
	RelativePath string    `json:"relative_path"`
	Size         int64     `json:"size"`
	ModTime      time.Time `json:"mod_time"`
	Hash         string    `json:"hash,omitempty"`
	Reason       string    `json:"reason"` // Why action would be taken
}

// BackendPlan describes what would happen with a backend
type BackendPlan struct {
	BackendID    string `json:"backend_id"`
	BackendName  string `json:"backend_name"`
	BackendType  string `json:"backend_type"`
	RemotePath   string `json:"remote_path"`
	Available    bool   `json:"available"`
	ErrorMessage string `json:"error_message,omitempty"`
}
