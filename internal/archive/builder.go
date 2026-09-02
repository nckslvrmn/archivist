package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/nsilverman/archivist/internal/models"
)

// ProgressCallback is called during archive creation to report progress
type ProgressCallback func(current, total int64, currentFile string)

// Builder creates compressed archives from source directories
type Builder struct {
	SourcePath string
	OutputPath string
	Options    models.ArchiveOptions
	Progress   ProgressCallback
}

// Result describes a completed archive.
type Result struct {
	Path          string
	Hash          string // "<algo>:<hex>"
	Size          int64
	FilesArchived int
	// Warnings collects entries that were skipped rather than aborting the
	// whole archive (unreadable files, sockets, dangling symlinks). A backup
	// that covers 99% of a tree is worth far more than no backup at all, but
	// the gaps must be reported.
	Warnings []string
}

// NewBuilder creates a new archive builder
func NewBuilder(sourcePath, outputDir string, options models.ArchiveOptions, progress ProgressCallback) *Builder {
	return &Builder{
		SourcePath: sourcePath,
		OutputPath: outputDir,
		Options:    options,
		Progress:   progress,
	}
}

// maxWarnings caps the warning list so a pathological tree (e.g. thousands of
// unreadable files) cannot balloon the execution record.
const maxWarnings = 100

