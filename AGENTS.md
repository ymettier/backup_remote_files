/* Copyright 2026 The Backup_remote_files Authors. All rights reserved. */
/* SPDX-License-Identifier: MIT */

# AGENTS.md

## Project Overview
This is a backup remote files application written in Go. It retrieves files from remote URLs and exports Prometheus metrics for monitoring backup status and health.

## Technology Stack
- **Language**: Go 1.26+
- **Configuration**: YAML via Koanf
- **Metrics**: Prometheus client_golang
- **Logging**: Structured logging with slog and lumberjack
- **Testing**: Go testing + testify for assertions
- **CLI**: spf13/pflag

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
- Use `gofmt` / `goimports` formatting. Max line length 140.
- Group imports: stdlib first, third-party second, internal (`backup_remote_files/...`) last.
- Copyright header on every `.go` file:
  ```go
  // Copyright 2024-2026 The Backup_remote_files Authors. All rights reserved.
  // SPDX-License-Identifier: MIT
  ```
- Copyright year is always 20XX-20YY or just 20XX if 20XX and 20YY are the same. 20XX is the file's creation year (from `git log --diff-filter=A --follow` for that file). 20YY is the current year.
- No copyright on `version.txt`
- Put copyright on `README.md` at the end of the file.
- All source file should have a copyright header (the syntax depends on the file type). For non-go files, use the appropriate comment syntax (e.g., `//` for `.txt`, `/*` for `.md`) and set a header similar to the `.go` files.

### Linting
- flags (like `-c` or `-f`) are never constants. When the linter complain, add a `//nolint` directive to the line.

### Naming
- Package names: single word, lowercase, matching directory name.
- Files: `package.go` and `package_test.go` (same package, not `_test` external).
- Constants: PascalCase or ALL_CAPS for string constants. Exported types: PascalCase. Unexported: camelCase.

### Patterns
- Constructors: `New()` returns a value (not pointer) for small structs.
- Logger: singleton via `logger.Get()`.
- Context: pass `context.Context` to operations that may need cancellation.

### Configuration Management
- Use Koanf for all YAML parsing
- Use spf13/pflag for CLI flag parsing
- Provide sensible defaults for all config options
- Validate configuration values at startup
- Support both short and long CLI flags
- With Koanf, prefer getting typed values than using the Get() method.

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

### Key Dependencies
- Avoid `github.com/sirupsen/logrus` (blocked by depguard linter).
- Use `interface{}` → rewritten to `any` by gofmt.

### Testing Conventions
- Write tests alongside features in `*_test.go` files
- Use testify assertions (`assert.Equal`, `assert.Nil`, `assert.FileExists`)
- Tests should be isolated and use temporary files/directories
- Always clean up test artifacts with defer

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
- Never commit, never stage (`git add`), never run any `git commit` command — even if the user explicitly asks you to commit. If you ask and the user says yes, you can commit.
- Instead, always suggest a full `git commit` command for the user to run themselves.
- Never work in the `main` branch.
- Never commit to `main` branch.
- Commit message should be clear and descriptive.
- Commit message should follow the [Conventional Commits](https://www.conventionalcommits.org/) specification.
- Commit message should be in the format: `<type>: <description>`.
- Commit message should be lowercase and should not start with a capital letter.
- Commit message should be descriptive and should not be too short.

## Build & Run
- Build: `go build`
- Test: `go test ./...`
- Docker: `docker build -t backup_remote_files .`

## Version Management
- Keep Go version in `Dockerfile` and `.github/workflows/*.yml` in sync. Use the latest patch release (e.g., `1.26.5` not `1.26` or `stable`).
- `go.mod` is the exception: its `go` directive sets the minimum Go version. Only bump when the code requires a newer toolchain feature.
- Keep all tooling in `.github/workflows/` (goreleaser, golangci-lint, actions/\*) at their latest stable versions.
- When updating a version, check all references across the project (go.mod, Dockerfile, workflows, AGENTS.md).

## Linting
- Lint: `golangci-lint run ./...`. When it fails for versioning reasons, fallback to `docker run -t --rm -v $(pwd):/app:z -w /app golangci/golangci-lint:v2.12.2 golangci-lint run ./...`

## Important Notes
- Configuration errors cause immediate exit with os.Exit(1)
- All file operations use .part suffix during transfer, renamed on success
- function `backupFile()` backs up a single file from the given URL to the destination path. It returns `nil` on success, `*httpError` on HTTP/network failures, and `*fsError` on local filesystem failures. Use `errors.As` in the caller to distinguish the category.
