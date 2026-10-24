// Copyright 2024-2026 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"backup_remote_files/config"
	"backup_remote_files/testutil"
)

const (
	programName       = "./backup_remote_files"
	defaultOutputFile = "outputFilename"
	defaultPartFile   = "outputFilename.part"
	testOutputFile    = "test.out"
)

func createConfigFile(t *testing.T, key, url string) (configurationFilename, outputFilename string) {
	t.Helper()
	configurationFilename = "config." + key + ".yaml"
	outputFilename = key + ".out"

	configContent := fmt.Sprintf(
		"backups:\n"+
			"  %s:\n"+
			"    url: '%s'\n"+
			"    username: ''\n"+
			"    password: ''\n"+
			"    outputFile: '%s'\n"+
			"\n"+
			"interval: '1m'\n"+
			"retryInterval: '10s'\n"+
			"metricsPrefix: 'backuprf'\n",
		key,
		url,
		outputFilename,
	)
	err := os.WriteFile(configurationFilename, []byte(configContent), 0600)
	require.NoError(t, err)
	return configurationFilename, outputFilename
}

func TestRetrieveUrls(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(t *testing.T) (string, string, func())
		configContent   string
		modifyConfig    func(*config.Config)
		modifyStatus    func(map[string]bool)
		isRetry         bool
		wantSuccess     bool
		wantFileExist   string
		wantFileGone    string
		checkContent    string
		checkOldContent string
	}{
		{
			name: "target is directory",
			setup: func(t *testing.T) (string, string, func()) {
				testutil.UseTempDir(t)
				wantedMsg := "Iune0Shaex"
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprintln(w, wantedMsg)
				}))
				return ts.URL, wantedMsg, func() { ts.Close() }
			},
			modifyConfig: func(cfg *config.Config) { err := os.Mkdir(cfg.Backups[0].OutputFile, 0750); assert.NoError(t, err) },
			isRetry:      true,
			wantSuccess:  true,
			wantFileGone: defaultPartFile,
		},
		{
			name: "simple success",
			setup: func(t *testing.T) (string, string, func()) {
				testutil.UseTempDir(t)
				wantedMsg := "voh0ahch3E"
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprintln(w, wantedMsg)
				}))
				return ts.URL, wantedMsg, func() { ts.Close() }
			},
			isRetry:       true,
			wantSuccess:   true,
			wantFileExist: defaultOutputFile,
			checkContent:  "voh0ahch3E\n",
		},
		{
			name: "retry only failed",
			setup: func(t *testing.T) (string, string, func()) {
				testutil.UseTempDir(t)
				wantedMsg := "Iune0Shaex"
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprintln(w, wantedMsg)
				}))
				return ts.URL, wantedMsg, func() { ts.Close() }
			},
			configContent: `backups:
  a:
    url: '%s'
    username: ''
    password: ''
    outputFile: 'retry_a.out'
  b:
    url: '%s'
    username: ''
    password: ''
    outputFile: 'retry_b.out'
interval: '1m'
retryInterval: '10s'
metricsPrefix: 'backuprf'`,
			modifyStatus:  func(success map[string]bool) { success["a"] = true; success["b"] = false },
			isRetry:       false,
			wantSuccess:   true,
			wantFileGone:  "retry_a.out",
			wantFileExist: "retry_b.out",
			checkContent:  "Iune0Shaex\n",
		},
		{
			name: "broken server preserves old file",
			setup: func(t *testing.T) (string, string, func()) {
				testutil.UseTempDir(t)
				wantedMsg := "aiK8eephiT"
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("Internal Server Error"))
				}))
				return ts.URL, wantedMsg, func() { ts.Close() }
			},
			modifyConfig: func(cfg *config.Config) {
				err := os.WriteFile(cfg.Backups[0].OutputFile, []byte("old"), 0600)
				assert.NoError(t, err)
			},
			isRetry:         true,
			wantSuccess:     false,
			wantFileExist:   defaultOutputFile,
			checkOldContent: "old",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, wantedMsg, cleanup := tt.setup(t)
			defer cleanup()

			var configFilename, outputFilename string
			if tt.configContent != "" {
				configFilename = "test_config.yaml"
				outputFilename = "retry_b.out"
				err := os.WriteFile(configFilename, []byte(fmt.Sprintf(tt.configContent, url, url)), 0600)
				require.NoError(t, err)
			} else {
				configFilename, outputFilename = createConfigFile(t, wantedMsg, url)
			}

			cfgVal, err := config.New(configFilename, 9289)
			require.NoError(t, err)
			cfg := &cfgVal
			if tt.modifyConfig != nil {
				tt.modifyConfig(cfg)
			}

			reg := prometheus.NewRegistry()
			m := NewMetrics(reg, cfg.MetricsPrefix)

			status := newBackupStatus(cfg.Backups)
			if tt.modifyStatus != nil {
				tt.modifyStatus(status.success)
			}

			r := retrieveUrls(t.Context(), *cfg, m, status, tt.isRetry)
			assert.Equal(t, tt.wantSuccess, r)

			if tt.wantFileGone != "" {
				gone := tt.wantFileGone
				if gone == defaultPartFile {
					gone = outputFilename + ".part"
				}
				_, err := os.Stat(gone)
				assert.True(t, os.IsNotExist(err), "%s should not exist", gone)
			}

			if tt.wantFileExist != "" {
				exist := tt.wantFileExist
				if exist == defaultOutputFile {
					exist = outputFilename
				}
				if assert.FileExists(t, exist) {
					content, err := os.ReadFile(exist)
					require.NoError(t, err)
					if tt.checkContent != "" {
						assert.Equal(t, tt.checkContent, string(content))
					}
					if tt.checkOldContent != "" {
						assert.Equal(t, tt.checkOldContent, string(content))
					}
				}
			}
		})
	}
}

func TestInitializeCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, "test")
	initializeCounters(m)

	families, err := reg.Gather()
	assert.NoError(t, err)
	assert.NotEmpty(t, families)
}

func TestBackupFile(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(t *testing.T) (string, func())
		url          string
		outputFile   string
		timeout      time.Duration
		wantErr      bool
		checkErr     func(t *testing.T, err error)
		checkContent string
	}{
		{
			name: "invalid URL",
			setup: func(_ *testing.T) (string, func()) {
				return "", func() {}
			},
			url:        "://invalid",
			outputFile: testOutputFile,
			timeout:    10 * time.Second,
			wantErr:    true,
			checkErr: func(t *testing.T, err error) {
				var target *httpError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name: "HTTP do error (connection refused)",
			setup: func(_ *testing.T) (string, func()) {
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprintln(w, "test")
				}))
				url := ts.URL
				ts.Close()
				return url, func() {}
			},
			url:        "",
			outputFile: testOutputFile,
			timeout:    10 * time.Second,
			wantErr:    true,
			checkErr: func(t *testing.T, err error) {
				var target *httpError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name: "create file error (nonexistent directory)",
			setup: func(t *testing.T) (string, func()) {
				testutil.UseTempDir(t)
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprintln(w, "test")
				}))
				return ts.URL, func() { ts.Close() }
			},
			url:        "",
			outputFile: "nonexistent_dir/file.out",
			timeout:    10 * time.Second,
			wantErr:    true,
			checkErr: func(t *testing.T, err error) {
				var target *fsError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name: "copy error (write to /dev/full)",
			setup: func(t *testing.T) (string, func()) {
				testutil.UseTempDir(t)
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprintln(w, "test")
				}))
				err := os.Symlink("/dev/full", "test.out.part")
				require.NoError(t, err)
				return ts.URL, func() { ts.Close() }
			},
			url:        "",
			outputFile: testOutputFile,
			timeout:    10 * time.Second,
			wantErr:    true,
			checkErr: func(t *testing.T, err error) {
				var target *fsError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name: "timeout exceeded",
			setup: func(t *testing.T) (string, func()) {
				testutil.UseTempDir(t)
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					time.Sleep(500 * time.Millisecond)
					fmt.Fprintln(w, "test")
				}))
				return ts.URL, func() { ts.Close() }
			},
			url:        "",
			outputFile: testOutputFile,
			timeout:    100 * time.Millisecond,
			wantErr:    true,
			checkErr: func(t *testing.T, err error) {
				var target *httpError
				assert.ErrorAs(t, err, &target)
			},
		},
		{
			name: "successful backup",
			setup: func(t *testing.T) (string, func()) {
				testutil.UseTempDir(t)
				testContent := "Hello, World!"
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprint(w, testContent)
				}))
				return ts.URL, func() { ts.Close() }
			},
			url:          "",
			outputFile:   testOutputFile,
			timeout:      10 * time.Second,
			wantErr:      false,
			checkContent: "Hello, World!",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, cleanup := tt.setup(t)
			defer cleanup()
			if tt.url != "" {
				url = tt.url
			}

			size, err := backupFile(t.Context(), "test", url, "", "", tt.outputFile, tt.timeout)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.checkErr != nil {
					tt.checkErr(t, err)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, int64(len(tt.checkContent)), size)
				content, err := os.ReadFile(tt.outputFile)
				assert.NoError(t, err)
				assert.Equal(t, tt.checkContent, string(content))
			}
		})
	}
}

