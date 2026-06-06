// Copyright 2024 The Backup_icf_cvf Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"backup_remote_files/logger"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/alecthomas/kong"
	"log/slog"
	"gopkg.in/yaml.v3"
)

type VersionFlag bool

func printVersion(version string) string {
	output := fmt.Sprintf("%-15s: %s\n", "Version", version)

	// Get and print additionnal build info
	var lastCommit time.Time
	revision := "unknown"
	dirtyBuild := true

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return output
	}

	for _, kv := range info.Settings {
		if kv.Value == "" {
			continue
		}
		switch kv.Key {
		case "vcs.revision":
			revision = kv.Value
		case "vcs.time":
			lastCommit, _ = time.Parse(time.RFC3339, kv.Value)
		case "vcs.modified":
			dirtyBuild = kv.Value == "true"
		}
	}

	output += fmt.Sprintf("%-15s: %s\n", "Revision", revision)
	output += fmt.Sprintf("%-15s: %v\n", "Dirty Build", dirtyBuild)
	output += fmt.Sprintf("%-15s: %s\n", "Last Commit", lastCommit)
	output += fmt.Sprintf("%-15s: %s\n", "Go Version", info.GoVersion)
	return output
}

func (v VersionFlag) BeforeReset(version string) error {
	output := printVersion(version)
	fmt.Printf("%s", output)
	os.Exit(0)
	return nil
}

type CLI struct {
	ConfigFile string      `name:"config" short:"c" required:"" help:"Configuration file"`
	Port       int         `name:"port" short:"p" default:"9289" optional:"" help:"Exporter port"`
	Version    VersionFlag `name:"version" short:"V"  help:"Show version info"`
}

type Backup struct {
	ID              string
	URL             string
	Username        string
	Password        string
	OutputFile      string
	RetrieveSuccess bool
}

type Config struct {
	Port          int
	Backups       []Backup
	Interval      time.Duration
	RetryInterval time.Duration
	MetricsPrefix string
}

func New(version string) (Config, error) {
	var cli CLI
	kong.Parse(&cli, kong.Bind(version))
	var cfg Config
	cfg.Port = cli.Port

	err := cfg.readConfig(cli.ConfigFile)
	if err != nil {
		return cfg, err
	}

	return cfg, nil
}

func (c *Config) readConfig(filename string) error {
	var data map[string]any
	l := logger.Get()
	yamlFile, err := os.ReadFile(filename)
	if err != nil {
		l.Error("Failed to read configuration file", slog.String("file", filename), slog.Any("error", err)); os.Exit(1)
		return err
	}

	err = yaml.Unmarshal(yamlFile, &data)

	// Logging configuration
	if logging, ok := data["logging"].(map[string]any); ok {
		var logOpts logger.LogOptions
		if level, ok := logging["level"].(string); ok {
			logOpts.Level = level
		}
		if filename, ok := logging["filename"].(string); ok {
			logOpts.Filename = filename
		}
		if maxSize, ok := logging["maxSize"].(int); ok {
			logOpts.MaxSize = maxSize
		}
		if maxBackups, ok := logging["maxBackups"].(int); ok {
			logOpts.MaxBackups = maxBackups
		}
		if maxAge, ok := logging["maxAge"].(int); ok {
			logOpts.MaxAge = maxAge
		}
		if compress, ok := logging["compress"].(bool); ok {
			logOpts.Compress = compress
		}
		logger.Reset(&logOpts)
		l = logger.Get() // Update local logger reference
	}
	if err != nil {
		l.Error("Failed to parse configuration file", slog.String("file", filename), slog.Any("error", err)); os.Exit(1)
		return err
	}
	// Configuration file format
	//
	// backups:
	//   - url: <some url>
	//     username: <some username>
	//     password: <some password>
	//     outputFile: <output file>
	//
	// interval: "1h" // default "1d"
	// retryInterval: "5m" // default "1h"
	// metricsPrefix: "backupremotefiles"

	// parse backups

	// Interval
	if _, ok := data["interval"]; ok {
		c.Interval, err = time.ParseDuration(data["interval"].(string))
		if err != nil {
			l.Error("Failed to parse duration 'interval'", slog.Any("error", err))
			return err
		}
	} else {
		c.Interval, err = time.ParseDuration("1d")
		if err != nil {
			l.Error("Failed to generate duration 'interval' from default value. THIS IS A BUG", slog.Any("error", err)); os.Exit(1)
			return err
		}
	}
	l.Info("Config: interval", slog.String("interval", c.Interval.String()))

	// RetryInterval
	if _, ok := data["retryInterval"]; ok {
		c.RetryInterval, err = time.ParseDuration(data["retryInterval"].(string))
		if err != nil {
			l.Error("Failed to parse duration 'retryInterval'", slog.Any("error", err))
			return err
		}
	} else {
		c.RetryInterval, err = time.ParseDuration("1d")
		if err != nil {
			l.Error("Failed to generate duration 'interval' from default value. THIS IS A BUG", slog.Any("error", err)); os.Exit(1)
			return err
		}
	}
	l.Info("Config: retryInterval", slog.String("retryInterval", c.RetryInterval.String()))

	// Metrics prefix
	if _, ok := data["metricsPrefix"]; ok {
		c.MetricsPrefix = data["metricsPrefix"].(string)
	} else {
		c.MetricsPrefix = "backupremotefiles"
	}
	l.Info("Config: metricsPrefix", slog.String("metricsPrefix", c.RetryInterval.String()))

	c.Backups = make([]Backup, 0)
	for id, v := range data["backups"].(map[string]any) {
		var b Backup
		fb := v.(map[string]any)
		b.ID = id
		b.URL = fb["url"].(string)
		b.Username = fb["username"].(string)
		b.Password = fb["password"].(string)
		b.OutputFile = fb["outputFile"].(string)
		b.RetrieveSuccess = true // initialize status in safe state
		c.Backups = append(c.Backups, b)
		l.Info("Config: backup url", slog.String("url", b.URL))
	}
	return nil
}
