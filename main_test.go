// Copyright 2023 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"backup_remote_files/config"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func useTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("failed to restore working directory: %v", err)
		}
	})
}

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
	useTempDir(t)
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

	os.Args = []string{"./backup_remote_files", "-c", configurationFilename}

	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)

	status := newBackupStatus(cfg.Backups)
	r := retrieveUrls(cfg, m, status, true)
	if !assert.FileExists(t, outputFilename+".part") {
		return
	}

	assert.True(t, r)
}

func TestRetrieveUrlsSimple(t *testing.T) {
	useTempDir(t)
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
	r := retrieveUrls(cfg, m, status, true)
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
	useTempDir(t)
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

	r := retrieveUrls(cfg, m, status, false)

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
	useTempDir(t)
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
	r := retrieveUrls(cfg, m, status, true)
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
	assert.Equal(t, string(byteValue), oldMsg)
}
