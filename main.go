// Copyright 2024-2026 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
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

func backupFile(ctx context.Context, id, url, username, password, outputFile string) error {
	l := logger.Get()
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		l.Error("Failed to create new request", slog.String("id", id), slog.String("url", url), slog.Any("error", err))
		return &httpError{err}
	}
	req.SetBasicAuth(username, password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		l.Error("Failed to read data", slog.String("id", id), slog.String("url", url), slog.Any("error", err))
		return &httpError{err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		l.Error("Request returned HTTP status code >= 300", slog.String("id", id), slog.String("url", url), slog.Int("status", resp.StatusCode))
		return &httpError{errors.New("HTTP status >= 300")}
	}

	outputFileFD, err := os.Create(outputFile + ".part")
	if err != nil {
		l.Error("Failed to open file for writing", slog.String("id", id), slog.String("filename", outputFile), slog.Any("error", err))
		return &fsError{err}
	}
	defer outputFileFD.Close()
	if _, err = io.Copy(outputFileFD, resp.Body); err != nil {
		l.Error("Failed to write contents to file", slog.String("id", id), slog.String("filename", outputFile))
		return &fsError{err}
	}

	err = os.Rename(outputFile+".part", outputFile)
	if err != nil {
		l.Error("Failed to rename file",
			slog.String("id", id),
			slog.String("oldFilename", outputFile+".part"),
			slog.String("newFilename", outputFile),
		)
		return &fsError{err}
	}
	l.Info("Successfully retrieved file", slog.String("id", id), slog.String("filename", outputFile))
	return nil
}

func fileSize(filename string) (int64, error) {
	l := logger.Get()
	fi, err := os.Stat(filename)
	if err != nil {
		l.Error("Failed to get file stats", slog.String("filename", filename), slog.Any("error", err))
		return 0, err
	}
	return fi.Size(), nil
}

func recordBackupFailed(metric *metrics, backupID string) {
	metric.Status.With(prometheus.Labels{"id": backupID}).Set(float64(0))
	metric.BackupFailed.With(prometheus.Labels{"id": backupID}).Inc()
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
			// no retrieval if not retrieving all and its last retrieval was successful
			continue
		}
		if !retrieveAll {
			l.Info("Retrying...", slog.String("id", backup.ID))
		}
		status.success[backup.ID] = true
		if err := backupFile(ctx, backup.ID, backup.URL, backup.Username, backup.Password, backup.OutputFile); err != nil {
			recordBackupFailed(metric, backup.ID)
			var target *httpError
			isHTTP := errors.As(err, &target)
			status.success[backup.ID] = !isHTTP
			if isHTTP {
				allRetrievalsSuccess = false
			}
			continue
		}
		size, err := fileSize(backup.OutputFile)
		if err != nil {
			recordBackupFailed(metric, backup.ID)
			continue
		}
		metric.Status.With(prometheus.Labels{"id": backup.ID}).Set(float64(1))
		metric.Size.With(prometheus.Labels{"id": backup.ID}).Set(float64(size))
		metric.Time.With(prometheus.Labels{"id": backup.ID}).Set(float64(time.Now().Unix()))
	}
	metric.BackupTotal.Inc()
	l.Info("End of retrieving files to backup")
	return allRetrievalsSuccess
}

func run(ctx context.Context) error {
	flags := config.ParseFlags(Version, os.Args[1:])
	cfg, err := config.New(flags.ConfigFile, flags.Port)
	if err != nil {
		return err
	}

	l := logger.Get()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)

	initializeCounters(m)

	status := newBackupStatus(cfg.Backups)

	ticker := time.NewTicker(cfg.Interval)
	tickerRetry := time.NewTicker(cfg.RetryInterval)

	if retrieveUrls(ctx, cfg, m, status, true) {
		tickerRetry.Stop()
	}

	go func(cfg config.Config, m *metrics, status *backupStatus) {
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
	}(cfg, m, status)

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))

	server := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		ReadHeaderTimeout: 3 * time.Second,
	}

	go func() { //nolint:gosec
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:mnd
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	l.Info("Starting exporter HTTP server", slog.Int("port", cfg.Port))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		l.Error("Could not start exporter HTTP server", slog.Any("error", err))
		return err
	}

	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
