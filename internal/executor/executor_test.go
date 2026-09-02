package executor

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nsilverman/archivist/internal/config"
	"github.com/nsilverman/archivist/internal/models"
	"github.com/nsilverman/archivist/internal/storage"
)

// newTestExecutor wires a real config manager, SQLite database and local
// backend over a temp root, and returns the executor plus the task ID.
func newTestExecutor(t *testing.T, sourceBytes int) (*Executor, *storage.Database, string) {
	t.Helper()

	root := t.TempDir()
	tempDir := filepath.Join(root, "temp")
	sourcesDir := filepath.Join(root, "sources")

	cfg, err := config.NewManager(filepath.Join(root, "config", "config.json"), root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := cfg.CreateDefaultWithPaths(tempDir, sourcesDir); err != nil {
		t.Fatalf("CreateDefaultWithPaths: %v", err)
	}

	// Source directory with incompressible data, so a run takes long enough
	// for a second trigger to overlap it.
	src := filepath.Join(sourcesDir, "data")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	const chunk = 256 * 1024
	for written := 0; written < sourceBytes; written += chunk {
		buf := make([]byte, chunk)
		if _, err := rand.Read(buf); err != nil {
			t.Fatal(err)
		}
		name := filepath.Join(src, fmt.Sprintf("f%04d.bin", written/chunk))
		if err := os.WriteFile(name, buf, 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := cfg.AddBackend(&models.Backend{
		ID: "local1", Type: "local", Name: "local1", Enabled: true,
		Config: map[string]interface{}{"path": filepath.Join(root, "backups")},
	}); err != nil {
		t.Fatalf("AddBackend: %v", err)
	}

	task := &models.Task{
		ID:         "task1",
		Name:       "Test Task",
		SourcePath: "sources/data",
		BackendIDs: []string{"local1"},
		Schedule:   models.Schedule{Type: "manual"},
		ArchiveOptions: models.ArchiveOptions{
			Format: "tar.gz", Compression: "gzip", UseTimestamp: true,
		},
		Enabled: true,
	}
	if err := cfg.AddTask(task); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	db, err := storage.NewDatabase(filepath.Join(root, "config", "archivist.db"))
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	return NewExecutor(cfg, db), db, task.ID
}

func waitForCompletion(t *testing.T, e *Executor, taskID string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if !e.IsRunning(taskID) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("execution did not finish within the deadline")
}

// TestExecuteRejectsConcurrentRuns is the regression test for the duplicate
// run race: the running check and the running insert have to happen under one
// exclusive lock, or a cron firing and a manual trigger both start the same
// task against the same temp file and backends.
func TestExecuteRejectsConcurrentRuns(t *testing.T) {
	e, db, taskID := newTestExecutor(t, 8*1024*1024)

	const callers = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	var accepted int
	var rejected int

	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := e.Execute(taskID)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				accepted++
			} else {
				rejected++
			}
		}()
	}
	close(start)
	wg.Wait()

	if accepted != 1 {
		t.Errorf("accepted %d concurrent Execute calls, want exactly 1 (rejected %d)", accepted, rejected)
	}

	waitForCompletion(t, e, taskID)

	// Exactly one execution row must exist: a rejected call must not leave a
	// record behind either.
	execs, err := db.ListExecutions(taskID, "", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(execs) != accepted {
		t.Errorf("recorded %d executions, want %d", len(execs), accepted)
	}
	if len(execs) == 1 && execs[0].Status != "success" {
		t.Errorf("execution status = %s (%s), want success", execs[0].Status, execs[0].ErrorMessage)
	}
}

// TestExecuteAfterCompletion confirms the slot is released for the next run.
func TestExecuteAfterCompletion(t *testing.T) {
	e, db, taskID := newTestExecutor(t, 256*1024)

	for i := 0; i < 2; i++ {
		if _, err := e.Execute(taskID); err != nil {
			t.Fatalf("run %d: Execute: %v", i+1, err)
		}
		waitForCompletion(t, e, taskID)
	}

	execs, err := db.ListExecutions(taskID, "", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(execs) != 2 {
		t.Fatalf("recorded %d executions, want 2", len(execs))
	}
	for _, exec := range execs {
		if exec.Status != "success" {
			t.Errorf("execution %s status = %s (%s), want success", exec.ID, exec.Status, exec.ErrorMessage)
		}
	}
}

// TestCancelMarksExecutionCancelled: cancelling during the archive phase must
// stop the run and record it as cancelled rather than failed.
func TestCancelMarksExecutionCancelled(t *testing.T) {
	e, db, taskID := newTestExecutor(t, 32*1024*1024)

	executionID, err := e.Execute(taskID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Cancel while the archive is still being written.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := e.Cancel(executionID); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("could not cancel execution before it finished")
		}
		time.Sleep(5 * time.Millisecond)
	}

	waitForCompletion(t, e, taskID)

	exec, err := db.GetExecution(executionID)
	if err != nil {
		t.Fatal(err)
	}
	if exec.Status != "cancelled" {
		t.Errorf("status = %s (%s), want cancelled", exec.Status, exec.ErrorMessage)
	}
}

func TestPercent(t *testing.T) {
	cases := []struct {
		current, total int64
		want           float64
	}{
		{0, 0, 0},   // empty source: must not produce NaN
		{5, 0, 0},   // unknown total
		{-1, -1, 0}, // nonsense input
		{50, 200, 25},
		{200, 200, 100},
	}
	for _, c := range cases {
		if got := percent(c.current, c.total); got != c.want {
			t.Errorf("percent(%d, %d) = %v, want %v", c.current, c.total, got, c.want)
		}
	}
}

// TestTempDirCleanedUp: a finished run must not leave archives in temp.
func TestTempDirCleanedUp(t *testing.T) {
	e, _, taskID := newTestExecutor(t, 256*1024)

	if _, err := e.Execute(taskID); err != nil {
		t.Fatal(err)
	}
	waitForCompletion(t, e, taskID)

	tempDir := e.config.ResolvePath(e.config.GetSettings().TempDir)
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("temp dir not cleaned up, contains: %v", names)
	}
}