func TestHTTPErrors(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "httpError unwrap",
			run: func(t *testing.T) {
				inner := errors.New("inner")
				err := &httpError{error: inner}
				assert.Equal(t, inner, errors.Unwrap(err))
			},
		},
		{
			name: "fsError unwrap",
			run: func(t *testing.T) {
				inner := errors.New("inner")
				err := &fsError{error: inner}
				assert.Equal(t, inner, errors.Unwrap(err))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t)
		})
	}
}

func TestRecordBackupFailed(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, "test")
	initializeCounters(m)

	recordBackupFailed(m, "test_id")

	families, err := reg.Gather()
	assert.NoError(t, err)

	failedFamily := findMetric(families, "test_backup_failed")
	require.NotNil(t, failedFamily)
	metric := findMetricWithID(failedFamily, "test_id")
	require.NotNil(t, metric)
	assert.Equal(t, float64(1), metric.GetCounter().GetValue())

	statusFamily := findMetric(families, "test_backup_status")
	require.NotNil(t, statusFamily)
	metric = findMetricWithID(statusFamily, "test_id")
	require.NotNil(t, metric)
	assert.Equal(t, float64(0), metric.GetGauge().GetValue())
}

func findMetric(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, f := range families {
		if f.GetName() == name {
			return f
		}
	}
	return nil
}

func findMetricWithID(family *dto.MetricFamily, id string) *dto.Metric {
	for _, m := range family.GetMetric() {
		for _, l := range m.GetLabel() {
			if l.GetName() == "id" && l.GetValue() == id {
				return m
			}
		}
	}
	return nil
}

func TestMetricsValues(t *testing.T) {
	testutil.UseTempDir(t)
	wantedMsg := "metrics_test"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, wantedMsg)
	}))
	defer ts.Close()

	configurationFilename, outputFilename := createConfigFile(t, wantedMsg, ts.URL)

	cfg, err := config.New(configurationFilename, 9289)
	assert.NoError(t, err)

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)
	status := newBackupStatus(cfg.Backups)

	r := retrieveUrls(t.Context(), cfg, m, status, true)
	assert.True(t, r)
	defer os.Remove(outputFilename)

	families, err := reg.Gather()
	assert.NoError(t, err)

	statusFamily := findMetric(families, "backuprf_backup_status")
	require.NotNil(t, statusFamily)
	metric := findMetricWithID(statusFamily, wantedMsg)
	require.NotNil(t, metric)
	assert.Equal(t, float64(1), metric.GetGauge().GetValue())

	sizeFamily := findMetric(families, "backuprf_backup_size")
	require.NotNil(t, sizeFamily)
	metric = findMetricWithID(sizeFamily, wantedMsg)
	require.NotNil(t, metric)
	assert.Greater(t, metric.GetGauge().GetValue(), float64(0))

	totalFamily := findMetric(families, "backuprf_backup_nb")
	require.NotNil(t, totalFamily)
	assert.Equal(t, float64(1), totalFamily.GetMetric()[0].GetCounter().GetValue())
}

