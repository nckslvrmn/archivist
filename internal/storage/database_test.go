package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nsilverman/archivist/internal/models"
)

func newTestDB(t *testing.T) *Database {
	t.Helper()
	db, err := NewDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return db
}

// TestReconcileRunningExecutions is the regression test for executions left
// in "running" by a restart, which otherwise skew the dashboard forever.
func TestReconcileRunningExecutions(t *testing.T) {
	db := newTestDB(t)

	running := &models.Execution{
		ID: "exec-running", TaskID: "t1", TaskName: "t1",
		StartedAt: time.Now().Add(-time.Hour), Status: "running",
	}
	if err := db.CreateExecution(running); err != nil {
		t.Fatal(err)
	}

	completed := time.Now().Add(-30 * time.Minute)
	done := &models.Execution{
		ID: "exec-done", TaskID: "t1", TaskName: "t1",
		StartedAt: time.Now().Add(-time.Hour), Status: "success", CompletedAt: &completed,
	}
	if err := db.CreateExecution(done); err != nil {
		t.Fatal(err)
	}

	n, err := db.ReconcileRunningExecutions()
	if err != nil {
		t.Fatalf("ReconcileRunningExecutions: %v", err)
	}
	if n != 1 {
		t.Errorf("reconciled %d executions, want 1", n)
	}

	got, err := db.GetExecution("exec-running")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" {
		t.Errorf("status = %s, want failed", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt is nil, want a timestamp")
	}
	if got.ErrorMessage == "" {
		t.Error("ErrorMessage is empty, want an explanation")
	}

	// A finished execution must be left alone.
	untouched, err := db.GetExecution("exec-done")
	if err != nil {
		t.Fatal(err)
	}
	if untouched.Status != "success" {
		t.Errorf("completed execution status = %s, want success", untouched.Status)
	}

	// Stats must no longer report a phantom running execution.
	stats, err := db.GetExecutionStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Running != 0 {
		t.Errorf("stats.Running = %d, want 0", stats.Running)
	}
}

// TestConcurrentWrites covers the SQLITE_BUSY case: per-backend goroutines
// record their results at the same time as the execution row is updated.
func TestConcurrentWrites(t *testing.T) {
	db := newTestDB(t)

	exec := &models.Execution{
		ID: "exec-1", TaskID: "t1", TaskName: "t1",
		StartedAt: time.Now(), Status: "running",
	}
	if err := db.CreateExecution(exec); err != nil {
		t.Fatal(err)
	}

	const writers = 16
	errs := make(chan error, writers*2)
	for i := 0; i < writers; i++ {
		go func(i int) {
			uploadedAt := time.Now()
			errs <- db.AddBackendUpload("exec-1", &models.BackendResult{
				BackendID: "b1", BackendName: "b1", Status: "success",
				UploadedAt: &uploadedAt, Size: int64(i),
			})
		}(i)
		go func() {
			_, err := db.ListExecutions("", "", 10, 0)
			errs <- err
		}()
	}
	for i := 0; i < writers*2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent operation failed: %v", err)
		}
	}

	got, err := db.GetExecution("exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.BackendResults) != writers {
		t.Errorf("recorded %d backend results, want %d", len(got.BackendResults), writers)
	}
}

// TestForeignKeysEnforced confirms the pragma actually took effect, so the
// backend_uploads → executions constraint is real rather than decorative.
func TestForeignKeysEnforced(t *testing.T) {
	db := newTestDB(t)

	uploadedAt := time.Now()
	err := db.AddBackendUpload("no-such-execution", &models.BackendResult{
		BackendID: "b1", BackendName: "b1", Status: "success", UploadedAt: &uploadedAt,
	})
	if err == nil {
		t.Fatal("insert with a dangling execution_id succeeded, want a foreign key error")
	}
}

// TestPruneExecutions covers the history retention window.
func TestPruneExecutions(t *testing.T) {
	db := newTestDB(t)

	now := time.Now()
	rows := []struct {
		id     string
		age    time.Duration
		status string
	}{
		{"old-1", 100 * 24 * time.Hour, "success"},
		{"old-2", 91 * 24 * time.Hour, "failed"},
		{"recent", 10 * 24 * time.Hour, "success"},
		{"old-running", 200 * 24 * time.Hour, "running"},
	}
	for _, r := range rows {
		if err := db.CreateExecution(&models.Execution{
			ID: r.id, TaskID: "t1", TaskName: "t1",
			StartedAt: now.Add(-r.age), Status: r.status,
		}); err != nil {
			t.Fatal(err)
		}
		uploadedAt := now.Add(-r.age)
		if err := db.AddBackendUpload(r.id, &models.BackendResult{
			BackendID: "b1", BackendName: "b1", Status: "success", UploadedAt: &uploadedAt,
		}); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := db.PruneExecutions(now.AddDate(0, 0, -90))
	if err != nil {
		t.Fatalf("PruneExecutions: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed %d executions, want 2", removed)
	}

	for _, id := range []string{"old-1", "old-2"} {
		if _, err := db.GetExecution(id); err == nil {
			t.Errorf("execution %s still present after pruning", id)
		}
	}

	// A recent execution keeps its uploads.
	recent, err := db.GetExecution("recent")
	if err != nil {
		t.Fatalf("recent execution was pruned: %v", err)
	}
	if len(recent.BackendResults) != 1 {
		t.Errorf("recent execution has %d backend results, want 1", len(recent.BackendResults))
	}

	// An in-flight run is never pruned, however old it looks.
	if _, err := db.GetExecution("old-running"); err != nil {
		t.Errorf("running execution was pruned: %v", err)
	}

	// The pruned executions' upload rows went with them.
	var orphans int
	if err := db.db.QueryRow(`
		SELECT COUNT(*) FROM backend_uploads
		WHERE execution_id NOT IN (SELECT id FROM executions)
	`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("%d orphaned backend_uploads rows remain", orphans)
	}
}

func TestPruneExecutionsNoMatches(t *testing.T) {
	db := newTestDB(t)
	if err := db.CreateExecution(&models.Execution{
		ID: "e1", TaskID: "t1", TaskName: "t1", StartedAt: time.Now(), Status: "success",
	}); err != nil {
		t.Fatal(err)
	}

	removed, err := db.PruneExecutions(time.Now().AddDate(0, 0, -90))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("removed %d executions, want 0", removed)
	}
}
