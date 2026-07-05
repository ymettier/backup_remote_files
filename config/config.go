// Copyright 2024-2026 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"

	"backup_remote_files/logger"
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

func ParseFlags(version string, args []string) CLIFlags {
	fs := pflag.NewFlagSet("backup_remote_files", pflag.ContinueOnError)

	configFile := fs.StringP("config", "c", "", "Configuration file (required)")
	port := fs.IntP("port", "p", defaultPort, "Exporter port")
	showVersion := fs.BoolP("version", "V", false, "Show version info")
	showHelp := fs.BoolP("help", "h", false, "Print help")

	if err := fs.Parse(args); err != nil {
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
	ID         string
	URL        string
	Username   string
	Password   string
	OutputFile string
}

type Config struct {
	Port          int
	Backups       []Backup
	Interval      time.Duration
	RetryInterval time.Duration
	MetricsPrefix string
}

func New(configFile string, port int) (Config, error) {
	var cfg Config
	cfg.Port = port

	err := cfg.readConfig(configFile)
	if err != nil {
		return cfg, err
	}

	return cfg, nil
}

func lookupConfigKey(k *koanf.Koanf, camelKey string) (string, bool) {
	envKey := strings.ToLower(camelKey)
	if k.Exists(envKey) {
		return k.String(envKey), true
	}
	if k.Exists(camelKey) {
		return k.String(camelKey), true
	}
	underscoreKey := strings.ReplaceAll(strings.ToLower(camelKey), ".", "_")
	if k.Exists(underscoreKey) {
		return k.String(underscoreKey), true
	}
	return "", false
}

func (c *Config) getConfigString(k *koanf.Koanf, camelKey, defaultValue string) string {
	if val, ok := lookupConfigKey(k, camelKey); ok {
		return val
	}
	return defaultValue
}

func (c *Config) getConfigDuration(k *koanf.Koanf, camelKey, defaultDuration string) (time.Duration, error) {
	durationStr := defaultDuration
	if val, ok := lookupConfigKey(k, camelKey); ok {
		durationStr = val
	}
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return 0, err
	}
	return duration, nil
}

func loggerConfig(k *koanf.Koanf) logger.LogOptions {
	logOpts := logger.LogOptions{
		Level:      "INFO",
		Filename:   "",
		MaxSize:    5,
		MaxBackups: 10,
		MaxAge:     14,
		Compress:   true,
		JSON:       false,
	}

	if v := k.String("logging.level"); v != "" {
		logOpts.Level = v
	}
	if v := k.String("logging.filename"); v != "" {
		logOpts.Filename = v
	}
	if k.Exists("logging.maxSize") {
		logOpts.MaxSize = k.Int("logging.maxSize")
	}
	if k.Exists("logging.maxBackups") {
		logOpts.MaxBackups = k.Int("logging.maxBackups")
	}
	if k.Exists("logging.maxAge") {
		logOpts.MaxAge = k.Int("logging.maxAge")
	}
	if k.Exists("logging.compress") {
		logOpts.Compress = k.Bool("logging.compress")
	}
	if k.Exists("logging.json") {
		logOpts.JSON = k.Bool("logging.json")
	}
	return logOpts
}

func (c *Config) readConfig(filename string) error {
	l := logger.Get()

	// Initialize Koanf with YAML parser
	k := koanf.New(".")
	if err := k.Load(file.Provider(filename), yaml.Parser()); err != nil {
		return fmt.Errorf("failed to read configuration file %s: %w", filename, err)
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
		return fmt.Errorf("failed to load environment variables: %w", err)
	}

	// Logging configuration
	if k.Exists("logging") {
		logOpts := loggerConfig(k)
		logger.Reset(&logOpts)
		l = logger.Get()
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
		for _, id := range k.MapKeys("backups") {
			prefix := "backups." + id + "."
			b := Backup{
				ID:         id,
				URL:        k.String(prefix + "url"),
				Username:   k.String(prefix + "username"),
				Password:   k.String(prefix + "password"),
				OutputFile: k.String(prefix + "outputFile"),
			}
			c.Backups = append(c.Backups, b)
			l.Info("Config: backup url", slog.String("url", b.URL))
		}
	}
	return nil
}
