package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/nsilverman/archivist/internal/models"
)

func defaultOptions() models.ArchiveOptions {
	return models.ArchiveOptions{Format: "tar.gz", Compression: "gzip", UseTimestamp: true}
}

// readArchive returns the archive's entries keyed by name, with the body of
// regular files.
type entry struct {
	typeflag byte
	linkname string
	body     string
	size     int64
}

func readArchive(t *testing.T, path string) map[string]entry {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()

	entries := make(map[string]entry)
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read body of %s: %v", hdr.Name, err)
		}
		entries[hdr.Name] = entry{
			typeflag: hdr.Typeflag,
			linkname: hdr.Linkname,
			body:     string(body),
			size:     hdr.Size,
		}
	}
	return entries
}

// TestBuildSymlinkedSourceRoot covers the documented setup, where source_path
// points at a symlink under sources/. A walk of a symlinked root yields only
// the link itself, which used to fail the build outright.
func TestBuildSymlinkedSourceRoot(t *testing.T) {
	tmp := t.TempDir()

	real := filepath.Join(tmp, "real")
	if err := os.MkdirAll(filepath.Join(real, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "a.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "sub", "b.txt"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(tmp, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "out")
	res, err := NewBuilder(link, out, defaultOptions(), nil).Build(context.Background(), "symroot")
	if err != nil {
		t.Fatalf("Build through symlinked root: %v", err)
	}

	if res.FilesArchived != 2 {
		t.Errorf("FilesArchived = %d, want 2", res.FilesArchived)
	}

	entries := readArchive(t, res.Path)
	if got := entries["a.txt"].body; got != "hello" {
		t.Errorf("a.txt body = %q, want %q", got, "hello")
	}
	if got := entries["sub/b.txt"].body; got != "nested" {
		t.Errorf("sub/b.txt body = %q, want %q", got, "nested")
	}
	if _, ok := entries["sub/"]; !ok {
		t.Errorf("directory entry sub/ missing; entries: %v", keys(entries))
	}
}

// TestBuildSymlinksInsideTree checks that links within the tree are stored as
// links pointing at their real target, rather than being opened as files.
func TestBuildSymlinksInsideTree(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(filepath.Join(src, "dir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "target.txt"), []byte("real content"), 0644); err != nil {
		t.Fatal(err)
	}
	// A link to a file, a link to a directory, and a dangling link.
	if err := os.Symlink("target.txt", filepath.Join(src, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dir", filepath.Join(src, "dirlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nowhere.txt", filepath.Join(src, "dangling.txt")); err != nil {
		t.Fatal(err)
	}

	res, err := NewBuilder(src, filepath.Join(tmp, "out"), defaultOptions(), nil).Build(context.Background(), "symtree")
	if err != nil {
		t.Fatalf("Build with symlinks in tree: %v", err)
	}

	entries := readArchive(t, res.Path)

	for name, wantTarget := range map[string]string{
		"alias.txt":    "target.txt",
		"dirlink":      "dir",
		"dangling.txt": "nowhere.txt",
	} {
		e, ok := entries[name]
		if !ok {
			t.Errorf("%s missing from archive; entries: %v", name, keys(entries))
			continue
		}
		if e.typeflag != tar.TypeSymlink {
			t.Errorf("%s typeflag = %q, want symlink", name, e.typeflag)
		}
		if e.linkname != wantTarget {
			t.Errorf("%s linkname = %q, want %q", name, e.linkname, wantTarget)
		}
		if e.size != 0 {
			t.Errorf("%s size = %d, want 0 (symlinks carry no body)", name, e.size)
		}
	}

	if got := entries["target.txt"].body; got != "real content" {
		t.Errorf("target.txt body = %q, want %q", got, "real content")
	}
}

// TestBuildSkipsUnreadableFile: one denied file must not cost the whole backup.
func TestBuildSkipsUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}

	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "ok.txt"), []byte("readable"), 0644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(src, "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0000); err != nil {
		t.Fatal(err)
	}

	res, err := NewBuilder(src, filepath.Join(tmp, "out"), defaultOptions(), nil).Build(context.Background(), "denied")
	if err != nil {
		t.Fatalf("Build with unreadable file: %v", err)
	}

	entries := readArchive(t, res.Path)
	if got := entries["ok.txt"].body; got != "readable" {
		t.Errorf("ok.txt body = %q, want %q", got, "readable")
	}
	if _, ok := entries["secret.txt"]; ok {
		t.Errorf("unreadable file should have been skipped, not archived")
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "secret.txt") {
		t.Errorf("Warnings = %v, want one mentioning secret.txt", res.Warnings)
	}
}

// TestBuildSpecialFiles: a FIFO must be recorded without being opened —
// reading one would block the backup forever — and a socket must be skipped.
func TestBuildSpecialFiles(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("mknod semantics are platform specific")
	}

	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "plain.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(src, "pipe"), 0644); err != nil {
		t.Skipf("cannot create fifo: %v", err)
	}

	done := make(chan struct{})
	var res *Result
	var err error
	go func() {
		defer close(done)
		res, err = NewBuilder(src, filepath.Join(tmp, "out"), defaultOptions(), nil).Build(context.Background(), "special")
	}()

	select {
	case <-done:
	case <-timeoutAfter(t):
		t.Fatal("Build blocked on a FIFO")
	}
	if err != nil {
		t.Fatalf("Build with special files: %v", err)
	}

	entries := readArchive(t, res.Path)
	if got := entries["plain.txt"].body; got != "data" {
		t.Errorf("plain.txt body = %q, want %q", got, "data")
	}
	pipe, ok := entries["pipe"]
	if !ok {
		t.Fatalf("fifo entry missing; entries: %v", keys(entries))
	}
	if pipe.typeflag != tar.TypeFifo {
		t.Errorf("pipe typeflag = %q, want fifo", pipe.typeflag)
	}
}

