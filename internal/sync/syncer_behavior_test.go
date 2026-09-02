package sync

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsilverman/archivist/internal/backend"
	"github.com/nsilverman/archivist/internal/models"
)

// fakeBackend records uploads and deletes, and can report a hash per object.
type fakeBackend struct {
	mu      sync.Mutex
	remote  map[string]backend.BackupInfo
	uploads []string
	deletes []string
	// inFlight/maxInFlight track observed upload concurrency.
	inFlight    int
	maxInFlight int
	uploadDelay time.Duration
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{remote: make(map[string]backend.BackupInfo)}
}

func (f *fakeBackend) Initialize(map[string]interface{}, backend.PathResolver) error { return nil }
func (f *fakeBackend) Test() error                                                   { return nil }
func (f *fakeBackend) Close() error                                                  { return nil }

func (f *fakeBackend) Upload(ctx context.Context, localPath, remotePath string, progress backend.ProgressCallback) error {
	f.mu.Lock()
	f.inFlight++
	if f.inFlight > f.maxInFlight {
		f.maxInFlight = f.inFlight
	}
	f.mu.Unlock()

	if f.uploadDelay > 0 {
		select {
		case <-time.After(f.uploadDelay):
		case <-ctx.Done():
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.inFlight--
	f.uploads = append(f.uploads, remotePath)
	return ctx.Err()
}

func (f *fakeBackend) List(ctx context.Context, prefix string) ([]backend.BackupInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []backend.BackupInfo
	for _, info := range f.remote {
		if strings.HasPrefix(info.Path, prefix) {
			out = append(out, info)
		}
	}
	return out, nil
}

func (f *fakeBackend) Delete(ctx context.Context, remotePath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, remotePath)
	delete(f.remote, remotePath)
	return nil
}

func (f *fakeBackend) GetUsage(ctx context.Context) (*models.StorageUsage, error) {
	return &models.StorageUsage{}, nil
}

func (f *fakeBackend) uploadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.uploads)
}

