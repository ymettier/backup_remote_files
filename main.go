package main

import (
	"backup_remote_files/config"
	"backup_remote_files/logger"
	"context"
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

	"go.uber.org/zap"
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
			[]string{"id"},
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

	for _, backup := range cfg.Backups {
		metric.BackupFailed.With(prometheus.Labels{"id": backup.ID}).Add(0)
	}
}

func backupFile(id, url, username, password, outputFile string) (retrieveSuccess bool, err error) {
	l := logger.Get()
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, http.NoBody)
	if err != nil {
		l.Error("Failed to create new request", zap.String("id", id), zap.String("url", url), zap.Error(err))
		return false, err
	}
	req.SetBasicAuth(username, password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		l.Error("Failed to read data", zap.String("id", id), zap.String("url", url), zap.Error(err))
		return false, err
	}
	defer resp.Body.Close()

	outputFileFD, err := os.Create(outputFile)
	if err != nil {
		l.Error("Failed to open file for writing", zap.String("id", id), zap.String("filename", outputFile), zap.Error(err))
		return true, err
	}
	defer outputFileFD.Close()
	if _, err = io.Copy(outputFileFD, resp.Body); err != nil {
		l.Error("Failed to write contents to file", zap.String("id", id), zap.String("filename", outputFile))
		return true, err
	}
	l.Info("Successfully retrieved file", zap.String("id", id), zap.String("filename", outputFile))
	return true, nil
}

func fileSize(filename string) (int64, error) {
	l := logger.Get()
	fi, err := os.Stat(filename)
	if err != nil {
		l.Error("Failed to get file stats", zap.String("filename", filename), zap.Error(err))
		return 0, err
	}
	return fi.Size(), nil
}

func retrieveUrls(cfg config.Config, metric *metrics, retrieveAll bool) (allRetrievalsSuccess bool) {
	l := logger.Get()
	allRetrievalsSuccess = true
	if retrieveAll {
		l.Info("Starting retrieving files to backup")
	} else {
		l.Info("Retrying failed retrievals")
	}
	for id, backup := range cfg.Backups {
		if (!retrieveAll) && backup.RetrieveSuccess {
			// no retrieval if not retrieving all and it last retrieval was successful
			continue
		}
		if !retrieveAll {
			l.Info("Retrying...", zap.String("id", backup.ID))
		}
		cfg.Backups[id].RetrieveSuccess = true
		if RetrieveSuccess, err := backupFile(backup.ID, backup.URL, backup.Username, backup.Password, backup.OutputFile); err != nil {
			// already logged in fileSize(); no need to log here
			metric.Status.With(prometheus.Labels{"id": backup.ID}).Set(float64(0))
			metric.BackupFailed.With(prometheus.Labels{"id": backup.ID}).Inc()
			cfg.Backups[id].RetrieveSuccess = RetrieveSuccess
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
	l := logger.Get()

	// Read configuration
	cfg, err := config.New(Version)
	if err != nil {
		os.Exit(1)
	}

	// Create a non-global registry.
	reg := prometheus.NewRegistry()
	// Create new metrics and register them using the custom registry.
	m := NewMetrics(reg, cfg.MetricsPrefix)

	// Create BuildInfo metrics
	initializeCounters(cfg, m)

	// Create tickers for retrievals
	ticker := time.NewTicker(cfg.Interval)
	tickerRetry := time.NewTicker(cfg.RetryInterval)

	// First return
	if retrieveUrls(cfg, m, true) {
		tickerRetry.Stop()
	}

	// Go-routine : do backups
	go func(cfg config.Config, m *metrics) {
		for {
			select {
			case <-ticker.C:
				if retrieveUrls(cfg, m, true) {
					tickerRetry.Stop()
				} else {
					tickerRetry.Reset(cfg.RetryInterval)
				}
			case <-tickerRetry.C:
				if retrieveUrls(cfg, m, false) {
					tickerRetry.Stop()
				}
			}
		}
	}(cfg, m)

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))

	server := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		ReadHeaderTimeout: 3 * time.Second,
	}

	l.Info("Starting exporter HTTP server", zap.Int("port", cfg.Port))
	err = server.ListenAndServe()
	if err != nil {
		l.Fatal("Could not start exporter HTTP server", zap.Error(err))
	}
}
