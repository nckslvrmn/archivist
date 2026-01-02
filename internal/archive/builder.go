package archive

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// NewBuilder creates a new archive builder
func NewBuilder(sourcePath, outputDir string, options models.ArchiveOptions, progress ProgressCallback) *Builder {
	return &Builder{
		SourcePath: sourcePath,
		OutputPath: outputDir,
		Options:    options,
		Progress:   progress,
	}
}

// Build creates the archive and returns the path and hash
func (b *Builder) Build(taskName string) (archivePath string, hash string, size int64, err error) {
	// Generate filename from pattern
	filename, err := b.GenerateFilename(taskName)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to generate filename: %w", err)
	}

	archivePath = filepath.Join(b.OutputPath, filename)

	// Ensure output directory exists
	if err := os.MkdirAll(b.OutputPath, 0755); err != nil {
		return "", "", 0, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Calculate total size for progress reporting
	totalSize, fileCount, err := b.calculateSize(b.SourcePath)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to calculate source size: %w", err)
	}

	// Create archive based on format
	switch b.Options.Format {
	case "tar.gz", "tar":
		hash, size, err = b.createTarGz(archivePath, totalSize, fileCount)
	default:
		return "", "", 0, fmt.Errorf("unsupported archive format: %s", b.Options.Format)
	}

	if err != nil {
		return "", "", 0, err
	}

	return archivePath, hash, size, nil
}

// GenerateFilename creates the archive filename from the pattern
func (b *Builder) GenerateFilename(taskName string) (string, error) {
	pattern := b.Options.NamePattern
	if pattern == "" {
		// Default pattern
		if b.Options.UseTimestamp {
			pattern = "{task}_{timestamp}.tar.gz"
		} else {
			pattern = "{task}_latest.tar.gz"
		}
	}

	// Replace placeholders
	filename := pattern

	// Sanitize task name for filename
	sanitizedTask := sanitizeFilename(taskName)
	filename = strings.ReplaceAll(filename, "{task}", sanitizedTask)

	// Replace timestamp if present
	if strings.Contains(filename, "{timestamp}") {
		if b.Options.UseTimestamp {
			timestamp := time.Now().Format("20060102_150405")
			filename = strings.ReplaceAll(filename, "{timestamp}", timestamp)
		} else {
			// Remove timestamp placeholder if not using timestamps
			filename = strings.ReplaceAll(filename, "_{timestamp}", "")
			filename = strings.ReplaceAll(filename, "{timestamp}_", "")
			filename = strings.ReplaceAll(filename, "{timestamp}", "")
		}
	}

	// Ensure proper extension
	if !strings.HasSuffix(filename, ".tar.gz") && !strings.HasSuffix(filename, ".tar") {
		filename += ".tar.gz"
	}

	return filename, nil
}

// createTarGz creates a tar.gz archive
func (b *Builder) createTarGz(outputPath string, totalSize int64, fileCount int) (hash string, size int64, err error) {
	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create archive file: %w", err)
	}
	defer func() {
		if err := outFile.Close(); err != nil {
			log.Printf("Error closing output file: %v", err)
		}
	}()

	// Create hash writer
	hasher := sha256.New()
	multiWriter := io.MultiWriter(outFile, hasher)

	// Create gzip writer if compression is enabled
	var archiveWriter = multiWriter
	if b.Options.Compression == "gzip" || b.Options.Compression == "" {
		gzipWriter := gzip.NewWriter(multiWriter)
		defer func() {
			if err := gzipWriter.Close(); err != nil {
				log.Printf("Error closing gzip writer: %v", err)
			}
		}()
		archiveWriter = gzipWriter
	}

	// Create tar writer
	tarWriter := tar.NewWriter(archiveWriter)
	defer func() {
		if err := tarWriter.Close(); err != nil {
			log.Printf("Error closing tar writer: %v", err)
		}
	}()

	// Track progress
	var bytesProcessed int64
	filesProcessed := 0

	// Walk the source directory
	err = filepath.Walk(b.SourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return fmt.Errorf("failed to create tar header: %w", err)
		}

		// Set the name to be relative to the source path
		relPath, err := filepath.Rel(b.SourcePath, path)
		if err != nil {
			return err
		}
		header.Name = relPath

		// Write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header: %w", err)
		}

		// If it's a file, write its contents
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open file %s: %w", path, err)
			}
			defer func() {
				if err := file.Close(); err != nil {
					log.Printf("Error closing file %s: %v", path, err)
				}
			}()

			written, err := io.Copy(tarWriter, file)
			if err != nil {
				return fmt.Errorf("failed to write file %s: %w", path, err)
			}

			bytesProcessed += written
			filesProcessed++

			// Report progress
			if b.Progress != nil {
				b.Progress(bytesProcessed, totalSize, relPath)
			}
		}

		return nil
	})

	if err != nil {
		return "", 0, fmt.Errorf("failed to create archive: %w", err)
	}

	// Get file size
	stat, err := outFile.Stat()
	if err != nil {
		return "", 0, fmt.Errorf("failed to stat archive: %w", err)
	}

	// Calculate hash
	hashBytes := hasher.Sum(nil)
	hashString := fmt.Sprintf("sha256:%x", hashBytes)

	return hashString, stat.Size(), nil
}

// calculateSize calculates the total size of files in a directory
func (b *Builder) calculateSize(path string) (totalSize int64, fileCount int, err error) {
	err = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
			fileCount++
		}
		return nil
	})
	return
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