// Build creates the archive and returns its path, hash and size.
// The archive is removed if the build fails, so a cancelled or failed run
// never leaves a partial file behind in the temp directory.
func (b *Builder) Build(ctx context.Context, taskName string) (*Result, error) {
	filename, err := GenerateFilename(taskName, b.Options)
	if err != nil {
		return nil, fmt.Errorf("failed to generate filename: %w", err)
	}

	// Resolve the source root through any symlinks. The documented workflow
	// points source_path at a symlink under sources/, and filepath.Walk does
	// not follow a symlinked root — without this the walk yields the link
	// itself and the archive is empty or fails outright.
	root, err := filepath.EvalSymlinks(b.SourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve source path %s: %w", b.SourcePath, err)
	}

	archivePath := filepath.Join(b.OutputPath, filename)

	if err := os.MkdirAll(b.OutputPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Calculate total size for progress reporting
	totalSize, _, err := b.calculateSize(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate source size: %w", err)
	}

	if !SupportedFormat(b.Options.Format) {
		return nil, fmt.Errorf("unsupported archive format: %s (supported: tar, tar.gz, tar.zst)", b.Options.Format)
	}

	result, err := b.createTarArchive(ctx, root, archivePath, totalSize)

	if err != nil {
		if removeErr := os.Remove(archivePath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("Error removing partial archive %s: %v", archivePath, removeErr)
		}
		return nil, err
	}

	result.Path = archivePath
	return result, nil
}

// GenerateFilename produces an archive filename from the configured pattern.
// Exposed at package scope so dry-run / planning code can derive the filename
// without instantiating a Builder.
func GenerateFilename(taskName string, opts models.ArchiveOptions) (string, error) {
	ext := ArchiveExtension(opts)

	pattern := opts.NamePattern
	if pattern == "" {
		if opts.UseTimestamp {
			pattern = "{task}_{timestamp}" + ext
		} else {
			pattern = "{task}_latest" + ext
		}
	}

	filename := pattern
	filename = strings.ReplaceAll(filename, "{task}", sanitizeFilename(taskName))

	if strings.Contains(filename, "{timestamp}") {
		if opts.UseTimestamp {
			filename = strings.ReplaceAll(filename, "{timestamp}", time.Now().Format("20060102_150405"))
		} else {
			filename = strings.ReplaceAll(filename, "_{timestamp}", "")
			filename = strings.ReplaceAll(filename, "{timestamp}_", "")
			filename = strings.ReplaceAll(filename, "{timestamp}", "")
		}
	}

	hasKnownExt := false
	for _, known := range knownExtensions {
		if strings.HasSuffix(filename, known) {
			hasKnownExt = true
			break
		}
	}
	if !hasKnownExt {
		filename += ext
	}

	return filename, nil
}

// Supported archive extensions, longest first so ".tar.gz" is matched before
// ".tar" when inspecting a filename pattern.
var knownExtensions = []string{".tar.gz", ".tar.zst", ".tar"}

// ArchiveExtension returns the on-disk extension produced for the given
// archive options. Retention and the skip-unchanged check both filter remote
// objects with it, so it must agree with GenerateFilename exactly.
func ArchiveExtension(opts models.ArchiveOptions) string {
	if opts.NamePattern != "" {
		for _, ext := range knownExtensions {
			if strings.HasSuffix(opts.NamePattern, ext) {
				return ext
			}
		}
	}
	switch compressionFor(opts) {
	case compressionZstd:
		return ".tar.zst"
	case compressionNone:
		return ".tar"
	default:
		return ".tar.gz"
	}
}

// Compression algorithms understood by the builder.
const (
	compressionGzip = "gzip"
	compressionZstd = "zstd"
	compressionNone = "none"
)

// compressionFor resolves the effective compression for a task, tolerating
// the older configurations that only set Format.
func compressionFor(opts models.ArchiveOptions) string {
	switch strings.ToLower(opts.Compression) {
	case compressionZstd:
		return compressionZstd
	case compressionNone:
		return compressionNone
	case compressionGzip:
		return compressionGzip
	}
	// Compression unset: fall back to what the format implies.
	switch strings.ToLower(opts.Format) {
	case "tar.zst":
		return compressionZstd
	case "tar":
		return compressionNone
	default:
		return compressionGzip
	}
}

// SupportedFormat reports whether the builder can produce this format.
func SupportedFormat(format string) bool {
	switch strings.ToLower(format) {
	case "tar", "tar.gz", "tar.zst":
		return true
	default:
		return false
	}
}

// createTarArchive creates a tar archive rooted at root (already
// symlink-resolved), compressed according to the task's options.
func (b *Builder) createTarArchive(ctx context.Context, root, outputPath string, totalSize int64) (*Result, error) {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create archive file: %w", err)
	}
	fileClosed := false
	defer func() {
		if fileClosed {
			return
		}
		if err := outFile.Close(); err != nil {
			log.Printf("Error closing output file: %v", err)
		}
	}()

	// Create hash writer
	hasher := sha256.New()
	multiWriter := io.MultiWriter(outFile, hasher)

	// The hasher sits beneath the compressor so it sees the final on-disk
	// bytes, including the footers emitted on Close — otherwise the recorded
	// hash covers only a prefix of the file and never matches what backends
	// compute after upload.
	archiveWriter := multiWriter
	var compressor io.WriteCloser
	switch compressionFor(b.Options) {
	case compressionZstd:
		zw, err := zstd.NewWriter(multiWriter)
		if err != nil {
			return nil, fmt.Errorf("failed to create zstd writer: %w", err)
		}
		compressor = zw
		archiveWriter = zw
	case compressionNone:
		// Plain tar: the tar writer writes straight to the file.
	default:
		gw := gzip.NewWriter(multiWriter)
		compressor = gw
		archiveWriter = gw
	}

	tarWriter := tar.NewWriter(archiveWriter)

	result := &Result{}
	var bytesProcessed int64

	warn := func(format string, args ...interface{}) {
		msg := fmt.Sprintf(format, args...)
		log.Printf("archive: %s", msg)
		if len(result.Warnings) < maxWarnings {
			result.Warnings = append(result.Warnings, msg)
		} else if len(result.Warnings) == maxWarnings {
			result.Warnings = append(result.Warnings, "additional warnings suppressed")
		}
	}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		// An error on the root itself is fatal; anywhere else we skip the
		// entry and keep going.
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			warn("skipping %s: %v", path, walkErr)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			warn("skipping %s: %v", path, err)
			return nil
		}
		if relPath == "." {
			return nil // the root directory itself needs no entry
		}
		name := filepath.ToSlash(relPath)

		// d.Info is an lstat: symlinks are reported as symlinks, not targets.
		info, err := d.Info()
		if err != nil {
			warn("skipping %s: %v", path, err)
			return nil
		}

		mode := info.Mode()
		switch {
		case mode&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				warn("skipping symlink %s: %v", name, err)
				return nil
			}
			header, err := tar.FileInfoHeader(info, target)
			if err != nil {
				warn("skipping symlink %s: %v", name, err)
				return nil
			}
			header.Name = name
			if err := tarWriter.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to write tar header for %s: %w", name, err)
			}

		case mode.IsDir():
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				warn("skipping directory %s: %v", name, err)
				return nil
			}
			header.Name = name + "/"
			if err := tarWriter.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to write tar header for %s: %w", name, err)
			}

		case mode.IsRegular():
			written, err := b.writeRegularFile(tarWriter, path, name, info, warn)
			if err != nil {
				return err
			}
			if written < 0 {
				return nil // skipped, already warned
			}
			bytesProcessed += written
			result.FilesArchived++
			if b.Progress != nil {
				b.Progress(bytesProcessed, totalSize, name)
			}

		case mode&(os.ModeDevice|os.ModeCharDevice|os.ModeNamedPipe) != 0:
			// Metadata-only entries: recorded so a restore recreates them,
			// but never opened — reading a FIFO would block forever.
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				warn("skipping special file %s: %v", name, err)
				return nil
			}
			header.Name = name
			if err := tarWriter.WriteHeader(header); err != nil {
				return fmt.Errorf("failed to write tar header for %s: %w", name, err)
			}

		default:
			// Sockets and anything else tar cannot represent.
			warn("skipping unsupported file type %s (mode %s)", name, mode)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create archive: %w", err)
	}

	// Flush writers in order (tar → compressor → file) so the hasher sees
	// every trailing byte before we read the digest.
	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}
	if compressor != nil {
		if err := compressor.Close(); err != nil {
			return nil, fmt.Errorf("failed to close compression writer: %w", err)
		}
	}
	if err := outFile.Sync(); err != nil {
		return nil, fmt.Errorf("failed to sync archive: %w", err)
	}

	stat, err := outFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat archive: %w", err)
	}

	if err := outFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close archive: %w", err)
	}
	fileClosed = true

	result.Hash = fmt.Sprintf("sha256:%x", hasher.Sum(nil))
	result.Size = stat.Size()
	return result, nil
}

