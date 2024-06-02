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

	os.Args = []string{"./backup_remote_riles", "-c", configurationFilename}

	// from main.go
	cfg, err := config.New("0.0.0")
	assert.Nil(t, err)
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, cfg.MetricsPrefix)

	// Now start the tests
	// Test retrieveUrls
	r := retrieveUrls(cfg, m, true)
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