func TestMetricsValues_BrokenServer(t *testing.T) {
	testutil.UseTempDir(t)
	wantedMsg := "metrics_broken"
	oldMsg := "old"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte("Internal Server Error"))
		assert.NoError(t, err)
	}))
	defer ts.Close()

	configurationFilename, outputFilename := createConfigFile(t, wantedMsg, ts.URL)

	err := os.WriteFile(outputFilename, []byte(oldMsg), 0600)
	assert.NoError(t, err)

	cfg, err := config.New(configurationFilename, 9289)
	assert.NoError(t, err)

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)
	status := newBackupStatus(cfg.Backups)

	_ = retrieveUrls(t.Context(), cfg, m, status, true)
	defer os.Remove(outputFilename)

	families, err := reg.Gather()
	assert.NoError(t, err)

	statusFamily := findMetric(families, "backuprf_backup_status")
	require.NotNil(t, statusFamily)
	metric := findMetricWithID(statusFamily, wantedMsg)
	require.NotNil(t, metric)
	assert.Equal(t, float64(0), metric.GetGauge().GetValue())

	failedFamily := findMetric(families, "backuprf_backup_failed")
	require.NotNil(t, failedFamily)
	metric = findMetricWithID(failedFamily, wantedMsg)
	require.NotNil(t, metric)
	assert.Equal(t, float64(1), metric.GetCounter().GetValue())

	totalFamily := findMetric(families, "backuprf_backup_nb")
	require.NotNil(t, totalFamily)
	assert.Equal(t, float64(1), totalFamily.GetMetric()[0].GetCounter().GetValue())
}

func TestMetricsValues_RenameBlockedByDirectory(t *testing.T) {
	testutil.UseTempDir(t)
	wantedMsg := "metrics_fserr"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, wantedMsg)
	}))
	defer ts.Close()

	configurationFilename, _ := createConfigFile(t, wantedMsg, ts.URL)

	cfg, err := config.New(configurationFilename, 9289)
	assert.NoError(t, err)

	// Override the output path to a directory so rename fails
	cfg.Backups[0].OutputFile = "."

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)
	status := newBackupStatus(cfg.Backups)

	_ = retrieveUrls(t.Context(), cfg, m, status, true)

	families, err := reg.Gather()
	assert.NoError(t, err)

	statusFamily := findMetric(families, "backuprf_backup_status")
	require.NotNil(t, statusFamily)
	metric := findMetricWithID(statusFamily, wantedMsg)
	require.NotNil(t, metric)
	assert.Equal(t, float64(0), metric.GetGauge().GetValue())
}

func getFreePort(t *testing.T) int {
	t.Helper()
	lc := net.ListenConfig{}
	l, err := lc.Listen(t.Context(), "tcp", ":0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestRun_ServesMetrics(t *testing.T) {
	testutil.UseTempDir(t)
	wantedMsg := "run_metrics"
	port := getFreePort(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, wantedMsg)
	}))
	defer ts.Close()

	configContent := fmt.Sprintf(
		"backups:\n"+
			"  %s:\n"+
			"    url: '%s'\n"+
			"    username: ''\n"+
			"    password: ''\n"+
			"    outputFile: '%s'\n"+
			"\n"+
			"interval: '1m'\n"+
			"retryInterval: '10s'\n"+
			"metricsPrefix: 'run_test'\n",
		wantedMsg, ts.URL, wantedMsg+".out",
	)
	configFile := "run_config.yaml"
	err := os.WriteFile(configFile, []byte(configContent), 0600)
	require.NoError(t, err)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{programName, "-c", configFile, "-p", strconv.Itoa(port)} //nolint:goconst

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx)
	}()

	var resp *http.Response
	client := &http.Client{}
	for range 20 {
		req, reqErr := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			fmt.Sprintf("http://localhost:%d/metrics", port),
			http.NoBody,
		)
		if reqErr != nil {
			err = reqErr
			time.Sleep(50 * time.Millisecond)
			continue
		}
		resp, err = client.Do(req)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), "run_test_backup_status")

	cancel()
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.Fail(t, "run() did not return after cancellation")
	}
}

func TestRun_ParseFlagsError(t *testing.T) {
	testutil.UseTempDir(t)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{programName, "--unknown-flag"}

	err := run(t.Context())
	assert.Error(t, err)
}

func TestRun_ConfigError(t *testing.T) {
	testutil.UseTempDir(t)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{programName, "-c", "nonexistent.yaml"}

	err := run(t.Context())
	assert.Error(t, err)
}

func TestStartMetricsServer_PortConflict(t *testing.T) {
	port := getFreePort(t)

	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", ":"+strconv.Itoa(port))
	require.NoError(t, err)
	defer l.Close()

	reg := prometheus.NewRegistry()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- startMetricsServer(ctx, reg, port)
	}()

	select {
	case err := <-errCh:
		assert.Error(t, err)
	case <-time.After(2 * time.Second):
		require.Fail(t, "startMetricsServer did not return an error")
	}
}

