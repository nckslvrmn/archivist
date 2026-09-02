package sync

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"testing"
)

func relPaths(files []FileInfo) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.RelativePath
	}
	sort.Strings(out)
	return out
}

// TestScanLocalFilesSymlinkedRoot mirrors the documented sources/ layout,
// where the source path is a symlink. Walking a symlinked root yields only
// the link itself, so sync used to see a single unusable entry.
func TestScanLocalFilesSymlinkedRoot(t *testing.T) {
	tmp := t.TempDir()

	real := filepath.Join(tmp, "real")
	if err := os.MkdirAll(filepath.Join(real, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "sub", "b.txt"), []byte("bb"), 0644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(tmp, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	s := &Syncer{SourcePath: link}
	files, err := s.scanLocalFiles()
	if err != nil {
		t.Fatalf("scanLocalFiles: %v", err)
	}

	got := relPaths(files)
	want := []string{"a.txt", "sub/b.txt"}
	if len(got) != len(want) {
		t.Fatalf("scanned %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("scanned %v, want %v", got, want)
			break
		}
	}
}

// TestScanLocalFilesSkipsNonRegular: entries with no object representation on
// a storage backend must be skipped, not queued for upload (uploading a
// symlink-to-directory fails, and reading a FIFO blocks forever).
func TestScanLocalFilesSkipsNonRegular(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(filepath.Join(src, "dir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dir", filepath.Join(src, "dirlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(src, "filelink")); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		if err := syscall.Mkfifo(filepath.Join(src, "pipe"), 0644); err != nil {
			t.Logf("skipping fifo case: %v", err)
		}
	}

	s := &Syncer{SourcePath: src}
	files, err := s.scanLocalFiles()
	if err != nil {
		t.Fatalf("scanLocalFiles: %v", err)
	}

	got := relPaths(files)
	if len(got) != 1 || got[0] != "real.txt" {
		t.Errorf("scanned %v, want only [real.txt]", got)
	}
}

// TestScanLocalFilesMissingRoot: a broken source must report an error rather
// than silently syncing nothing (which, with delete_remote, would wipe the
// remote copy).
func TestScanLocalFilesMissingRoot(t *testing.T) {
	s := &Syncer{SourcePath: filepath.Join(t.TempDir(), "does-not-exist")}
	if _, err := s.scanLocalFiles(); err == nil {
		t.Fatal("scanLocalFiles succeeded on a missing source, want an error")
	}
}
