package backend

import (
	"context"
	"errors"
	"log"
	"math"
	"time"
)

// RetryConfig controls the retry/backoff policy used by UploadWithRetry.
// Zero values disable retries.
type RetryConfig struct {
	MaxAttempts  int           // Total attempts including the first; <2 disables retry.
	InitialDelay time.Duration // First sleep before retry.
	MaxDelay     time.Duration // Cap on backoff delay.
}

// DefaultUploadRetry is the policy executor.uploadToBackend uses when the
// caller does not override it.
var DefaultUploadRetry = RetryConfig{
	MaxAttempts:  3,
	InitialDelay: 2 * time.Second,
	MaxDelay:     30 * time.Second,
}

// UploadWithRetry calls b.Upload with exponential backoff. Context cancellation
// and ErrUploadFatal short-circuit the retry loop.
func UploadWithRetry(ctx context.Context, b StorageBackend, localPath, remotePath string, progress ProgressCallback, cfg RetryConfig) error {
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := b.Upload(ctx, localPath, remotePath, progress)
		if err == nil {
			return nil
		}
		lastErr = err

		if errors.Is(err, ErrUploadFatal) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if attempt == cfg.MaxAttempts {
			break
		}

		delay := time.Duration(math.Pow(2, float64(attempt-1))) * cfg.InitialDelay
		if cfg.MaxDelay > 0 && delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
		log.Printf("upload attempt %d/%d failed for %s: %v (retrying in %s)", attempt, cfg.MaxAttempts, remotePath, err, delay)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}

// ErrUploadFatal can be wrapped by backend implementations to opt out of retry
// for errors that won't change on a second attempt (e.g. permission denied,
// 4xx auth failures).
var ErrUploadFatal = errors.New("upload error is not retryable")
