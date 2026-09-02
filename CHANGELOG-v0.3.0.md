# v0.3.0

Bug-fix and hardening release. Several of these were silent data-loss bugs — if you back up anything under a symlink, upgrading is strongly recommended.

## Fixed

- **Symlinked sources no longer fail.** A `source_path` pointing at a symlink (the documented `sources/` layout) produced a failed backup, and a single symlink anywhere in a tree failed the whole archive. Symlinks are now stored as symlinks, FIFOs and devices are recorded without being read, sockets are skipped, and unreadable files are skipped with a warning instead of aborting the run.
- **`GET /api/v1/config` no longer destroys stored credentials.** Credential masking wrote through a shallow copy into the live config, which the next save persisted.
- **A task can no longer run twice at once.** The "already running" check and the claim now happen under one lock.
- **Executions no longer stay `running` forever** after a restart; interrupted runs are closed out at startup.
- **SQLite locking.** WAL, a 5s busy timeout, and enforced foreign keys; execution listing no longer issues a query per row.
- **Cancellation works during the archive phase**, and cancelled runs are recorded as `cancelled` rather than `failed`. Retention no longer runs on a cancelled context.
- **Retention and skip-unchanged no longer list the entire bucket** on every upload.

## Added

- **zstd and uncompressed tar** (`.tar.zst`, `.tar`) alongside gzip, selectable per task.
- **Retention by age** (`keep_days`), combinable with `keep_last`. The newest backup is never deleted.
- **Execution history pruning** via `execution_history_days` (default 90).
- **Parallel sync uploads**, bounded by `max_concurrent_uploads` (default 4).
- **Sync `compare_method`**: `auto`, `hash`, `mtime`, `size`.
- **Local backend hashing**, so skip-unchanged and post-upload integrity checks work for local backups instead of silently doing nothing.
- **Empty-source guard**: sync refuses to delete remote files when the source scans as empty but the remote is not, so an unmounted volume can't be mirrored into a deletion.

## Security

- `config.json` is now written `0600` inside a `0700` directory, and both are tightened on startup for existing installs. It stores backend credentials in cleartext.
- WebSocket connections are validated against the request origin. Add `--allowed-origins` / `ARCHIVIST_ALLOWED_ORIGINS` if the browser reaches Archivist under a different host.
- Base image packages are upgraded at build time to pick up published security fixes.

## Internal

- First test suite: 45 tests across 9 packages, run with `-race` in CI.
- New CI: golangci-lint, govulncheck, and Trivy image scanning.
- Go 1.27, all modules updated.

## Upgrading

No config migration needed. Existing tasks keep gzip; `keep_days` defaults to disabled and `execution_history_days` to 90 days, so old execution records are pruned on first start.
