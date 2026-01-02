package storage

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nsilverman/archivist/internal/models"
)

// Database handles all database operations
type Database struct {
	db *sql.DB
}

// NewDatabase creates a new database connection
func NewDatabase(path string) (*Database, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	d := &Database{db: db}

	// Initialize schema
	if err := d.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return d, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// initSchema creates the database schema
func (d *Database) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS executions (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		task_name TEXT NOT NULL,
		started_at TIMESTAMP NOT NULL,
		completed_at TIMESTAMP,
		status TEXT NOT NULL,
		archive_size INTEGER,
		archive_hash TEXT,
		backend_results TEXT,
		error_message TEXT,
		duration_ms INTEGER
	);

	CREATE INDEX IF NOT EXISTS idx_executions_task_id ON executions(task_id);
	CREATE INDEX IF NOT EXISTS idx_executions_started_at ON executions(started_at);
	CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status);

	CREATE TABLE IF NOT EXISTS backend_uploads (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		execution_id TEXT NOT NULL,
		backend_id TEXT NOT NULL,
		backend_name TEXT NOT NULL,
		status TEXT NOT NULL,
		uploaded_at TIMESTAMP,
		size INTEGER,
		remote_path TEXT,
		error_message TEXT,
		FOREIGN KEY (execution_id) REFERENCES executions(id)
	);

	CREATE INDEX IF NOT EXISTS idx_backend_uploads_execution_id ON backend_uploads(execution_id);
	`

	_, err := d.db.Exec(schema)
	return err
}

// CreateExecution creates a new execution record
func (d *Database) CreateExecution(exec *models.Execution) error {
	query := `
		INSERT INTO executions (
			id, task_id, task_name, started_at, completed_at, status,
			archive_size, archive_hash, backend_results, error_message, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := d.db.Exec(query,
		exec.ID,
		exec.TaskID,
		exec.TaskName,
		exec.StartedAt,
		exec.CompletedAt,
		exec.Status,
		exec.ArchiveSize,
		exec.ArchiveHash,
		nil, // backend_results stored separately
		exec.ErrorMessage,
		exec.DurationMs,
	)

	return err
}

// UpdateExecution updates an existing execution record
func (d *Database) UpdateExecution(exec *models.Execution) error {
	query := `
		UPDATE executions SET
			completed_at = ?,
			status = ?,
			archive_size = ?,
			archive_hash = ?,
			error_message = ?,
			duration_ms = ?
		WHERE id = ?
	`

	_, err := d.db.Exec(query,
		exec.CompletedAt,
		exec.Status,
		exec.ArchiveSize,
		exec.ArchiveHash,
		exec.ErrorMessage,
		exec.DurationMs,
		exec.ID,
	)

	return err
}

// GetExecution retrieves an execution by ID
func (d *Database) GetExecution(id string) (*models.Execution, error) {
	query := `
		SELECT id, task_id, task_name, started_at, completed_at, status,
			archive_size, archive_hash, error_message, duration_ms
		FROM executions WHERE id = ?
	`

	var exec models.Execution
	var completedAt sql.NullTime
	var archiveSize sql.NullInt64
	var archiveHash, errorMessage sql.NullString
	var durationMs sql.NullInt64

	err := d.db.QueryRow(query, id).Scan(
		&exec.ID,
		&exec.TaskID,
		&exec.TaskName,
		&exec.StartedAt,
		&completedAt,
		&exec.Status,
		&archiveSize,
		&archiveHash,
		&errorMessage,
		&durationMs,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("execution not found: %s", id)
		}
		return nil, err
	}

	if completedAt.Valid {
		exec.CompletedAt = &completedAt.Time
	}
	if archiveSize.Valid {
		exec.ArchiveSize = archiveSize.Int64
	}
	if archiveHash.Valid {
		exec.ArchiveHash = archiveHash.String
	}
	if errorMessage.Valid {
		exec.ErrorMessage = errorMessage.String
	}
	if durationMs.Valid {
		exec.DurationMs = durationMs.Int64
	}

	// Load backend results
	exec.BackendResults, err = d.getBackendUploads(id)
	if err != nil {
		return nil, err
	}

	return &exec, nil
}

// ListExecutions retrieves executions with optional filtering
func (d *Database) ListExecutions(taskID string, status string, limit, offset int) ([]models.Execution, error) {
	query := `
		SELECT id, task_id, task_name, started_at, completed_at, status,
			archive_size, archive_hash, error_message, duration_ms
		FROM executions
		WHERE 1=1
	`
	args := []interface{}{}

	if taskID != "" {
		query += " AND task_id = ?"
		args = append(args, taskID)
	}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	query += " ORDER BY started_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	var executions []models.Execution
	for rows.Next() {
		var exec models.Execution
		var completedAt sql.NullTime
		var archiveSize sql.NullInt64
		var archiveHash, errorMessage sql.NullString
		var durationMs sql.NullInt64

		err := rows.Scan(
			&exec.ID,
			&exec.TaskID,
			&exec.TaskName,
			&exec.StartedAt,
			&completedAt,
			&exec.Status,
			&archiveSize,
			&archiveHash,
			&errorMessage,
			&durationMs,
		)
		if err != nil {
			return nil, err
		}

		if completedAt.Valid {
			exec.CompletedAt = &completedAt.Time
		}
		if archiveSize.Valid {
			exec.ArchiveSize = archiveSize.Int64
		}
		if archiveHash.Valid {
			exec.ArchiveHash = archiveHash.String
		}
		if errorMessage.Valid {
			exec.ErrorMessage = errorMessage.String
		}
		if durationMs.Valid {
			exec.DurationMs = durationMs.Int64
		}

		// Load backend results
		exec.BackendResults, _ = d.getBackendUploads(exec.ID)

		executions = append(executions, exec)
	}

	return executions, rows.Err()
}

