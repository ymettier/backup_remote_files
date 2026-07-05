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
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"backup_remote_files/config"
	"backup_remote_files/testutil"
)

func createConfigFile(key, url string) (configurationFilename, outputFilename string, err error) {
	configurationFilename = "config." + key + ".yaml"
	outputFilename = key + ".out"

	f, err := os.Create(configurationFilename)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	fmt.Fprintf(f, ""+
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
	return configurationFilename, outputFilename, nil
}

func TestRetrieveUrlsWithTargetDirCollision(t *testing.T) {
	testutil.UseTempDir(t)
	wantedMsg := "Iune0Shaex"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, wantedMsg)
	}))
	defer ts.Close()

	configurationFilename, outputFilename, err := createConfigFile(wantedMsg, ts.URL)
	assert.Nil(t, err)

	err = os.Mkdir(outputFilename, 0750)
	assert.Nil(t, err)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"./backup_remote_files", "-c", configurationFilename} //nolint:goconst

	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)
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
	assert.Nil(t, err)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"./backup_remote_files", "-c", configurationFilename}

	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)
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

	configContent := fmt.Sprintf(""+
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
	assert.Nil(t, err)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"./backup_remote_files", "-c", configFilename}

	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)
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
	assert.Nil(t, err)
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
		assert.Nil(t, err)
	}))
	defer ts.Close()

	configurationFilename, outputFilename, err := createConfigFile(wantedMsg, ts.URL)
	assert.Nil(t, err)

	err = os.WriteFile(outputFilename, []byte(oldMsg), 0600)
	assert.Nil(t, err)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"./backup_remote_files", "-c", configurationFilename}

	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)
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

func TestFileSize_NotFound(t *testing.T) {
	_, err := fileSize("nonexistent_file")
	assert.Error(t, err)
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
	err := backupFile(context.Background(), "test", "://invalid", "", "", "test.out")
	assert.Error(t, err)

	var target *httpError
	assert.True(t, errors.As(err, &target))
}

func TestBackupFile_HTTPDoError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "test")
	}))
	ts.Close()

	err := backupFile(context.Background(), "test", ts.URL, "", "", "test.out")

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

	err := backupFile(context.Background(), "test", ts.URL, "", "", "nonexistent_dir/file.out")

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

	err = backupFile(context.Background(), "test", ts.URL, "", "", "test.out")

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
	assert.Nil(t, err)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"./backup_remote_files", "-c", configurationFilename}

	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)

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
		assert.Nil(t, err)
	}))
	defer ts.Close()

	configurationFilename, outputFilename, err := createConfigFile(wantedMsg, ts.URL)
	assert.Nil(t, err)

	err = os.WriteFile(outputFilename, []byte(oldMsg), 0600)
	assert.Nil(t, err)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"./backup_remote_files", "-c", configurationFilename}

	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)

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

func TestMetricsValues_FileSizeError(t *testing.T) {
	testutil.UseTempDir(t)
	wantedMsg := "metrics_fserr"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, wantedMsg)
	}))
	defer ts.Close()

	configurationFilename, _, err := createConfigFile(wantedMsg, ts.URL)
	assert.Nil(t, err)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"./backup_remote_files", "-c", configurationFilename}

	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)

	// Override the output path to a directory so fileSize fails
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
	os.Args = []string{"./backup_remote_files", "-c", configFile, "-p", strconv.Itoa(port)}

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

func TestRun_ConfigError(t *testing.T) {
	testutil.UseTempDir(t)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"./backup_remote_files", "-c", "nonexistent.yaml"}

	err := run(context.Background())
	assert.Error(t, err)
}
