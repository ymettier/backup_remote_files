package main

import (
	"backup_remote_files/config"
	"backup_remote_files/logger"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	_ "embed"

	"log/slog"
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
				Help:      "Status of latest backup",
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
				Help:      "Number of retreivals",
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

func initializeCounters(cfg config.Config, metric *metrics) {
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

func backupFile(id, url, username, password, outputFile string) (retrieveSuccess bool, err error) {
	l := logger.Get()
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, http.NoBody)
	if err != nil {
		l.Error("Failed to create new request", slog.String("id", id), slog.String("url", url), slog.Any("error", err))
		return false, err
	}
	req.SetBasicAuth(username, password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		l.Error("Failed to read data", slog.String("id", id), slog.String("url", url), slog.Any("error", err))
		return false, err
	}
	defer resp.Body.Close()

	outputFileFD, err := os.Create(outputFile + ".part")
	if err != nil {
		l.Error("Failed to open file for writing", slog.String("id", id), slog.String("filename", outputFile), slog.Any("error", err))
		return true, err
	}
	defer outputFileFD.Close()
	if _, err = io.Copy(outputFileFD, resp.Body); err != nil {
		l.Error("Failed to write contents to file", slog.String("id", id), slog.String("filename", outputFile))
		return true, err
	}
	outputFileFD.Close()

	// code 300 is http.StatusMultipleChoices
	if resp.StatusCode >= http.StatusMultipleChoices {
		l.Error("Request returned HTTP status code >= 300", slog.String("id", id), slog.String("url", url), slog.Int("status", resp.StatusCode))
		os.Remove(outputFile + ".part")
		return false, nil
	} else {
		err := os.Rename(outputFile+".part", outputFile)
		if err != nil {
			l.Error("Failed to rename file",
				slog.String("id", id),
				slog.String("oldFilename", outputFile+".part"),
				slog.String("newFilename", outputFile),
			)
			return true, err
		}
		l.Info("Successfully retrieved file", slog.String("id", id), slog.String("filename", outputFile))
	}
	return true, nil
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

func retrieveUrls(cfg config.Config, metric *metrics, status *backupStatus, retrieveAll bool) (allRetrievalsSuccess bool) {
	l := logger.Get()
	allRetrievalsSuccess = true
	if retrieveAll {
		l.Info("Starting retrieving files to backup")
	} else {
		l.Info("Retrying failed retrievals")
	}
	for _, backup := range cfg.Backups {
		if (!retrieveAll) && status.success[backup.ID] {
			// no retrieval if not retrieving all and it last retrieval was successful
			continue
		}
		if !retrieveAll {
			l.Info("Retrying...", slog.String("id", backup.ID))
		}
		status.success[backup.ID] = true
		if RetrieveSuccess, err := backupFile(backup.ID, backup.URL, backup.Username, backup.Password, backup.OutputFile); err != nil {
			// already logged in fileSize(); no need to log here
			metric.Status.With(prometheus.Labels{"id": backup.ID}).Set(float64(0))
			metric.BackupFailed.With(prometheus.Labels{"id": backup.ID}).Inc()
			status.success[backup.ID] = RetrieveSuccess
			if !RetrieveSuccess {
				allRetrievalsSuccess = false
			}
			continue
		}
		size, err := fileSize(backup.OutputFile)
		if err != nil {
			// already logged in fileSize(); no need to log here
			metric.Status.With(prometheus.Labels{"id": backup.ID}).Set(float64(0))
			metric.BackupFailed.With(prometheus.Labels{"id": backup.ID}).Inc()
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

func main() {
	// Read configuration
	cfg, err := config.New(Version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	l := logger.Get()

	// Create a non-global registry.
	reg := prometheus.NewRegistry()
	// Create new metrics and register them using the custom registry.
	m := NewMetrics(reg, cfg.MetricsPrefix)

	// Create BuildInfo metrics
	initializeCounters(cfg, m)

	// Track per-backup retrieval status separately from config
	status := newBackupStatus(cfg.Backups)

	// Create tickers for retrievals
	ticker := time.NewTicker(cfg.Interval)
	tickerRetry := time.NewTicker(cfg.RetryInterval)

	// First return
	if retrieveUrls(cfg, m, status, true) {
		tickerRetry.Stop()
	}

	// Go-routine : do backups
	go func(cfg config.Config, m *metrics, status *backupStatus) {
		for {
			select {
			case <-ticker.C:
				if retrieveUrls(cfg, m, status, true) {
					tickerRetry.Stop()
				} else {
					tickerRetry.Reset(cfg.RetryInterval)
				}
			case <-tickerRetry.C:
				if retrieveUrls(cfg, m, status, false) {
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

	l.Info("Starting exporter HTTP server", slog.Int("port", cfg.Port))
	err = server.ListenAndServe()
	if err != nil {
		l.Error("Could not start exporter HTTP server", slog.Any("error", err))
		os.Exit(1)
	}
}
