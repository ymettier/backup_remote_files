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

const programName = "./backup_remote_files"

func createConfigFile(key, url string) (configurationFilename, outputFilename string, err error) {
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
	err = os.WriteFile(configurationFilename, []byte(configContent), 0600)
	if err != nil {
		return "", "", err
	}
	return configurationFilename, outputFilename, nil
}

func TestRetrieveUrls_TargetIsDirectory(t *testing.T) {
	testutil.UseTempDir(t)
	wantedMsg := "Iune0Shaex"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, wantedMsg)
	}))
	defer ts.Close()

	configurationFilename, outputFilename, err := createConfigFile(wantedMsg, ts.URL)
	assert.NoError(t, err)

	err = os.Mkdir(outputFilename, 0750)
	assert.NoError(t, err)

	cfg, err := config.New(configurationFilename, 9289)
	assert.NoError(t, err)
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)

	status := newBackupStatus(cfg.Backups)
	r := retrieveUrls(context.Background(), cfg, m, status, true)
	if !assert.FileExists(t, outputFilename+".part") {
		return
	}

	assert.True(t, r)
}

func TestRetrieveUrlsSimple(t *testing.T) {
	testutil.UseTempDir(t)
	wantedMsg := "voh0ahch3E"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, wantedMsg)
	}))
	defer ts.Close()

	configurationFilename, outputFilename, err := createConfigFile(wantedMsg, ts.URL)
	assert.NoError(t, err)

	cfg, err := config.New(configurationFilename, 9289)
	assert.NoError(t, err)
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)

	status := newBackupStatus(cfg.Backups)
	r := retrieveUrls(context.Background(), cfg, m, status, true)
	if !assert.FileExists(t, outputFilename) {
		return
	}

	assert.True(t, r)

	outputFile, err := os.Open(outputFilename)
	if !assert.NoError(t, err) {
		return
	}
	defer outputFile.Close()

	byteValue, _ := io.ReadAll(outputFile)
	assert.Equal(t, string(byteValue), wantedMsg+"\n")
}

func TestRetrieveUrlsRetry(t *testing.T) {
	testutil.UseTempDir(t)
	wantedMsg := "Iune0Shaex"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, wantedMsg)
	}))
	defer ts.Close()

	configContent := fmt.Sprintf(
		"backups:\n"+
			"  a:\n"+
			"    url: '%s'\n"+
			"    username: ''\n"+
			"    password: ''\n"+
			"    outputFile: 'retry_a.out'\n"+
			"  b:\n"+
			"    url: '%s'\n"+
			"    username: ''\n"+
			"    password: ''\n"+
			"    outputFile: 'retry_b.out'\n"+
			"interval: '1m'\n"+
			"retryInterval: '10s'\n"+
			"metricsPrefix: 'backuprf'\n",
		ts.URL, ts.URL)

	configFilename := "retry_config.yaml"
	err := os.WriteFile(configFilename, []byte(configContent), 0600)
	assert.NoError(t, err)

	cfg, err := config.New(configFilename, 9289)
	assert.NoError(t, err)
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)

	status := newBackupStatus(cfg.Backups)
	status.success["a"] = true
	status.success["b"] = false

	r := retrieveUrls(context.Background(), cfg, m, status, false)

	assert.True(t, r)

	_, err = os.Stat("retry_a.out")
	assert.True(t, os.IsNotExist(err), "successful backup should not be retried")

	if !assert.FileExists(t, "retry_b.out") {
		return
	}
	outputFile, err := os.Open("retry_b.out")
	assert.NoError(t, err)
	defer outputFile.Close()
	byteValue, _ := io.ReadAll(outputFile)
	assert.Equal(t, string(byteValue), wantedMsg+"\n")
}

func TestRetrieveUrlsBroken(t *testing.T) {
	testutil.UseTempDir(t)
	wantedMsg := "aiK8eephiT"
	oldMsg := "old"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte("Internal Server Error"))
		assert.NoError(t, err)
	}))
	defer ts.Close()

	configurationFilename, outputFilename, err := createConfigFile(wantedMsg, ts.URL)
	assert.NoError(t, err)

	err = os.WriteFile(outputFilename, []byte(oldMsg), 0600)
	assert.NoError(t, err)

	cfg, err := config.New(configurationFilename, 9289)
	assert.NoError(t, err)
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)

	status := newBackupStatus(cfg.Backups)
	r := retrieveUrls(context.Background(), cfg, m, status, true)
	assert.FileExists(t, outputFilename)

	assert.False(t, r)

	outputFile, err := os.Open(outputFilename)
	if !assert.NoError(t, err) {
		return
	}
	defer outputFile.Close()

	byteValue, _ := io.ReadAll(outputFile)
	assert.Equal(t, string(byteValue), oldMsg)
}

func TestInitializeCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, "test")
	initializeCounters(m)

	families, err := reg.Gather()
	assert.NoError(t, err)
	assert.NotEmpty(t, families)
}

func TestBackupFile_InvalidURL(t *testing.T) {
	_, err := backupFile(context.Background(), "test", "://invalid", "", "", "test.out")
	assert.Error(t, err)

	var target *httpError
	assert.True(t, errors.As(err, &target))
}

func TestBackupFile_HTTPDoError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "test")
	}))
	ts.Close()

	_, err := backupFile(context.Background(), "test", ts.URL, "", "", "test.out")

	assert.Error(t, err)
	var target *httpError
	assert.True(t, errors.As(err, &target))
}

func TestBackupFile_CreateFileError(t *testing.T) {
	testutil.UseTempDir(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "test")
	}))
	defer ts.Close()

	_, err := backupFile(context.Background(), "test", ts.URL, "", "", "nonexistent_dir/file.out")

	assert.Error(t, err)
	var target *fsError
	assert.True(t, errors.As(err, &target))
}

func TestBackupFile_CopyError(t *testing.T) {
	testutil.UseTempDir(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "test")
	}))
	defer ts.Close()

	err := os.Symlink("/dev/full", "test.out.part")
	assert.NoError(t, err)

	_, err = backupFile(context.Background(), "test", ts.URL, "", "", "test.out")

	assert.Error(t, err)
	var target *fsError
	assert.True(t, errors.As(err, &target))
}

func TestHTTPError_Unwrap(t *testing.T) {
	inner := errors.New("inner")
	err := &httpError{inner}
	assert.Equal(t, inner, errors.Unwrap(err))
}

func TestFSError_Unwrap(t *testing.T) {
	inner := errors.New("inner")
	err := &fsError{inner}
	assert.Equal(t, inner, errors.Unwrap(err))
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
			if l.GetName() == "id" && l.GetValue() == id { //nolint:goconst
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

	configurationFilename, outputFilename, err := createConfigFile(wantedMsg, ts.URL)
	assert.NoError(t, err)

	cfg, err := config.New(configurationFilename, 9289)
	assert.NoError(t, err)

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)
	status := newBackupStatus(cfg.Backups)

	r := retrieveUrls(context.Background(), cfg, m, status, true)
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

	configurationFilename, outputFilename, err := createConfigFile(wantedMsg, ts.URL)
	assert.NoError(t, err)

	err = os.WriteFile(outputFilename, []byte(oldMsg), 0600)
	assert.NoError(t, err)

	cfg, err := config.New(configurationFilename, 9289)
	assert.NoError(t, err)

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)
	status := newBackupStatus(cfg.Backups)

	_ = retrieveUrls(context.Background(), cfg, m, status, true)
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

	configurationFilename, _, err := createConfigFile(wantedMsg, ts.URL)
	assert.NoError(t, err)

	cfg, err := config.New(configurationFilename, 9289)
	assert.NoError(t, err)

	// Override the output path to a directory so rename fails
	cfg.Backups[0].OutputFile = "."

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)
	status := newBackupStatus(cfg.Backups)

	_ = retrieveUrls(context.Background(), cfg, m, status, true)

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
	l, err := lc.Listen(context.Background(), "tcp", ":0")
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx)
	}()

	var resp *http.Response
	client := &http.Client{}
	for range 20 {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://localhost:%d/metrics", port), http.NoBody)
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
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return after cancellation")
	}
}

func TestRun_ParseFlagsError(t *testing.T) {
	testutil.UseTempDir(t)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{programName, "--unknown-flag"}

	err := run(context.Background())
	assert.Error(t, err)
}

func TestRun_ConfigError(t *testing.T) {
	testutil.UseTempDir(t)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{programName, "-c", "nonexistent.yaml"}

	err := run(context.Background())
	assert.Error(t, err)
}

func TestStartMetricsServer_PortConflict(t *testing.T) {
	port := getFreePort(t)

	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", ":"+strconv.Itoa(port))
	require.NoError(t, err)
	defer l.Close()

	reg := prometheus.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- startMetricsServer(ctx, reg, port)
	}()

	select {
	case err := <-errCh:
		assert.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("startMetricsServer did not return an error")
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

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
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

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx)
	}()

	client := &http.Client{}
	var body []byte
	for range 80 {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://localhost:%d/metrics", port), http.NoBody)
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
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return after cancellation")
	}
}
