package backend

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
)

// ComputeFileHash streams localPath through the named algorithm ("md5",
// "sha1", or "sha256") and returns the lowercase hex digest.
func ComputeFileHash(localPath, algo string) (string, error) {
	var h hash.Hash
	switch strings.ToLower(algo) {
	case "md5":
		h = md5.New()
	case "sha1":
		h = sha1.New()
	case "sha256":
		h = sha256.New()
	default:
		return "", fmt.Errorf("unsupported hash algorithm: %s", algo)
	}

	f, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file for hashing: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("failed to hash file: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// MatchReason explains the outcome of a RemoteMatchesLocal call so callers
// can log honestly (e.g. "uploaded because backend has no hash support" vs
// "uploaded because content differs").
type MatchReason int

const (
	MatchUnknown       MatchReason = iota
	MatchEqual                     // Local content hash equals remote.
	MatchDiffers                   // Hashes compared but did not match.
	MatchRemoteMissing             // Backend reports no such object.
	MatchNoHashSupport             // Backend does not implement HashedBackend.
	MatchNoUsableHash              // Remote exists but exposes no comparable hash.
)

func (r MatchReason) String() string {
	switch r {
	case MatchEqual:
		return "remote hash matches local"
	case MatchDiffers:
		return "remote hash differs from local"
	case MatchRemoteMissing:
		return "remote object missing"
	case MatchNoHashSupport:
		return "backend has no hash support"
	case MatchNoUsableHash:
		return "remote exposes no comparable hash"
	default:
		return "unknown"
	}
}

// RemoteMatchesLocal reports whether the backend has remotePath stored with
// the same content as localPath, along with the reason for the answer.
func RemoteMatchesLocal(ctx context.Context, b StorageBackend, localPath, remotePath string) (bool, MatchReason, error) {
	h, ok := b.(HashedBackend)
	if !ok {
		return false, MatchNoHashSupport, nil
	}

	algo, remoteHex, err := h.RemoteHash(ctx, remotePath)
	if err != nil {
		if errors.Is(err, ErrRemoteObjectNotFound) {
			return false, MatchRemoteMissing, nil
		}
		return false, MatchUnknown, err
	}
	if algo == "" || remoteHex == "" {
		return false, MatchNoUsableHash, nil
	}

	localHex, err := ComputeFileHash(localPath, algo)
	if err != nil {
		return false, MatchUnknown, err
	}
	if strings.EqualFold(localHex, remoteHex) {
		return true, MatchEqual, nil
	}
	return false, MatchDiffers, nil
}

// RemoteMatchesHash compares a precomputed local hash against the remote
// object's hash. Returns MatchNoUsableHash if the algorithms differ — the
// caller can fall back to RemoteMatchesLocal if it wants to recompute.
func RemoteMatchesHash(ctx context.Context, b StorageBackend, remotePath, localAlgo, localHex string) (bool, MatchReason, error) {
	h, ok := b.(HashedBackend)
	if !ok {
		return false, MatchNoHashSupport, nil
	}
	algo, remoteHex, err := h.RemoteHash(ctx, remotePath)
	if err != nil {
		if errors.Is(err, ErrRemoteObjectNotFound) {
			return false, MatchRemoteMissing, nil
		}
		return false, MatchUnknown, err
	}
	if algo == "" || remoteHex == "" {
		return false, MatchNoUsableHash, nil
	}
	if !strings.EqualFold(algo, localAlgo) {
		return false, MatchNoUsableHash, nil
	}
	if strings.EqualFold(localHex, remoteHex) {
		return true, MatchEqual, nil
	}
	return false, MatchDiffers, nil
}
