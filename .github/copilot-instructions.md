# GitHub Copilot Instructions

## Project Overview
This is a backup remote files application written in Go. It retrieves files from remote URLs and exports Prometheus metrics for monitoring backup status and health.

## Technology Stack
- **Language**: Go 1.27+
- **Configuration**: YAML via Koanf
- **Metrics**: Prometheus client_golang
- **Logging**: Structured logging with slog and lumberjack
- **Testing**: Go testing + testify for assertions
- **CLI**: spf13/pflag
- **Parsing configuration file**: YAML via Koanf

## Project Structure
```
.
├── config/          # Configuration parsing and CLI flags
├── logger/          # Structured logging setup
├── main.go          # Application entry point and metrics collection
├── main_test.go     # Integration tests
├── go.mod           # Go modules
└── Dockerfile       # Container image definition
```

## Key Components

### Configuration (config/config.go)
- CLI flag parsing: `-c/--config` (required), `-p/--port` (default 9289), `-V/--version`
- YAML configuration parsing using Koanf
- Support for multiple backup definitions with URL, username, password, outputFile
- Logging configuration per logger/LogOptions

### Logging (logger/logger.go)
- Structured logging using log/slog
- File rotation via lumberjack.v2
- Configurable levels: INFO, DEBUG, ERROR, WARN
- JSON and text output formats
- Configuration through config.yaml

### Main Application (main.go)
- Prometheus metrics registry for backup status tracking
- Periodic backup retrieval with interval and retry intervals
- HTTP server on configurable port serving /metrics endpoint
- Backup failure tracking and retry logic

## Development Guidelines

### Code Style
- Use structured logging (slog) instead of fmt.Printf for application output
- All public functions should have documentation comments
- Keep functions focused and under 50 lines when possible
- Use meaningful variable names

### Testing
- Write tests alongside features in `*_test.go` files
- Use testify assertions (`assert.Equal`, `assert.Nil`, `assert.FileExists`)
- Tests should be isolated and use temporary files/directories
- Always clean up test artifacts with defer

### Configuration Management
- Use Koanf for all YAML parsing
- Provide sensible defaults for all config options
- Validate configuration values at startup
- Support both short and long CLI flags

### Environment Variables
- Use optional environment variables for configuration (e.g., `BACKUP_DIR`, `LOG_LEVEL`)
- Environment variables should override values from the config file
- Environment variable names should be in uppercase with underscores (e.g., `BACKUP_DIR`, `LOG_LEVEL`)
- Environment variables should be documented in the `config.yaml.sample`
- Environment variables should be prefixed with `BRF_` (e.g., `BRF_DIR`, `BRF_LOG_LEVEL`)

### Metrics
- Use prometheus.NewGaugeVec/prometheus.NewCounterVec for metrics
- Register metrics immediately after creation
- Assign labels consistently (e.g., "id" for backup identifiers)
- Document what each metric represents in the Help text

### Error Handling
- Use slog for error logging with context
- Return errors explicitly, don't panic
- Log errors with relevant context (IDs, filenames, URLs)
- Gracefully handle missing or corrupted configuration

## Common Tasks

### Adding a New Configuration Option
1. Add field to Config struct in config/config.go
2. Add parsing logic in readConfig() function
3. Add test case in config_test.go
4. Update config.yaml.sample with example value

### Adding a New Metric
1. Define in metrics struct in main.go
2. Register in NewMetrics() with NewGaugeVec/NewCounterVec
3. Set/increment in appropriate backup retrieval logic
4. Update metrics documentation comments

### Modifying CLI Flags
1. Update parseFlags() in config/config.go
2. Add both short and long form support
3. Update help text
4. Test with -h flag

## Dependencies
- `github.com/knadh/koanf` - Configuration management
- `github.com/prometheus/client_golang` - Metrics
- `gopkg.in/natefinch/lumberjack.v2` - Log rotation
- `gopkg.in/yaml.v3` - YAML support
- `github.com/spf13/pflag` - CLI flag parsing
- `github.com/stretchr/testify` - Testing utilities

## Build & Deploy
- Build: `go build`
- Test: `go test ./...`
- Docker: `docker build -t backup_remote_files .`
- Lint: `golangci-lint run ./...`. When it fails for versionning reasons, fallback to `docker run -t --rm -v $(pwd):/app:z -w /app golangci/golangci-lint:v2.12.2 golangci-lint run ./...`

## Important Notes
- Never work in the `main` branch
- Never commit to `main` branch
- Never commit. Let the user commit their own changes. But suggest a commit message by showing the full `git commit` command.
- Configuration errors cause immediate exit with os.Exit(1)
- All file operations use .part suffix during transfer, renamed on success
- function `backupFile()` backs up a single file from the given URL to the destination path. It returns `nil` on success, `*httpError` on HTTP/network failures, and `*fsError` on local filesystem failures. Use `errors.As` in the caller to distinguish the category.