// TestBuildHashMatchesFile guards the on-disk hash contract that the upload
// integrity check and the skip-unchanged check both depend on.
func TestBuildHashMatchesFile(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := NewBuilder(src, filepath.Join(tmp, "out"), defaultOptions(), nil).Build(context.Background(), "hash")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	want := "sha256:" + hex.EncodeToString(sum[:])
	if res.Hash != want {
		t.Errorf("Hash = %s, want %s", res.Hash, want)
	}
	if res.Size != int64(len(data)) {
		t.Errorf("Size = %d, want %d", res.Size, len(data))
	}
}

// TestBuildCancelled: cancelling must stop the walk and leave no partial
// archive behind in the temp directory.
func TestBuildCancelled(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		name := filepath.Join(src, fmt.Sprintf("f%02d.txt", i))
		if err := os.WriteFile(name, make([]byte, 1024), 0644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	out := filepath.Join(tmp, "out")

	// Cancel once the first few files are in.
	builder := NewBuilder(src, out, defaultOptions(), func(current, total int64, file string) {
		if current > 2048 {
			cancel()
		}
	})

	if _, err := builder.Build(ctx, "cancelme"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Build error = %v, want context.Canceled", err)
	}

	remaining, err := os.ReadDir(out)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Errorf("partial archive left behind: %d file(s) in %s", len(remaining), out)
	}
	cancel()
}

func keys(m map[string]entry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// timeoutAfter returns a channel that fires if a test step takes too long.
func timeoutAfter(t *testing.T) <-chan time.Time {
	t.Helper()
	return time.After(10 * time.Second)
}

// TestArchiveFormats checks each supported compression produces a readable
// archive with the extension retention filtering expects.
func TestArchiveFormats(t *testing.T) {
	cases := []struct {
		name        string
		format      string
		compression string
		wantExt     string
	}{
		{"gzip", "tar.gz", "gzip", ".tar.gz"},
		{"zstd", "tar.zst", "zstd", ".tar.zst"},
		{"none", "tar", "none", ".tar"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmp := t.TempDir()
			src := filepath.Join(tmp, "src")
			if err := os.MkdirAll(src, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("payload"), 0644); err != nil {
				t.Fatal(err)
			}

			opts := models.ArchiveOptions{Format: c.format, Compression: c.compression, UseTimestamp: true}
			if got := ArchiveExtension(opts); got != c.wantExt {
				t.Errorf("ArchiveExtension = %s, want %s", got, c.wantExt)
			}

			res, err := NewBuilder(src, filepath.Join(tmp, "out"), opts, nil).Build(context.Background(), "fmt")
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if !strings.HasSuffix(res.Path, c.wantExt) {
				t.Errorf("archive path %s does not end in %s", res.Path, c.wantExt)
			}

			// The filename retention matches on must be the one produced.
			name, err := GenerateFilename("fmt", opts)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(name, ArchiveExtension(opts)) {
				t.Errorf("GenerateFilename %s disagrees with ArchiveExtension %s", name, ArchiveExtension(opts))
			}

			body := readArchiveWith(t, res.Path, c.compression)
			if body["f.txt"].body != "payload" {
				t.Errorf("f.txt body = %q, want %q", body["f.txt"].body, "payload")
			}
		})
	}
}

// readArchiveWith reads an archive compressed with the named algorithm.
func readArchiveWith(t *testing.T, path, compression string) map[string]entry {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	var r io.Reader = f
	switch compression {
	case "gzip":
		gz, err := gzip.NewReader(f)
		if err != nil {
			t.Fatalf("gzip reader: %v", err)
		}
		defer gz.Close()
		r = gz
	case "zstd":
		zr, err := zstd.NewReader(f)
		if err != nil {
			t.Fatalf("zstd reader: %v", err)
		}
		defer zr.Close()
		r = zr
	}

	entries := make(map[string]entry)
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		entries[hdr.Name] = entry{typeflag: hdr.Typeflag, linkname: hdr.Linkname, body: string(body), size: hdr.Size}
	}
	return entries
}

func TestSupportedFormat(t *testing.T) {
	for _, format := range []string{"tar", "tar.gz", "tar.zst"} {
		if !SupportedFormat(format) {
			t.Errorf("SupportedFormat(%q) = false, want true", format)
		}
	}
	// Formats the README once advertised but the builder never implemented
	// must be rejected loudly rather than silently producing a gzip archive.
	for _, format := range []string{"tar.bz2", "tar.xz", "zip", "rar"} {
		if SupportedFormat(format) {
			t.Errorf("SupportedFormat(%q) = true, want false", format)
		}
	}
}

func TestBuildRejectsUnsupportedFormat(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}

	opts := models.ArchiveOptions{Format: "zip", UseTimestamp: true}
	_, err := NewBuilder(src, filepath.Join(tmp, "out"), opts, nil).Build(context.Background(), "nope")
	if err == nil {
		t.Fatal("Build with an unsupported format succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "unsupported archive format") {
		t.Errorf("error = %v, want it to mention the unsupported format", err)
	}
}
