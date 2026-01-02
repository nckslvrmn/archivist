# Archivist

A Docker-first web-based backup solution for creating regularly scheduled backups with multi-cloud storage support.

## Features

- **Multi-Cloud Storage**: AWS S3, Google Cloud Storage, Google Drive, Azure Blob Storage, Backblaze B2, and S3-compatible storage
- **Configurable Storage Tier**: Configure storage classes (S3 Glacier, GCS Nearline/Coldline/Archive, Azure Cool/Cold/Archive) to reduce costs
- **Flexible Scheduling**: Simple presets (hourly, daily, weekly) or custom cron expressions
- **Multiple Backends per Task**: Send backups to multiple storage locations simultaneously
- **Real-Time Monitoring**: Track backup progress and execution history through web interface
- **Archive or Sync Modes**: Create compressed archives or sync (mirror) directories individually
- **Retention Policies**: Automatic backup lifecycle management and cleanup
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

| Flag          | Environment Variable  | Default | Description                          |
|---------------|-----------------------|---------|--------------------------------------|
| `--root`      | `ARCHIVIST_ROOT`      | `/data` | Root data directory                  |
| `--port`      | `ARCHIVIST_PORT`      | `8080`  | HTTP server port                     |
| `--log-level` | `ARCHIVIST_LOG_LEVEL` | `info`  | Log level (debug, info, warn, error) |

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

Creates compressed tar.gz archives of source directories:

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

**Naming strategies**:

- **Timestamped** (`use_timestamp: true`): `database_20250127_143022.tar.gz`
- **Static** (`use_timestamp: false`): `database_latest.tar.gz` (overwrites previous)

### Sync Mode

Syncs files individually to backends without creating archives:

```json
{
  "mode": "sync",
  "sync_options": {
    "compare_method": "hash",
    "delete_remote": false
  }
}
```

**Compare methods**:

- `hash` - Compare SHA256 hashes (slower, most accurate)
- `mtime` - Compare modification time and size (faster)

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
      "keep_last": 24
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
  make lint          - Run linters
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
