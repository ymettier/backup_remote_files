// Copyright 2024 The Backup_icf_cvf Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"backup_remote_files/logger"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"log/slog"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

const defaultPort = 9289

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
	fs := pflag.NewFlagSet("backup_remote_files", pflag.ContinueOnError)

	configFile := fs.StringP("config", "c", "", "Configuration file (required)")
	port := fs.IntP("port", "p", defaultPort, "Exporter port")
	showVersion := fs.BoolP("version", "V", false, "Show version info")
	showHelp := fs.BoolP("help", "h", false, "Print help")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if *showHelp {
		fs.PrintDefaults()
		os.Exit(0)
	}

	if *showVersion {
		output := printVersion(version)
		fmt.Printf("%s", output)
		os.Exit(0)
	}

	if *configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -c/--config is required\n")
		os.Exit(1)
	}

	return CLIFlags{
		ConfigFile: *configFile,
		Port:       *port,
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

func (c *Config) getConfigString(k *koanf.Koanf, camelKey, defaultValue string) string {
	// Check env form (lowercase) first so env vars override YAML values.
	// Env provider transforms BRF_RETRYINTERVAL -> retryinterval,
	// while YAML keys use camelCase (retryInterval).
	envKey := strings.ToLower(camelKey)
	if k.Exists(envKey) {
		return k.String(envKey)
	}
	// Check YAML form (camelCase for flat keys like "retryInterval")
	if k.Exists(camelKey) {
		return k.String(camelKey)
	}
	// For nested keys like "logging.level", also check with underscore form
	// (in case the transformer created underscore version instead of dot)
	underscoreKey := strings.ReplaceAll(strings.ToLower(camelKey), ".", "_")
	if k.Exists(underscoreKey) {
		return k.String(underscoreKey)
	}
	return defaultValue
}

func (c *Config) getConfigDuration(k *koanf.Koanf, camelKey, defaultDuration string) (time.Duration, error) {
	var durationStr string
	// Check env form (lowercase) first so env vars override YAML values.
	envKey := strings.ToLower(camelKey)
	if k.Exists(envKey) {
		durationStr = k.String(envKey)
	} else if k.Exists(camelKey) {
		durationStr = k.String(camelKey)
	} else {
		// For nested keys like "logging.level", also check with underscore form
		underscoreKey := strings.ReplaceAll(strings.ToLower(camelKey), ".", "_")
		if k.Exists(underscoreKey) {
			durationStr = k.String(underscoreKey)
		} else {
			durationStr = defaultDuration
		}
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return 0, err
	}
	return duration, nil
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

	// Load environment variables with BRF_ prefix (overrides YAML values)
	// For flat keys: BRF_RETRYINTERVAL -> retryInterval, BRF_INTERVAL -> interval
	// For nested keys: BRF_LOGGING_LEVEL -> logging.level
	if err := k.Load(env.Provider("BRF_", ".", func(s string) string {
		// Remove BRF_ prefix
		s = strings.TrimPrefix(s, "BRF_")
		// Convert to lowercase
		s = strings.ToLower(s)
		// Replace underscores with dots for nested keys
		s = strings.ReplaceAll(s, "_", ".")
		return s
	}), nil); err != nil {
		l.Error("Failed to load environment variables", slog.Any("error", err))
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
	// Interval
	var err error
	c.Interval, err = c.getConfigDuration(k, "interval", "24h")
	if err != nil {
		l.Error("Failed to parse duration 'interval'", slog.Any("error", err))
		return err
	}
	l.Info("Config: interval", slog.String("interval", c.Interval.String()))

	// RetryInterval
	c.RetryInterval, err = c.getConfigDuration(k, "retryInterval", "1d")
	if err != nil {
		l.Error("Failed to parse duration 'retryInterval'", slog.Any("error", err))
		return err
	}
	l.Info("Config: retryInterval", slog.String("retryInterval", c.RetryInterval.String()))

	// Metrics prefix
	c.MetricsPrefix = c.getConfigString(k, "metricsPrefix", "backupremotefiles")
	l.Info("Config: metricsPrefix", slog.String("metricsPrefix", c.MetricsPrefix))

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