// writeRegularFile streams one regular file into the archive. It returns the
// number of bytes written, or -1 if the file was skipped (already warned).
// Only header.Size bytes are ever written, and a short read is zero-padded:
// a file that grows or shrinks while being archived must not corrupt the
// stream for every entry that follows it.
func (b *Builder) writeRegularFile(tw *tar.Writer, path, name string, info os.FileInfo, warn func(string, ...interface{})) (int64, error) {
	// Open before writing the header: once a header is written the body is
	// owed to the stream, so an unreadable file has to be detected first.
	file, err := os.Open(path)
	if err != nil {
		warn("skipping unreadable file %s: %v", name, err)
		return -1, nil
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Error closing file %s: %v", path, err)
		}
	}()

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		warn("skipping %s: %v", name, err)
		return -1, nil
	}
	header.Name = name

	if err := tw.WriteHeader(header); err != nil {
		return 0, fmt.Errorf("failed to write tar header for %s: %w", name, err)
	}

	written, err := io.CopyN(tw, file, header.Size)
	if err != nil && !errors.Is(err, io.EOF) {
		// The header is already committed, so the body must still be filled
		// out to header.Size to keep the archive readable.
		warn("truncated %s after %d/%d bytes: %v", name, written, header.Size, err)
	}
	if remaining := header.Size - written; remaining > 0 {
		if err == nil {
			warn("file %s shrank during archiving, padding %d bytes", name, remaining)
		}
		if _, err := io.CopyN(tw, zeroReader{}, remaining); err != nil {
			return 0, fmt.Errorf("failed to pad %s: %w", name, err)
		}
	}

	return header.Size, nil
}

// zeroReader is an infinite source of zero bytes used to pad truncated entries.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// calculateSize calculates the total size of regular files under root.
// Unreadable entries are ignored here; the walk in createTarGz reports them.
func (b *Builder) calculateSize(ctx context.Context, root string) (totalSize int64, fileCount int, err error) {
	err = filepath.WalkDir(root, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			// Sizing is best-effort: it only drives the progress bar, and
			// the archive walk reports anything it cannot read.
			return nil //nolint:nilerr // deliberate skip
		}
		totalSize += info.Size()
		fileCount++
		return nil
	})
	return
}

// SanitizeTaskName converts a task name to the form embedded in archive
// filenames. Retention/matching code uses this to filter the listing back
// down to objects produced by a specific task.
func SanitizeTaskName(name string) string {
	return sanitizeFilename(name)
}

// sanitizeFilename removes characters that aren't safe for filenames
func sanitizeFilename(name string) string {
	// Replace spaces with hyphens
	name = strings.ReplaceAll(name, " ", "-")

	// Remove or replace unsafe characters
	unsafe := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range unsafe {
		name = strings.ReplaceAll(name, char, "")
	}

	// Convert to lowercase
	name = strings.ToLower(name)

	return name
}
