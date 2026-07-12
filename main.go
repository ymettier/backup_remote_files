// Copyright 2024-2026 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"backup_remote_files/config"
	"backup_remote_files/logger"
)

var (
	Version string = strings.TrimSpace(version)
	//go:embed version.txt
	version string
)

type metrics struct {
	BuildInfo    *prometheus.GaugeVec
	Status       *prometheus.GaugeVec
	Size         *prometheus.GaugeVec
	Time         *prometheus.GaugeVec
	BackupFailed *prometheus.CounterVec
	BackupTotal  prometheus.Counter
}

func NewMetrics(reg prometheus.Registerer, namespace string) *metrics {
	m := &metrics{
		BuildInfo: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "build_info",
				Help:      "Build information",
			},
			[]string{"goarch", "goos", "goversion", "version"},
		),
		Status: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "backup_status",
				Help:      "Status of latest backup",
			},
			[]string{"id"}, //nolint:goconst // label value is not constant
		),
		Size: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "backup_size",
				Help:      "Size of latest backup",
			},
			[]string{"id"},
		),
		Time: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "backup_time",
				Help:      "Timestamp of latest backup",
			},
			[]string{"id"},
		),
		BackupFailed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "backup_failed",
				Help:      "Number of failed backups",
			},
			[]string{"id"},
		),
		BackupTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "backup_nb",
				Help:      "Number of retrievals",
			},
		),
	}
	reg.MustRegister(m.BuildInfo)
	reg.MustRegister(m.Status)
	reg.MustRegister(m.Size)
	reg.MustRegister(m.Time)
	reg.MustRegister(m.BackupFailed)
	reg.MustRegister(m.BackupTotal)
	return m
}

func initializeCounters(metric *metrics) {
	metric.BuildInfo.With(prometheus.Labels{
		"goarch":    runtime.GOARCH,
		"goos":      runtime.GOOS,
		"goversion": runtime.Version(),
		"version":   Version,
	}).Set(float64(1))
}

type backupStatus struct {
	success map[string]bool
}

func newBackupStatus(backups []config.Backup) *backupStatus {
	s := make(map[string]bool, len(backups))
	for _, b := range backups {
		s[b.ID] = true
	}
	return &backupStatus{success: s}
}

type httpError struct{ error }

func (e *httpError) Unwrap() error { return e.error }

type fsError struct{ error }

func (e *fsError) Unwrap() error { return e.error }

func fetchURL(
	ctx context.Context,
	url, username, password string,
	timeout time.Duration,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, &httpError{error: err}
	}
	req.SetBasicAuth(username, password)

	// Create a new client per request — these are periodic backups
	// (typically every 24h), so connection pooling is not beneficial
	// and each backup may target a different host.
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: timeout}).DialContext,
			TLSHandshakeTimeout:   timeout,
			ResponseHeaderTimeout: timeout,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, &httpError{error: err}
	}

	if resp.StatusCode >= http.StatusMultipleChoices {
		resp.Body.Close()
		return nil, &httpError{error: fmt.Errorf("unexpected status %d %s", resp.StatusCode, resp.Status)}
	}

	return resp, nil
}

func saveToFile(r io.Reader, outputFile string) (int64, error) {
	f, err := os.Create(outputFile + ".part")
	if err != nil {
		return 0, &fsError{error: err}
	}
	defer f.Close()

	written, err := io.Copy(f, r)
	if err != nil {
		return 0, &fsError{error: err}
	}

	if err := os.Rename(outputFile+".part", outputFile); err != nil {
		return 0, &fsError{error: err}
	}

	return written, nil
}

func backupFile(
	ctx context.Context,
	id, url, username, password, outputFile string,
	timeout time.Duration,
) (int64, error) {
	l := logger.Get()

	resp, err := fetchURL(ctx, url, username, password, timeout)
	if err != nil {
		l.Error("Failed to fetch URL",
			slog.String("id", id),
			slog.String("url", url),
			slog.Any("error", err),
		)
		return 0, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			l.Error("Failed to close response body", slog.String("id", id), slog.Any("error", err))
		}
	}()

	written, err := saveToFile(resp.Body, outputFile)
	if err != nil {
		l.Error("Failed to save file",
			slog.String("id", id),
			slog.String("filename", outputFile),
			slog.Any("error", err),
		)
		return 0, err
	}

	l.Info("Successfully retrieved file",
		slog.String("id", id),
		slog.String("filename", outputFile),
	)
	return written, nil
}

