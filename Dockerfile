FROM golang:1.22.3-alpine3.20 as builder

ENV GO111MODULE=on
ENV CGO_ENABLED=0
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
COPY . .
RUN go mod download
RUN apk update && apk add git
RUN git tag --points-at HEAD > version.txt; test -s version.txt || echo dev > version.txt; cat version.txt
RUN GOARCH=$TARGETARCH GOOS=$TARGETOS go build -v -o backup_remote_files
RUN GOARCH=$TARGETARCH GOOS=$TARGETOS go test ./...

FROM alpine:edge
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/backup_remote_files /bin/backup_remote_files
ENTRYPOINT ["/bin/backup_remote_files"]
