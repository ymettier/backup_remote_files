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

## Environment variables

- `LOG_LEVEL`: log level
- `LOG_TXT_FILENAME`: filename for plain text logs.
- `LOG_JSON_FILENAME`: filename for json logs.

## Log management

The files specified in `LOG_TXT_FILENAME` and/or `LOG_JSON_FILENAME` can be either

- a file name
- `stdout`
- `stderr`

If a file name is specified, a log rotation will be performed.

If none of `LOG_TXT_FILENAME` or `LOG_JSON_FILENAME` is specified, logs will be output to `stdout`, same as `LOG_TXT_FILENAME=stdout`.

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
GOLANGCILINTVERSION="1.59.0" # See https://hub.docker.com/r/golangci/golangci-lint/tags
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