func recordBackupFailed(metric *metrics, backupID string) {
	metric.Status.With(prometheus.Labels{"id": backupID}).Set(float64(0))
	metric.BackupFailed.With(prometheus.Labels{"id": backupID}).Inc()
}

func processBackup(
	ctx context.Context,
	backup *config.Backup,
	metric *metrics,
	status *backupStatus,
) (isHTTPError bool) {
	status.success[backup.ID] = true
	size, err := backupFile(
		ctx,
		backup.ID,
		backup.URL,
		backup.Username,
		backup.Password,
		backup.OutputFile,
		backup.Timeout,
	)
	if err != nil {
		recordBackupFailed(metric, backup.ID)
		var target *httpError
		isHTTP := errors.As(err, &target)
		status.success[backup.ID] = !isHTTP
		return isHTTP
	}
	metric.Status.With(prometheus.Labels{"id": backup.ID}).Set(float64(1))
	metric.Size.With(prometheus.Labels{"id": backup.ID}).Set(float64(size))
	metric.Time.With(prometheus.Labels{"id": backup.ID}).Set(float64(time.Now().Unix()))
	return false
}

func retrieveUrls(ctx context.Context, cfg config.Config, metric *metrics,
	status *backupStatus, retrieveAll bool) (allRetrievalsSuccess bool) {
	l := logger.Get()
	allRetrievalsSuccess = true
	if retrieveAll {
		l.Info("Starting retrieving files to backup")
	} else {
		l.Info("Retrying failed retrievals")
	}
	for _, backup := range cfg.Backups {
		if !retrieveAll && status.success[backup.ID] {
			continue
		}
		if !retrieveAll {
			l.Info("Retrying...", slog.String("id", backup.ID))
		}
		if processBackup(ctx, &backup, metric, status) {
			allRetrievalsSuccess = false
		}
	}
	metric.BackupTotal.Inc()
	l.Info("End of retrieving files to backup")
	return allRetrievalsSuccess
}

func runBackupLoop(
	ctx context.Context,
	cfg config.Config,
	m *metrics,
	status *backupStatus,
	ticker, tickerRetry *time.Ticker,
) {
	defer ticker.Stop()
	defer tickerRetry.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if retrieveUrls(ctx, cfg, m, status, true) {
				tickerRetry.Stop()
			} else {
				tickerRetry.Reset(cfg.RetryInterval)
			}
		case <-tickerRetry.C:
			if retrieveUrls(ctx, cfg, m, status, false) {
				tickerRetry.Stop()
			}
		}
	}
}

func startMetricsServer(ctx context.Context, reg *prometheus.Registry, port int) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))

	server := &http.Server{
		Addr:              ":" + strconv.Itoa(port),
		ReadHeaderTimeout: 3 * time.Second,
		Handler:           mux,
	}

	go func() { //nolint:gosec
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:mnd
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	l := logger.Get()
	l.Info("Starting exporter HTTP server", slog.Int("port", port))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		l.Error("Could not start exporter HTTP server", slog.Any("error", err))
		return err
	}

	return nil
}

func run(ctx context.Context) error {
	flags, err := config.ParseFlags(Version, os.Args[1:])
	if err != nil {
		return err
	}
	cfg, err := config.New(flags.ConfigFile, flags.Port)
	if err != nil {
		return err
	}

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)

	initializeCounters(m)

	status := newBackupStatus(cfg.Backups)

	ticker := time.NewTicker(cfg.Interval)
	tickerRetry := time.NewTicker(cfg.RetryInterval)

	if retrieveUrls(ctx, cfg, m, status, true) {
		tickerRetry.Stop()
	}

	go runBackupLoop(ctx, cfg, m, status, ticker, tickerRetry)

	return startMetricsServer(ctx, reg, cfg.Port)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)

	if err := run(ctx); err != nil {
		cancel()
		if !errors.Is(err, flag.ErrHelp) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}
