// Copyright 2024 The Backup_icf_cvf Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"backup_remote_files/logger"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"log/slog"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

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

type CLIFlags struct {
	ConfigFile string
	Port       int
}

func parseFlags(version string) CLIFlags {
	fs := flag.NewFlagSet("backup_remote_files", flag.ContinueOnError)
	
	configFile := fs.String("c", "", "Configuration file (required)")
	configFile2 := fs.String("config", "", "Configuration file (required)")
	port := fs.Int("p", 9289, "Exporter port")
	port2 := fs.Int("port", 9289, "Exporter port")
	showVersion := fs.Bool("V", false, "Show version info")
	showVersion2 := fs.Bool("version", false, "Show version info")
	
	fs.Parse(os.Args[1:])
	
	// Handle flags with priority for short form
	config := *configFile
	if config == "" {
		config = *configFile2
	}
	
	portVal := *port
	if portVal == 9289 && *port2 != 9289 {
		portVal = *port2
	}
	
	if *showVersion || *showVersion2 {
		output := printVersion(version)
		fmt.Printf("%s", output)
		os.Exit(0)
	}
	
	if config == "" {
		fmt.Fprintf(os.Stderr, "Error: -c/--config is required\n")
		os.Exit(1)
	}
	
	return CLIFlags{
		ConfigFile: config,
		Port:       portVal,
	}
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
	flags := parseFlags(version)
	var cfg Config
	cfg.Port = flags.Port

	err := cfg.readConfig(flags.ConfigFile)
	if err != nil {
		return cfg, err
	}

	return cfg, nil
}

func loggerConfig(logging map[string]any) logger.LogOptions {
	logOpts := logger.LogOptions{
		Level:      "INFO",
		Filename:   "",
		MaxSize:    5,
		MaxBackups: 10,
		MaxAge:     14,
		Compress:   true,
		JSON:       false,
	}

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
	if isJSON, ok := logging["json"].(bool); ok {
		logOpts.JSON = isJSON
	}
	return logOpts
}

func (c *Config) readConfig(filename string) error {
	l := logger.Get()
	
	// Initialize Koanf with YAML parser
	k := koanf.New(".")
	if err := k.Load(file.Provider(filename), yaml.Parser()); err != nil {
		l.Error("Failed to read configuration file", slog.String("file", filename), slog.Any("error", err))
		os.Exit(1)
		return err
	}

	// Logging configuration
	if k.Exists("logging") {
		logging := k.Get("logging").(map[string]any)
		logOpts := loggerConfig(logging)
		logger.Reset(&logOpts)
		l = logger.Get() // Update local logger reference
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
	// logging:
	//   level: <log level>            // default "info"
	//   filename: <log filename>      // default ""
	//   maxSize: <max log size>       // default 5
	//   maxBackups: <max log backups> // Default 10
	//   maxAge: <max log age>         // Default 14
	//   compress: <compress log>      // Default true
	//   json: <log in JSON>           // Default false

	// Interval
	var err error
	if k.Exists("interval") {
		c.Interval, err = time.ParseDuration(k.String("interval"))
		if err != nil {
			l.Error("Failed to parse duration 'interval'", slog.Any("error", err))
			return err
		}
	} else {
		c.Interval, err = time.ParseDuration("1d")
		if err != nil {
			l.Error("Failed to generate duration 'interval' from default value. THIS IS A BUG", slog.Any("error", err))
			os.Exit(1)
			return err
		}
	}
	l.Info("Config: interval", slog.String("interval", c.Interval.String()))

	// RetryInterval
	if k.Exists("retryInterval") {
		c.RetryInterval, err = time.ParseDuration(k.String("retryInterval"))
		if err != nil {
			l.Error("Failed to parse duration 'retryInterval'", slog.Any("error", err))
			return err
		}
	} else {
		c.RetryInterval, err = time.ParseDuration("1d")
		if err != nil {
			l.Error("Failed to generate duration 'interval' from default value. THIS IS A BUG", slog.Any("error", err))
			os.Exit(1)
			return err
		}
	}
	l.Info("Config: retryInterval", slog.String("retryInterval", c.RetryInterval.String()))

	// Metrics prefix
	if k.Exists("metricsPrefix") {
		c.MetricsPrefix = k.String("metricsPrefix")
	} else {
		c.MetricsPrefix = "backupremotefiles"
	}
	l.Info("Config: metricsPrefix", slog.String("metricsPrefix", c.RetryInterval.String()))

	c.Backups = make([]Backup, 0)
	if k.Exists("backups") {
		backups := k.Get("backups").(map[string]any)
		for id, v := range backups {
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
	}
	return nil
}