// writeFiles creates n files of identical content and returns the source dir.
func writeFiles(t *testing.T, n int, content string) string {
	t.Helper()
	src := t.TempDir()
	for i := 0; i < n; i++ {
		name := filepath.Join(src, fmt.Sprintf("f%03d.txt", i))
		if err := os.WriteFile(name, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return src
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestSyncUploadsInParallel: uploads must overlap, not run one at a time.
func TestSyncUploadsInParallel(t *testing.T) {
	src := writeFiles(t, 24, "payload")
	fake := newFakeBackend()
	fake.uploadDelay = 20 * time.Millisecond

	s := &Syncer{SourcePath: src, Backend: fake, RemotePath: "task", Concurrency: 6}
	result, err := s.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if result.FilesUploaded != 24 {
		t.Errorf("FilesUploaded = %d, want 24", result.FilesUploaded)
	}
	if fake.uploadCount() != 24 {
		t.Errorf("backend saw %d uploads, want 24", fake.uploadCount())
	}

	fake.mu.Lock()
	peak := fake.maxInFlight
	fake.mu.Unlock()
	if peak < 2 {
		t.Errorf("peak concurrent uploads = %d, want > 1 (uploads ran sequentially)", peak)
	}
	if peak > 6 {
		t.Errorf("peak concurrent uploads = %d, exceeds the configured limit of 6", peak)
	}
}

// TestSyncRespectsConcurrencyLimit pins the bound exactly.
func TestSyncRespectsConcurrencyLimit(t *testing.T) {
	src := writeFiles(t, 20, "payload")
	fake := newFakeBackend()
	fake.uploadDelay = 10 * time.Millisecond

	s := &Syncer{SourcePath: src, Backend: fake, RemotePath: "task", Concurrency: 2}
	if _, err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.maxInFlight > 2 {
		t.Errorf("peak concurrent uploads = %d, want at most 2", fake.maxInFlight)
	}
}

// TestSyncCountsAreAccurateUnderConcurrency guards the shared counters.
func TestSyncCountsAreAccurateUnderConcurrency(t *testing.T) {
	src := writeFiles(t, 50, "same")
	fake := newFakeBackend()

	// Half the files already exist remotely with a matching hash.
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range entries {
		if i%2 == 0 {
			continue
		}
		fake.remote["task/"+e.Name()] = backend.BackupInfo{
			Path:         "task/" + e.Name(),
			Size:         int64(len("same")),
			LastModified: time.Now().Add(time.Hour).Format(time.RFC3339),
			Hash:         md5hex("same"),
			HashAlgo:     "md5",
		}
	}

	s := &Syncer{SourcePath: src, Backend: fake, RemotePath: "task", Concurrency: 8}
	result, err := s.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if result.FilesUploaded+result.FilesSkipped != 50 {
		t.Errorf("uploaded %d + skipped %d != 50", result.FilesUploaded, result.FilesSkipped)
	}
	if result.FilesSkipped != 25 {
		t.Errorf("FilesSkipped = %d, want 25", result.FilesSkipped)
	}
	if result.FilesUploaded != 25 {
		t.Errorf("FilesUploaded = %d, want 25", result.FilesUploaded)
	}
	if len(result.Errors) != 0 {
		t.Errorf("errors: %v", result.Errors)
	}
}

// TestSyncRefusesToMirrorEmptySource is the guard against an unmounted
// volume being mirrored as "everything was deleted".
func TestSyncRefusesToMirrorEmptySource(t *testing.T) {
	src := t.TempDir() // exists, but empty
	fake := newFakeBackend()
	for _, name := range []string{"task/a.txt", "task/b.txt"} {
		fake.remote[name] = backend.BackupInfo{Path: name, Size: 1, LastModified: time.Now().Format(time.RFC3339)}
	}

	s := &Syncer{
		SourcePath: src, Backend: fake, RemotePath: "task",
		Options: models.SyncOptions{DeleteRemote: true},
	}
	_, err := s.Sync(context.Background())
	if err == nil {
		t.Fatal("Sync deleted the remote copy of an empty source, want an error")
	}
	if !strings.Contains(err.Error(), "refusing to delete") {
		t.Errorf("error = %v, want it to explain the refusal", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.deletes) != 0 {
		t.Errorf("deleted %v, want nothing deleted", fake.deletes)
	}
}

// An empty source with an empty remote is not suspicious, just empty.
func TestSyncEmptySourceEmptyRemoteIsFine(t *testing.T) {
	s := &Syncer{
		SourcePath: t.TempDir(), Backend: newFakeBackend(), RemotePath: "task",
		Options: models.SyncOptions{DeleteRemote: true},
	}
	if _, err := s.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

// TestSyncDeletesWhenSourceHasFiles confirms the guard does not block real mirroring.
func TestSyncDeletesWhenSourceHasFiles(t *testing.T) {
	src := writeFiles(t, 2, "payload")
	fake := newFakeBackend()
	fake.remote["task/gone.txt"] = backend.BackupInfo{
		Path: "task/gone.txt", Size: 1, LastModified: time.Now().Format(time.RFC3339),
	}

	s := &Syncer{
		SourcePath: src, Backend: fake, RemotePath: "task",
		Options: models.SyncOptions{DeleteRemote: true},
	}
	result, err := s.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.FilesDeleted != 1 {
		t.Errorf("FilesDeleted = %d, want 1", result.FilesDeleted)
	}
}

// TestCompareMethods pins each comparison mode's decision for a file whose
// size matches but whose content and timestamps differ in various ways.
func TestCompareMethods(t *testing.T) {
	src := t.TempDir()
	path := filepath.Join(src, "f.txt")
	if err := os.WriteFile(path, []byte("local"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	local := FileInfo{Path: path, RelativePath: "f.txt", Size: info.Size(), ModTime: info.ModTime()}

	remoteNewer := info.ModTime().Add(time.Hour).Format(time.RFC3339)
	remoteOlder := info.ModTime().Add(-time.Hour).Format(time.RFC3339)

	cases := []struct {
		name   string
		method string
		remote backend.BackupInfo
		want   bool
	}{
		{
			name:   "auto uses hash when available and content differs",
			method: "auto",
			remote: backend.BackupInfo{Size: local.Size, LastModified: remoteNewer, Hash: md5hex("remote"), HashAlgo: "md5"},
			want:   true,
		},
		{
			name:   "auto skips when hash matches even if mtime is older",
			method: "auto",
			remote: backend.BackupInfo{Size: local.Size, LastModified: remoteOlder, Hash: md5hex("local"), HashAlgo: "md5"},
			want:   false,
		},
		{
			name:   "auto falls back to mtime with no remote hash",
			method: "auto",
			remote: backend.BackupInfo{Size: local.Size, LastModified: remoteOlder},
			want:   true,
		},
		{
			name:   "hash re-uploads when the backend exposes no hash",
			method: "hash",
			remote: backend.BackupInfo{Size: local.Size, LastModified: remoteNewer},
			want:   true,
		},
		{
			name:   "hash ignores mtime when content matches",
			method: "hash",
			remote: backend.BackupInfo{Size: local.Size, LastModified: remoteOlder, Hash: md5hex("local"), HashAlgo: "md5"},
			want:   false,
		},
		{
			name:   "mtime ignores a differing hash",
			method: "mtime",
			remote: backend.BackupInfo{Size: local.Size, LastModified: remoteNewer, Hash: md5hex("remote"), HashAlgo: "md5"},
			want:   false,
		},
		{
			name:   "mtime uploads when local is newer",
			method: "mtime",
			remote: backend.BackupInfo{Size: local.Size, LastModified: remoteOlder},
			want:   true,
		},
		{
			name:   "size only never re-uploads on equal size",
			method: "size",
			remote: backend.BackupInfo{Size: local.Size, LastModified: remoteOlder, Hash: md5hex("remote"), HashAlgo: "md5"},
			want:   false,
		},
		{
			name:   "size uploads when sizes differ",
			method: "size",
			remote: backend.BackupInfo{Size: local.Size + 1, LastModified: remoteNewer},
			want:   true,
		},
		{
			name:   "unknown method behaves as auto",
			method: "nonsense",
			remote: backend.BackupInfo{Size: local.Size, LastModified: remoteOlder, Hash: md5hex("local"), HashAlgo: "md5"},
			want:   false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Syncer{SourcePath: src, Options: models.SyncOptions{CompareMethod: c.method}}
			if got := s.needsUpload(local, c.remote); got != c.want {
				t.Errorf("needsUpload = %v, want %v", got, c.want)
			}
		})
	}
}

// TestSyncCancellation: an in-flight sync must stop, not push every file.
func TestSyncCancellation(t *testing.T) {
	src := writeFiles(t, 200, "payload")
	fake := newFakeBackend()
	fake.uploadDelay = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()

	s := &Syncer{SourcePath: src, Backend: fake, RemotePath: "task", Concurrency: 4}
	_, err := s.Sync(ctx)
	if err == nil {
		t.Fatal("Sync returned nil error after cancellation")
	}
	if fake.uploadCount() >= 200 {
		t.Errorf("uploaded %d files despite cancellation", fake.uploadCount())
	}
}
