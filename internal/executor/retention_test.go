package executor

import (
	"testing"
	"time"

	"github.com/nsilverman/archivist/internal/backend"
	"github.com/nsilverman/archivist/internal/models"
)

func backupAt(path string, age time.Duration, now time.Time) backend.BackupInfo {
	return backend.BackupInfo{
		Path:         path,
		LastModified: now.Add(-age).Format(time.RFC3339),
	}
}

func paths(backups []backend.BackupInfo) []string {
	out := make([]string, len(backups))
	for i, b := range backups {
		out[i] = b.Path
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSelectExpiredBackups(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	// Oldest to newest.
	all := []backend.BackupInfo{
		backupAt("d100.tar.gz", 100*24*time.Hour, now),
		backupAt("d030.tar.gz", 30*24*time.Hour, now),
		backupAt("d010.tar.gz", 10*24*time.Hour, now),
		backupAt("d001.tar.gz", 24*time.Hour, now),
	}

	cases := []struct {
		name    string
		backups []backend.BackupInfo
		policy  models.RetentionPolicy
		want    []string
	}{
		{
			name:    "keep last 2 by count",
			backups: all,
			policy:  models.RetentionPolicy{KeepLast: 2},
			want:    []string{"d100.tar.gz", "d030.tar.gz"},
		},
		{
			name:    "keep last covers everything",
			backups: all,
			policy:  models.RetentionPolicy{KeepLast: 10},
			want:    nil,
		},
		{
			name:    "age limit only",
			backups: all,
			policy:  models.RetentionPolicy{KeepDays: 20},
			want:    []string{"d100.tar.gz", "d030.tar.gz"},
		},
		{
			name:    "count and age combine",
			backups: all,
			policy:  models.RetentionPolicy{KeepLast: 3, KeepDays: 20},
			want:    []string{"d100.tar.gz", "d030.tar.gz"},
		},
		{
			name:    "no policy deletes nothing",
			backups: all,
			policy:  models.RetentionPolicy{},
			want:    nil,
		},
		{
			// The core safety property: an aggressive age limit must never
			// leave a task with zero backups.
			name:    "newest is never deleted",
			backups: all,
			policy:  models.RetentionPolicy{KeepDays: 1},
			want:    []string{"d100.tar.gz", "d030.tar.gz", "d010.tar.gz"},
		},
		{
			name:    "keep last 0 with age limit still spares newest",
			backups: all,
			policy:  models.RetentionPolicy{KeepLast: 0, KeepDays: 1},
			want:    []string{"d100.tar.gz", "d030.tar.gz", "d010.tar.gz"},
		},
		{
			name:    "single backup is untouchable",
			backups: all[:1],
			policy:  models.RetentionPolicy{KeepLast: 1, KeepDays: 1},
			want:    nil,
		},
		{
			name:    "empty input",
			backups: nil,
			policy:  models.RetentionPolicy{KeepLast: 1},
			want:    nil,
		},
		{
			name: "unparseable timestamps are not aged out",
			backups: []backend.BackupInfo{
				{Path: "broken.tar.gz", LastModified: "not-a-timestamp"},
				backupAt("d001.tar.gz", 24*time.Hour, now),
			},
			policy: models.RetentionPolicy{KeepDays: 1},
			want:   nil,
		},
		{
			// An entry with an unreadable timestamp is ignored by retention
			// entirely: it is never deleted, and it never counts toward
			// keep_last and so cannot evict a real backup.
			name: "unparseable timestamps are ignored, not counted",
			backups: []backend.BackupInfo{
				{Path: "broken.tar.gz", LastModified: "not-a-timestamp"},
				backupAt("d010.tar.gz", 10*24*time.Hour, now),
				backupAt("d001.tar.gz", 24*time.Hour, now),
			},
			policy: models.RetentionPolicy{KeepLast: 2},
			want:   nil,
		},
		{
			name: "unparseable entries do not shield real ones from keep_last",
			backups: []backend.BackupInfo{
				{Path: "broken.tar.gz", LastModified: ""},
				backupAt("d100.tar.gz", 100*24*time.Hour, now),
				backupAt("d010.tar.gz", 10*24*time.Hour, now),
				backupAt("d001.tar.gz", 24*time.Hour, now),
			},
			policy: models.RetentionPolicy{KeepLast: 2},
			want:   []string{"d100.tar.gz"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := paths(selectExpiredBackups(c.backups, c.policy, now))
			if !equal(got, c.want) {
				t.Errorf("selectExpiredBackups = %v, want %v", got, c.want)
			}
		})
	}
}

// TestSelectExpiredBackupsDoesNotMutateInput guards the caller's slice, which
// is reused for logging after the selection.
func TestSelectExpiredBackupsDoesNotMutateInput(t *testing.T) {
	now := time.Now()
	input := []backend.BackupInfo{
		backupAt("newest.tar.gz", time.Hour, now),
		backupAt("oldest.tar.gz", 100*24*time.Hour, now),
	}
	before := paths(input)

	selectExpiredBackups(input, models.RetentionPolicy{KeepLast: 1}, now)

	if !equal(paths(input), before) {
		t.Errorf("input reordered: %v, want %v", paths(input), before)
	}
}