func TestRunBackupLoop_RetryReset(t *testing.T) {
	testutil.UseTempDir(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	cfg := config.Config{
		Port:          9999,
		Backups:       []config.Backup{{ID: "fail", URL: ts.URL, OutputFile: "fail.out"}},
		Interval:      10 * time.Millisecond,
		RetryInterval: 1 * time.Hour,
		MetricsPrefix: "reset_test",
	}
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)
	status := newBackupStatus(cfg.Backups)

	ticker := time.NewTicker(10 * time.Millisecond)
	tickerRetry := time.NewTicker(1 * time.Hour)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	runBackupLoop(ctx, cfg, m, status, ticker, tickerRetry)

	families, err := reg.Gather()
	require.NoError(t, err)
	failedFamily := findMetric(families, "reset_test_backup_failed")
	require.NotNil(t, failedFamily)
	assert.GreaterOrEqual(t, failedFamily.GetMetric()[0].GetCounter().GetValue(), float64(1))
}

func TestRunBackupLoop_TickerFires(t *testing.T) {
	tests := []struct {
		name          string
		interval      time.Duration
		retryInterval time.Duration
		prefix        string
	}{
		{
			name:          "regular ticker fires",
			interval:      10 * time.Millisecond,
			retryInterval: 1 * time.Hour,
			prefix:        "ticker_test",
		},
		{
			name:          "retry ticker fires",
			interval:      1 * time.Hour,
			retryInterval: 10 * time.Millisecond,
			prefix:        "retry_ticker_test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Port:          9999,
				Backups:       []config.Backup{},
				Interval:      tt.interval,
				RetryInterval: tt.retryInterval,
				MetricsPrefix: tt.prefix,
			}
			reg := prometheus.NewRegistry()
			m := NewMetrics(reg, cfg.MetricsPrefix)
			status := newBackupStatus(cfg.Backups)

			ticker := time.NewTicker(tt.interval)
			tickerRetry := time.NewTicker(tt.retryInterval)

			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()

			runBackupLoop(ctx, cfg, m, status, ticker, tickerRetry)

			families, err := reg.Gather()
			require.NoError(t, err)
			totalFamily := findMetric(families, tt.prefix+"_backup_nb")
			require.NotNil(t, totalFamily)
			assert.GreaterOrEqual(t, totalFamily.GetMetric()[0].GetCounter().GetValue(), float64(1))
		})
	}
}

func TestRunBackupLoop_Retry(t *testing.T) {
	testutil.UseTempDir(t)
	port := getFreePort(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	configContent := fmt.Sprintf(
		"backups:\n"+
			"  retry_test:\n"+
			"    url: '%s'\n"+
			"    username: ''\n"+
			"    password: ''\n"+
			"    outputFile: 'retry_test.out'\n"+
			"\n"+
			"interval: '1h'\n"+
			"retryInterval: '50ms'\n"+
			"metricsPrefix: 'retry_test'\n",
		ts.URL,
	)
	configFile := "retry_loop_config.yaml"
	err := os.WriteFile(configFile, []byte(configContent), 0600)
	require.NoError(t, err)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{programName, "-c", configFile, "-p", strconv.Itoa(port)}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx)
	}()

	client := &http.Client{}
	var body []byte
	for range 80 {
		req, reqErr := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			fmt.Sprintf("http://localhost:%d/metrics", port),
			http.NoBody,
		)
		if reqErr != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		resp, getErr := client.Do(req)
		if getErr != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		if strings.Contains(string(body), "retry_test_backup_nb 2") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.Contains(t, string(body), "retry_test_backup_nb 2")

	cancel()
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.Fail(t, "run() did not return after cancellation")
	}
}

// roundTripFunc adapts a function to an http.RoundTripper for tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// failCloseBody is an io.ReadCloser whose Close always returns an error,
// used to exercise the body-close error path in backupFile.
type failCloseBody struct{}

func (failCloseBody) Read(_ []byte) (int, error) { return 0, io.EOF }

func (failCloseBody) Close() error { return errors.New("close failed") }

func TestBackupFile_BodyCloseError(t *testing.T) {
	testutil.UseTempDir(t)

	origNewClient := newClient
	newClient = func(time.Duration) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: failCloseBody{}}, nil
			}),
		}
	}
	t.Cleanup(func() { newClient = origNewClient })

	_, err := backupFile(t.Context(), "test", "http://example.com", "", "", testOutputFile, 10*time.Second)
	assert.NoError(t, err)
	assert.FileExists(t, testOutputFile)
}
