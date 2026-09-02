# Archivist

A Docker-first web-based backup solution for creating regularly scheduled backups with multi-cloud storage support.

## Features

- **Multi-Cloud Storage**: AWS S3, Google Cloud Storage, Google Drive, Azure Blob Storage, Backblaze B2, and S3-compatible storage
- **Compression Choices**: gzip, zstd, or uncompressed tar
- **Configurable Storage Tier**: Configure storage classes (S3 Glacier, GCS Nearline/Coldline/Archive, Azure Cool/Cold/Archive) to reduce costs
- **Flexible Scheduling**: Simple presets (hourly, daily, weekly) or custom cron expressions
- **Multiple Backends per Task**: Send backups to multiple storage locations simultaneously
- **Real-Time Monitoring**: Track backup progress and execution history through web interface
- **Archive or Sync Modes**: Create compressed archives or sync (mirror) directories individually
- **Retention Policies**: Keep the newest N backups, drop anything past an age limit, or both
- **Portable Configuration**: JSON-based configuration with relative path support
- **Container-First Design**: Single-volume Docker strategy with symlink-based source management
- **Dark Minimal UI**: Clean, modern web interface

## Quick Start

### Using Docker

```bash
# Create data directory
mkdir -p ~/archivist-data

# Run the container
docker run -d \
  --name archivist \
  -p 8080:8080 \
  -v ~/archivist-data:/data \
  archivist:latest

# Create symlinks to directories you want to backup
ln -s /path/to/important/data ~/archivist-data/sources/important-data
ln -s /home/user/documents ~/archivist-data/sources/documents
```

The container automatically creates subdirectories: `config/`, `sources/`, `temp/`

### Using Docker Compose

An example `docker-compose.yml` is in the repository. Run it with `docker-compose up`.

### Running Locally

```bash
# Build and run
make build && make run
```

Access the web interface at <http://localhost:8080>

## Configuration

### Runtime Options

Configure via command-line flags or environment variables:

| Flag                | Environment Variable        | Default | Description                                        |
|---------------------|-----------------------------|---------|----------------------------------------------------|
| `--root`            | `ARCHIVIST_ROOT`            | `/data` | Root data directory                                |
| `--port`            | `ARCHIVIST_PORT`            | `8080`  | HTTP server port                                   |
| `--log-level`       | `ARCHIVIST_LOG_LEVEL`       | `info`  | Log level (debug, info, warn, error)               |
| `--allowed-origins` | `ARCHIVIST_ALLOWED_ORIGINS` | *(none)* | Extra origins allowed to open the progress WebSocket |

The WebSocket that streams backup progress accepts same-origin connections
automatically. `--allowed-origins` is only needed when the browser reaches
Archivist under a different host than the one it sees in the `Host` header
(comma-separated, e.g. `https://archivist.example.com`).

All paths are derived from the root directory:

- Config file: `{root}/config/config.json`
- Database: `{root}/config/archivist.db`
- Temp files: `{root}/temp/`
- Source symlinks: `{root}/sources/`

### Path Resolution

Archivist supports absolute and relative paths in configurations:

- **Absolute paths**: Used as-is (e.g., `/data/sources/mydata`)
- **Relative paths**: Resolved relative to root directory (e.g., `sources/mydata` → `{root}/sources/mydata`)

Using relative paths makes your configuration portable between environments.

### Settings

`config/config.json` holds a `settings` object alongside tasks and backends:

| Setting                   | Default | Description                                                        |
|---------------------------|---------|--------------------------------------------------------------------|
| `max_concurrent_backends` | `4`     | Per-task fan-out cap when a task targets several backends           |
| `max_concurrent_uploads`  | `4`     | Files compared and uploaded at once during a sync                   |
| `execution_history_days`  | `90`    | Execution records older than this are pruned; `0` keeps them forever |

### File Permissions

`config/config.json` stores backend credentials in cleartext, so Archivist
writes it with mode `0600` inside a `0700` directory, and tightens both on
startup for installations created by earlier versions. Keep any service
account JSON files in the same directory.

## Supported Storage Backends