// AddBackendUpload records a backend upload result
func (d *Database) AddBackendUpload(executionID string, result *models.BackendResult) error {
	query := `
		INSERT INTO backend_uploads (
			execution_id, backend_id, backend_name, status, uploaded_at,
			size, remote_path, error_message
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := d.db.Exec(query,
		executionID,
		result.BackendID,
		result.BackendName,
		result.Status,
		result.UploadedAt,
		result.Size,
		result.RemotePath,
		result.ErrorMessage,
	)

	return err
}

// getBackendUploads retrieves backend upload results for an execution
func (d *Database) getBackendUploads(executionID string) ([]models.BackendResult, error) {
	query := `
		SELECT backend_id, backend_name, status, uploaded_at, size, remote_path, error_message
		FROM backend_uploads WHERE execution_id = ?
	`

	rows, err := d.db.Query(query, executionID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	var results []models.BackendResult
	for rows.Next() {
		var result models.BackendResult
		var uploadedAt sql.NullTime
		var size sql.NullInt64
		var remotePath, errorMessage sql.NullString

		err := rows.Scan(
			&result.BackendID,
			&result.BackendName,
			&result.Status,
			&uploadedAt,
			&size,
			&remotePath,
			&errorMessage,
		)
		if err != nil {
			return nil, err
		}

		if uploadedAt.Valid {
			result.UploadedAt = &uploadedAt.Time
		}
		if size.Valid {
			result.Size = size.Int64
		}
		if remotePath.Valid {
			result.RemotePath = remotePath.String
		}
		if errorMessage.Valid {
			result.ErrorMessage = errorMessage.String
		}

		results = append(results, result)
	}

	return results, rows.Err()
}

// GetTaskStats returns statistics for a task
func (d *Database) GetTaskStats(taskID string) (*models.TaskStats, error) {
	query := `
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) as success,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed,
			AVG(CASE WHEN duration_ms IS NOT NULL THEN duration_ms ELSE 0 END) as avg_duration
		FROM executions WHERE task_id = ?
	`

	var stats models.TaskStats
	var avgDuration float64
	err := d.db.QueryRow(query, taskID).Scan(
		&stats.TotalExecutions,
		&stats.SuccessCount,
		&stats.FailureCount,
		&avgDuration,
	)
	if err != nil {
		return nil, err
	}
	stats.AverageDurationMs = int64(avgDuration)

	// Get last execution status and archive size
	lastQuery := `
		SELECT status, archive_size
		FROM executions
		WHERE task_id = ?
		ORDER BY started_at DESC
		LIMIT 1
	`

	var archiveSize sql.NullInt64
	err = d.db.QueryRow(lastQuery, taskID).Scan(&stats.LastExecutionStatus, &archiveSize)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	if archiveSize.Valid {
		stats.LastArchiveSize = archiveSize.Int64
	}

	return &stats, nil
}

// GetExecutionCount returns the count of executions matching criteria
func (d *Database) GetExecutionCount(since *time.Time, status string) (int, error) {
	query := "SELECT COUNT(*) FROM executions WHERE 1=1"
	args := []interface{}{}

	if since != nil {
		query += " AND started_at >= ?"
		args = append(args, since)
	}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	var count int
	err := d.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// GetExecutionStats returns overall execution statistics
func (d *Database) GetExecutionStats() (*models.ExecutionsStats, error) {
	query := `
		SELECT
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0) as success,
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) as failed,
			COALESCE(SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END), 0) as running
		FROM executions
	`

	var stats models.ExecutionsStats
	err := d.db.QueryRow(query).Scan(
		&stats.Total,
		&stats.Success,
		&stats.Failed,
		&stats.Running,
	)
	if err != nil {
		return nil, err
	}

	// Get count of executions in last 24 hours
	last24h := time.Now().Add(-24 * time.Hour)
	stats.Last24h, err = d.GetExecutionCount(&last24h, "")
	if err != nil {
		return nil, err
	}

	return &stats, nil
}

// ClearHistory deletes all execution records
func (d *Database) ClearHistory() error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			log.Printf("Error rolling back transaction: %v", err)
		}
	}()

	// Delete backend uploads first (foreign key constraint)
	if _, err := tx.Exec("DELETE FROM backend_uploads"); err != nil {
		return fmt.Errorf("failed to delete backend uploads: %w", err)
	}

	// Delete executions
	if _, err := tx.Exec("DELETE FROM executions"); err != nil {
		return fmt.Errorf("failed to delete executions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
