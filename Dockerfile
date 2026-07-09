# Copyright 2024-2026 The Backup_remote_files Authors. All rights reserved.
# SPDX-License-Identifier: MIT

FROM golang:1.26.4 AS builder

ENV GO111MODULE=on
ENV CGO_ENABLED=0
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
COPY . .
RUN go mod download
RUN git tag --points-at HEAD > version.txt; test -s version.txt || echo dev > version.txt; cat version.txt
RUN GOARCH=$TARGETARCH GOOS=$TARGETOS go build -v -o backup_remote_files
RUN GOARCH=$TARGETARCH GOOS=$TARGETOS go test ./...

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/backup_remote_files /bin/backup_remote_files
ENTRYPOINT ["/bin/backup_remote_files"]
