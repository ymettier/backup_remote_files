// Copyright 2023 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package main

import (
	"backup_remote_files/config"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func createConfigFile(key, url string) (configurationFilename, outputFilename string, err error) {
	configurationFilename = "config." + key + ".yaml"
	outputFilename = key + ".out"

	// Remove the configuration file if it already exists
	if _, err := os.Stat(configurationFilename); !errors.Is(err, os.ErrNotExist) {
		os.Remove(configurationFilename)
	}

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
	wantedMsg := "Iune0Shaex"

	// Create a web server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, wantedMsg)
	}))
	defer ts.Close()

	// Generate the configuration file
	configurationFilename, outputFilename, err := createConfigFile(wantedMsg, ts.URL)
	assert.Nil(t, err)
	defer os.Remove(configurationFilename)

	// Remove the output file if it already exists
	if _, err := os.Stat(outputFilename); !errors.Is(err, os.ErrNotExist) {
		os.Remove(outputFilename)
	}
	// Create a directory with the same name as the output file to generate an error
	err = os.Mkdir(outputFilename, 0750)
	assert.Nil(t, err)
	defer os.RemoveAll(outputFilename)

	// Change args to be able to run like main()
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }() // os.Args is a "global variable", so keep the state from before the test, and restore it after.

	os.Args = []string{"./backup_remote_files", "-c", configurationFilename} //nolint:goconst // args is better explicit here

	// from main.go
	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)

	// Now start the tests
	// Test retrieveUrls
	status := newBackupStatus(cfg.Backups)
	r := retrieveUrls(cfg, m, status, true)
	if !assert.FileExists(t, outputFilename+".part") {
		return
	}
	defer os.Remove(outputFilename + ".part")

	assert.True(t, r)
}

func TestRetrieveUrlsSimple(t *testing.T) {
	wantedMsg := "voh0ahch3E"

	// Create a web server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, wantedMsg)
	}))
	defer ts.Close()

	// Generate the configuration file
	configurationFilename, outputFilename, err := createConfigFile(wantedMsg, ts.URL)
	assert.Nil(t, err)
	defer os.Remove(configurationFilename)

	// Remove the output file if it already exists
	if _, err := os.Stat(outputFilename); !errors.Is(err, os.ErrNotExist) {
		os.Remove(outputFilename)
	}

	// Change args to be able to run like main()
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }() // os.Args is a "global variable", so keep the state from before the test, and restore it after.

	os.Args = []string{"./backup_remote_files", "-c", configurationFilename}

	// from main.go
	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)

	// Now start the tests
	// Test retrieveUrls
	status := newBackupStatus(cfg.Backups)
	r := retrieveUrls(cfg, m, status, true)
	if !assert.FileExists(t, outputFilename) {
		return
	}
	defer os.Remove(outputFilename)

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
	wantedMsg := "Iune0Shaex"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, wantedMsg)
	}))
	defer ts.Close()

	// Create a config file with two backups
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
	defer os.Remove(configFilename)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"./backup_remote_files", "-c", configFilename}

	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)

	// Create tracker with "a" successful and "b" failed
	status := newBackupStatus(cfg.Backups)
	status.success["a"] = true
	status.success["b"] = false

	// Call retrieveAll=false — should only retry "b"
	r := retrieveUrls(cfg, m, status, false)

	defer os.Remove("retry_a.out")
	defer os.Remove("retry_b.out")

	assert.True(t, r)

	// "a" was already successful, should NOT have been retried
	_, err = os.Stat("retry_a.out")
	assert.True(t, os.IsNotExist(err), "successful backup should not be retried")

	// "b" was failed, should have been retried and succeeded
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
	wantedMsg := "aiK8eephiT"
	oldMsg := "old"

	// Create a web server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte("Internal Server Error"))
		assert.Nil(t, err)
	}))
	defer ts.Close()

	// Generate the configuration file
	configurationFilename, outputFilename, err := createConfigFile(wantedMsg, ts.URL)
	assert.Nil(t, err)
	defer os.Remove(configurationFilename)

	// Create a file with "old" contents
	err = os.WriteFile(outputFilename, []byte(oldMsg), 0600)
	assert.Nil(t, err)

	// Change args to be able to run like main()
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }() // os.Args is a "global variable", so keep the state from before the test, and restore it after.

	os.Args = []string{"./backup_remote_files", "-c", configurationFilename}

	// from main.go
	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)

	// Now start the tests
	// Test retrieveUrls
	status := newBackupStatus(cfg.Backups)
	r := retrieveUrls(cfg, m, status, true)
	if !assert.FileExists(t, outputFilename) {
		return
	}
	defer os.Remove(outputFilename)

	assert.True(t, r)

	outputFile, err := os.Open(outputFilename)
	if !assert.NoError(t, err) {
		return
	}
	defer outputFile.Close()

	byteValue, _ := io.ReadAll(outputFile)
	assert.Equal(t, string(byteValue), oldMsg)
}