### Local Filesystem

Simple local storage for backups. Relative paths are resolved from the root directory.

<details>
<summary>View configuration details</summary>

```json
{
  "type": "local",
  "config": {
    "path": "backups"
  }
}
```

For Docker: `backups` → `/data/backups`

</details>

### AWS S3

Full support for all S3 storage classes including Glacier for cost optimization.

<details>
<summary>View configuration details</summary>

```json
{
  "type": "s3",
  "config": {
    "region": "us-east-1",
    "bucket": "my-backups",
    "prefix": "archivist/",
    "storage_tier": "GLACIER_IR",
    "access_key_id": "...",
    "secret_access_key": "..."
  }
}
```

**Valid storage classes** (optional, defaults to `STANDARD`):

- `STANDARD` - Frequent access, highest cost
- `STANDARD_IA` - Infrequent access
- `ONEZONE_IA` - Single AZ, infrequent access
- `INTELLIGENT_TIERING` - Automatic cost optimization
- `GLACIER_IR` - Instant retrieval archive
- `GLACIER` - Archive with 3-5 hour retrieval
- `DEEP_ARCHIVE` - Long-term archive, 12+ hour retrieval

</details>

### S3-Compatible Storage

Works with MinIO, DigitalOcean Spaces, Wasabi, and other S3-compatible services.

<details>
<summary>View configuration details</summary>

```json
{
  "type": "s3",
  "config": {
    "endpoint": "https://nyc3.digitaloceanspaces.com",
    "region": "us-east-1",
    "bucket": "my-backups",
    "access_key_id": "...",
    "secret_access_key": "..."
  }
}
```

</details>

### Google Cloud Storage

Full support for all GCS storage classes including Nearline, Coldline, and Archive.

<details>
<summary>View configuration details</summary>

```json
{
  "type": "gcs",
  "config": {
    "bucket": "my-backups",
    "prefix": "archivist/",
    "storage_tier": "NEARLINE",
    "credentials_file": "config/gcs-credentials.json"
  }
}
```

**Valid storage classes** (optional, defaults to `STANDARD`):

- `STANDARD` - Frequent access, highest cost
- `NEARLINE` - 30-day minimum, lower cost
- `COLDLINE` - 90-day minimum, very low cost
- `ARCHIVE` - 365-day minimum, cheapest

**Authentication options**:

- `credentials_file` - Path to service account JSON file (relative paths supported)
- `credentials_json` - Service account JSON as string
- If neither provided, uses Application Default Credentials (ADC)

</details>

### Google Drive

Store backups directly to Google Drive folders.

<details>
<summary>View configuration details</summary>

```json
{
  "type": "gdrive",
  "config": {
    "folder_id": "1abc...",
    "credentials_file": "config/gdrive-credentials.json"
  }
}
```

</details>

### Azure Blob Storage

Full support for Azure access tiers including Cool, Cold, and Archive.

<details>
<summary>View configuration details</summary>

```json
{
  "type": "azure",
  "config": {
    "account_name": "myaccount",
    "account_key": "...",
    "container": "backups",
    "storage_tier": "Cool"
  }
}
```

**Valid access tiers** (optional, defaults to account default):

- `Hot` - Frequent access, highest cost
- `Cool` - 30-day minimum, lower cost
- `Cold` - 90-day minimum, very low cost
- `Archive` - 180-day minimum, cheapest, requires rehydration

</details>

### Backblaze B2

Cost-effective cloud storage with S3-compatible API.

<details>
<summary>View configuration details</summary>

```json
{
  "type": "b2",
  "config": {
    "account_id": "...",
    "application_key": "...",
    "bucket": "my-backups"
  }
}
```

</details>

## Archive Modes

### Archive Mode (Default)

Creates a compressed tar archive of the source directory:

```json
{
  "mode": "archive",
  "archive_options": {
    "format": "tar.gz",
    "compression": "gzip",
    "use_timestamp": true
  }
}
```

**Supported formats**:

| `format`   | `compression` | Extension  | Notes                                     |
|------------|---------------|------------|-------------------------------------------|
| `tar.gz`   | `gzip`        | `.tar.gz`  | Default; widest compatibility             |
| `tar.zst`  | `zstd`        | `.tar.zst` | Faster and smaller than gzip              |
| `tar`      | `none`        | `.tar`     | No compression                            |

**Naming strategies**:

- **Timestamped** (`use_timestamp: true`): `database_20250127_143022.tar.gz`
- **Static** (`use_timestamp: false`): `database_latest.tar.gz` (overwrites previous)

**Symlinks and special files**: symlinks are archived as symlinks (their
targets are not followed), FIFOs and device nodes are recorded without being
read, and sockets are skipped. Files that cannot be read are skipped with a
warning rather than failing the whole backup.

### Sync Mode

Syncs files individually to backends without creating archives:

```json
{
  "mode": "sync",
  "sync_options": {
    "compare_method": "auto",
    "delete_remote": false
  }
}
```

**Compare methods** (a size difference always forces an upload):

- `auto` (default) - Compare the remote content hash when the backend exposes
  one, otherwise fall back to size and modification time
- `hash` - Compare content hashes only; re-upload when no remote hash exists
- `mtime` - Compare size and modification time
- `size` - Compare size alone (fastest, least accurate)

**Deleting remote files**: with `delete_remote: true`, remote files missing
from the source are removed. Archivist refuses to do this when the source
scans as empty but the remote is not, so an unmounted volume cannot be
mirrored into a deletion of your only copy.

## Retention

Archive tasks can limit how many backups are kept, how old they may get, or
both:

```json
{
  "retention_policy": {
    "keep_last": 24,
    "keep_days": 30
  }
}
```

- `keep_last` - Keep the newest N backups (`0` = unlimited)
- `keep_days` - Delete backups older than N days (`0` = disabled)

The limits combine: a backup is removed if it falls outside `keep_last` **or**
is older than `keep_days`. Two rules always win over the policy — the most
recent backup is never deleted, and a backup whose timestamp cannot be read is
left alone. Retention applies to archive mode only; sync mode is governed by
`delete_remote`.

Execution history is pruned separately, via the `execution_history_days`
setting.

## Volume Strategy

Archivist uses a single-volume approach with symlinks:

1. Mount one volume at `/data`:

   ```bash
   docker run -v ~/archivist-data:/data archivist:latest
   ```

2. The application creates: `/data/config/`, `/data/sources/`, `/data/temp/`

3. Create symlinks to backup sources:

   ```bash
   ln -s /path/to/database ~/archivist-data/sources/database
   ln -s /home/user/documents ~/archivist-data/sources/documents
   ```

4. Configure tasks with relative paths: `sources/database`, `sources/documents`

**Benefits**:

- Single volume mount - simple Docker configuration
- Portable configs - relative paths work everywhere
- Easy source management - add backups via symlinks
- Self-contained - move entire data directory

## API

Archivist provides a RESTful API. Here are some basic examples.

```bash
# List all tasks
curl http://localhost:8080/api/v1/tasks

# Create a task
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Hourly Logs Backup",
    "source_path": "sources/logs",
    "backend_ids": ["local-backup"],
    "schedule": {
      "type": "simple",
      "simple_type": "hourly"
    },
    "archive_options": {
      "format": "tar.gz",
      "use_timestamp": true
    },
    "retention_policy": {
      "keep_last": 24,
      "keep_days": 30
    },
    "enabled": true
  }'

# Manually trigger a backup
curl -X POST http://localhost:8080/api/v1/tasks/task-id/execute
```

## Development

### Prerequisites

- Go 1.21 or later
- Make
- Docker (optional)

### Make Commands

```bash
Archivist - Makefile Commands

  make test          - Run all tests
  make lint          - Run linters and formatting checks
  make vulncheck     - Report known vulnerabilities in dependencies
  make clean         - Clean build artifacts
  make build         - Build the Go binary
  make run           - Run the application locally
  make docker        - Build the Docker image
```

## Contributing

Contributions are welcome!

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

MIT License - see [LICENSE](LICENSE) for details
