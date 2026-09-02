package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type testResolver struct{}

func (testResolver) ResolvePath(path string) string { return path }

func newLocalBackend(t *testing.T) (*LocalBackend, string) {
	t.Helper()
	base := t.TempDir()
	b := &LocalBackend{}
	if err := b.Initialize(map[string]interface{}{"path": base}, testResolver{}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return b, base
}

// TestLocalRemoteHash: without this the local backend reported "no hash
// support", so skip-unchanged and the post-upload integrity check silently
// did nothing for local backups.
func TestLocalRemoteHash(t *testing.T) {
	b, base := newLocalBackend(t)

	content := []byte("backup contents")
	if err := os.WriteFile(filepath.Join(base, "archive.tar.gz"), content, 0644); err != nil {
		t.Fatal(err)
	}

	algo, digest, err := b.RemoteHash(context.Background(), "archive.tar.gz")
	if err != nil {
		t.Fatalf("RemoteHash: %v", err)
	}
	if algo != "sha256" {
		t.Errorf("algo = %s, want sha256", algo)
	}
	sum := sha256.Sum256(content)
	if digest != hex.EncodeToString(sum[:]) {
		t.Errorf("digest = %s, want %s", digest, hex.EncodeToString(sum[:]))
	}
}

func TestLocalRemoteHashMissing(t *testing.T) {
	b, _ := newLocalBackend(t)

	_, _, err := b.RemoteHash(context.Background(), "nope.tar.gz")
	if !errors.Is(err, ErrRemoteObjectNotFound) {
		t.Fatalf("err = %v, want ErrRemoteObjectNotFound", err)
	}
}

func TestLocalRemoteHashDirectory(t *testing.T) {
	b, base := newLocalBackend(t)
	if err := os.MkdirAll(filepath.Join(base, "adir"), 0755); err != nil {
		t.Fatal(err)
	}

	_, _, err := b.RemoteHash(context.Background(), "adir")
	if !errors.Is(err, ErrRemoteObjectNotFound) {
		t.Fatalf("err = %v, want ErrRemoteObjectNotFound", err)
	}
}

// TestLocalMatchesLocal exercises the path the executor actually uses.
func TestLocalMatchesLocal(t *testing.T) {
	b, base := newLocalBackend(t)

	local := filepath.Join(t.TempDir(), "local.tar.gz")
	if err := os.WriteFile(local, []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "remote.tar.gz"), []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "different.tar.gz"), []byte("other"), 0644); err != nil {
		t.Fatal(err)
	}

	matched, reason, err := RemoteMatchesLocal(context.Background(), b, local, "remote.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if !matched || reason != MatchEqual {
		t.Errorf("identical content: matched=%v reason=%v, want true/MatchEqual", matched, reason)
	}

	matched, reason, err = RemoteMatchesLocal(context.Background(), b, local, "different.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if matched || reason != MatchDiffers {
		t.Errorf("differing content: matched=%v reason=%v, want false/MatchDiffers", matched, reason)
	}

	matched, reason, err = RemoteMatchesLocal(context.Background(), b, local, "absent.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if matched || reason != MatchRemoteMissing {
		t.Errorf("missing remote: matched=%v reason=%v, want false/MatchRemoteMissing", matched, reason)
	}
}
