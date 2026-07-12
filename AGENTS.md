<!-- Copyright 2026 The Backup_remote_files Authors. All rights reserved. -->
<!-- SPDX-License-Identifier: MIT -->

# AGENTS.md

## Project Overview
This is a backup remote files application written in Go. It retrieves files from remote URLs
and exports Prometheus metrics for monitoring backup status and health.

## Technology Stack
- **Language**: Go 1.26+
- **Configuration**: `github.com/knadh/koanf` (YAML parsing)
- **CLI parsing**: `github.com/spf13/pflag`
- **Metrics**: Prometheus client_golang
- **Logging**: `log/slog` (stdlib, text handler, defaults to stderr)
- **Log rotation**: `gopkg.in/natefinch/lumberjack.v2`
- **Testing**: `testing` + `github.com/stretchr/testify/assert`
- **Build**: GoReleaser, CGO_ENABLED=0, Linux only (amd64/arm64)
- Avoid `github.com/sirupsen/logrus` (blocked by depguard linter)

## Project Structure
```
.
├── .github/         # GitHub Actions workflows
├── .golangci.yml    # Linter configuration
├── .goreleaser.yaml # Release automation
├── config/          # Configuration parsing and CLI flags
├── logger/          # Structured logging setup
├── testutil/        # Test helpers
├── main.go          # Application entry point and metrics collection
├── main_test.go     # Integration tests
├── go.mod / go.sum  # Go modules
├── Dockerfile       # Container image definition
├── config.yaml.sample  # Example configuration
└── version.txt      # Build version
```

## Key Components

### Configuration (config/config.go)
- CLI flag parsing: `-c/--config` (required), `-p/--port` (default 9289), `-V/--version`
- Support for multiple backup definitions with URL, username, password, outputFile
- Logging configuration per logger/LogOptions
- Use Koanf for all YAML parsing
- Use spf13/pflag for CLI flag parsing
- Provide sensible defaults for all config options
- Validate configuration values at startup
- Support both short and long CLI flags
- With Koanf, prefer getting typed values than using the Get() method.

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
- Use `gofmt` / `goimports` formatting. Max line length 140.
- Group imports: stdlib first, third-party second, internal (`backup_remote_files/...`) last.
- Flags (like `-c` or `-f`) are never constants. When the linter complains, add `//nolint`.
- Copyright header on every source file. For `.go` files:
  ```go
  // Copyright 2024-2026 The Backup_remote_files Authors. All rights reserved.
  // SPDX-License-Identifier: MIT
  ```
- Copyright year: `20XX-20YY` (creation year to current year), or `20XX` if same year.
  Derive `20XX` from `git log --diff-filter=A --follow <file>`.
- No copyright on `version.txt`. README.md copyright goes at end of file.
- All source file should have a copyright header (the syntax depends on the file type).
  For non-go files, use the appropriate comment syntax (e.g., `//` for `.txt`, `<!--` and `-->` for `.md`)
  and set a header similar to the `.go` files.
- Use `any` instead of `interface{}` (gofmt rewrites it)

### Naming
- Package names: single word, lowercase, matching directory name.
- Files: `package.go` and `package_test.go` (same package, not `_test` external).
- Constants: PascalCase or ALL_CAPS for string constants. Exported types: PascalCase. Unexported: camelCase.

### Patterns
- Constructors: `New()` returns a value (not pointer) for small structs.
- Logger: singleton via `logger.Get()`.
- Context: pass `context.Context` to operations that may need cancellation.

### Environment Variables
- Use optional environment variables for configuration (e.g., `BRF_BACKUP_DIR`...)
- Environment variables should override values from the config file
- Environment variable names should be in uppercase with underscores (e.g., `BRF_BACKUP_DIR`...)
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
- Configuration errors cause immediate exit with os.Exit(1)

### Logging (logger/logger.go)
- Structured logging using log/slog
- File rotation via lumberjack.v2
- Configurable levels: INFO, DEBUG, ERROR, WARN
- JSON and text output formats
- Configuration through config.yaml

### Testing Conventions
- Write tests alongside features in `*_test.go` files
- Use testify assertions (`assert.Equal`, `assert.Nil`, `assert.FileExists`)
- Tests should be isolated and use temporary files/directories
- Always clean up test artifacts with defer
- Test data files must be placed in the `testdata/` directory
- Unused testdata files must be removed

## HTTP Client Design

The application creates a new HTTP client per request in `main.go`. This design is intentional and well-suited for this workload because:

1. **Periodic backups**: Backups typically run every 24 hours or so, so connection pooling benefits are minimal
2. **Different hosts**: Each backup may target a different host, making a per-host pool impractical
3. **Isolation**: Fresh clients provide clean state per backup operation, avoiding resource leaks
4. **Simplicity**: Simpler code without complex connection management for edge cases

This approach avoids the complexity of maintaining connection pools while still being performant for the usage pattern.

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

### Updating Go Version
1. Update `go 1.xx.x` in `go.mod`
2. Update Go version reference in AGENTS.md
3. Update base image in Dockerfile (builder and runtime)
4. Update `.golangci.yml` if it references a Go version
5. Run `go mod tidy` after updating

### Modifying Docker Image
1. Update the builder base image in Dockerfile
2. Update the runtime base image in Dockerfile
3. Adjust package manager commands (apk → apt-get when switching from Alpine to Debian)
4. Test with `docker build -t backup_remote_files .`

## Dependencies
- `github.com/knadh/koanf` - Configuration management
- `github.com/prometheus/client_golang` - Metrics
- `gopkg.in/natefinch/lumberjack.v2` - Log rotation
- `github.com/spf13/pflag` - CLI flag parsing
- `github.com/stretchr/testify` - Testing utilities

## Commits
- Never commit, never stage (`git add`), never run `git commit` — even if explicitly asked. Always suggest the command for the user to run.
- Never work in or commit to the `main` branch.
- Commit message: clear, descriptive, lowercase, no capital start.
- Follow [Conventional Commits](https://www.conventionalcommits.org/): `<type>: <description>`.
- Before commit, always check the copyright in the files to commit.

## Build & Run
- Build: `go build`
- Test: `go test ./...`
- Docker: `docker build -t backup_remote_files .`
- CLI flags: `-c` (config), `-V` (version).

## Linting
- Run: `golangci-lint run ./...`
- Fallback (version mismatch):
  `docker run -t --rm -v $(pwd):/app:z -w /app golangci/golangci-lint:v2.12.2 golangci-lint run ./...`

## Version Management
- Keep Go version in `Dockerfile` and `.github/workflows/*.yml` in sync.
  Use the latest patch release (e.g., `1.26.5` not `1.26` or `stable`).
- `go.mod` is the exception: its `go` directive sets the minimum Go version. Only bump when the code requires a newer toolchain feature.
- Keep all tooling in `.github/workflows/` (goreleaser, golangci-lint, actions/\*) at their latest stable versions.
- When updating a version, check all references across the project (go.mod, Dockerfile, workflows, AGENTS.md).
- golangci-lint version in `README.md` (`GOLANGCILINTVERSION`) must stay in sync with
  `.github/workflows/golangci-lint.yml` and any reference in AGENTS.md.

## Important Notes
- All file operations use .part suffix during transfer, renamed on success
- function `backupFile()` backs up a single file from the given URL to the destination path.
  Returns `nil` on success, `*httpError` on HTTP/network failures, and `*fsError` on local
  filesystem failures. Use `errors.As` in the caller to distinguish the category.
