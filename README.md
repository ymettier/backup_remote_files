# backup_remote_files

Tool to retrieve remote files like vcf and ics files, with a Prometheus exporter for monitoring.

## Motivation

This tool was initially built in order to retrieve some remote ics and vcf files (calendar and contacts) and backup them.

Because I wanted to check that the retrieval was done with success, I implemented a Prometheus Exporter that expose metrics related to the retrieved files.

This tool should run as a daemon and a control on http://localhost:9289/metrics shows if everything is going well.

Of course, a shell script like this would do the same, but without any control on the retrieved files:

```bash
while true; do
  curl -sL -u username:password http://remote.url/remote/file -o backup_file
  sleep 86400 # 1 day
done
```

## Run

Run a binary. Example:

```bash
./backup_remote_files -h
./backup_remote_files -c config.yaml
```

Run with docker or podman

```bash
podman run --rm -it ghcr.io/ymettier/backup_remote_files:latest -V

podman run --rm -it \
    -v .:/files:z \
    -p 9289:9289 \
    ghcr.io/ymettier/backup_remote_files:latest -c /files/config.yaml
```

## Configuration

See config.yaml.sample

Note: `logging.filename` can be set to one of `stdout`, `stderr` or a filename. If a filename is set, rotation of the log file will be automatically performed.

## Environment variables

All environment variables are prefixed with `BRF_`.

See config.yaml.sample for a list of environment variables that can be used to override configuration values. Example:

- `BRF_LOGGING_LEVEL`: log level

## Build

### Development Build

```bash
go mod download
echo build > version.txt
go test ./...
go build -v .
```

### Run Linters

```bash
GOLANGCILINTVERSION="2.9.0" # See https://hub.docker.com/r/golangci/golangci-lint/tags
DOCKER=podman # or DOCKER="sudo docker" or DOCKER="docker"

${DOCKER} run -t --rm --privileged -v $(pwd):/app -w /app "golangci/golangci-lint:v${GOLANGCILINTVERSION}" golangci-lint run -v
```

### Release build

```bash
go mod download
git tag <version>
git tag --points-at HEAD > version.txt
go test ./...
go build -v .
```

### Dockerfile build

Optional :

```bash
git tag <version>
```

Build:

```bash
podman build -t backup_remote_files:build .
```

/* Copyright 2024-2026 The Backup_remote_files Authors. All rights reserved. */
/* SPDX-License-Identifier: MIT */
