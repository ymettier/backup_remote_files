// Copyright 2024-2026 The Backup_remote_files Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	_ "embed"
	"flag"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"

	"backup_remote_files/logger"
)

const defaultPort = 9289

//go:embed config.default.yaml
var defaultConfig []byte

// envKeyAliases maps BRF_ environment variable suffixes to their canonical
// configuration key when the generic conversion would not produce it.
var envKeyAliases = map[string]string{
	"retryinterval":          "retryInterval",
	"metricsprefix":          "exporter.metricsPrefix",
	"exporter_metricsprefix": "exporter.metricsPrefix",
	"backuptimeout":          "backupTimeout",
	"log_level":              "logging.level",
	"logging_maxsize":        "logging.maxSize",
	"logging_maxbackups":     "logging.maxBackups",
	"logging_maxage":         "logging.maxAge",
}

// envKey converts a BRF_ environment variable name into a koanf key.
func envKey(s string) string {
	key := strings.ToLower(strings.TrimPrefix(s, "BRF_"))
	if canonical, ok := envKeyAliases[key]; ok {
		return canonical
	}
	return strings.ReplaceAll(key, "_", ".")
}

func versionInfo(version string) string {
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
	PortSet    bool
}

func ParseFlags(version string, args []string) (CLIFlags, error) {
	fs := pflag.NewFlagSet("backup_remote_files", pflag.ContinueOnError)

	configFile := fs.StringP("config", "c", "", "Configuration file (required)")
	port := fs.IntP("port", "p", defaultPort, "Exporter port")
	showVersion := fs.BoolP("version", "V", false, "Show version info")
	showHelp := fs.BoolP("help", "h", false, "Print help")

	if err := fs.Parse(args); err != nil {
		return CLIFlags{}, err
	}

	if *showHelp {
		fs.PrintDefaults()
		return CLIFlags{}, flag.ErrHelp
	}

	if *showVersion {
		output := versionInfo(version)
		fmt.Print(output)
		return CLIFlags{}, flag.ErrHelp
	}

	if *configFile == "" {
		return CLIFlags{}, fmt.Errorf("-c/--config is required")
	}

	return CLIFlags{
		ConfigFile: *configFile,
		Port:       *port,
		PortSet:    fs.Changed("port"),
	}, nil
}

type Backup struct {
	ID         string
	URL        string
	Username   string
	Password   string
	OutputFile string
	Timeout    time.Duration
}

type Config struct {
	Port          int
	Backups       []Backup
	Interval      time.Duration
	RetryInterval time.Duration
	MetricsPrefix string
}

func New(configFile string, port int, portSet bool) (Config, error) {
	var cfg Config

	err := cfg.readConfig(configFile)
	if err != nil {
		return cfg, err
	}

	if portSet {
		cfg.Port = port
	}

	return cfg, nil
}

func getConfigDuration(k *koanf.Koanf, key string) (time.Duration, error) {
	return time.ParseDuration(k.String(key))
}

func loggerConfig(k *koanf.Koanf) logger.LogOptions {
	return logger.LogOptions{
		Level:      k.String("logging.level"),
		Filename:   k.String("logging.filename"),
		MaxSize:    k.Int("logging.maxSize"),
		MaxBackups: k.Int("logging.maxBackups"),
		MaxAge:     k.Int("logging.maxAge"),
		Compress:   k.Bool("logging.compress"),
		JSON:       k.Bool("logging.json"),
	}
}

func (c *Config) readConfig(filename string) error {
	// Initialize Koanf with the embedded defaults
	k := koanf.New(".")
	if err := k.Load(rawbytes.Provider(defaultConfig), yaml.Parser()); err != nil {
		return fmt.Errorf("failed to load default configuration: %w", err)
	}

	// Merge the user configuration on top of the defaults
	if err := k.Load(file.Provider(filename), yaml.Parser()); err != nil {
		return fmt.Errorf("failed to read configuration file %s: %w", filename, err)
	}

	// Load environment variables with BRF_ prefix (overrides YAML values)
	if err := k.Load(env.Provider("BRF_", ".", envKey), nil); err != nil {
		return fmt.Errorf("failed to load environment variables: %w", err)
	}

	// Logging configuration
	logOpts := loggerConfig(k)
	logger.Reset(&logOpts)
	l := logger.Get()

	if err := c.readIntervals(k, l); err != nil {
		return err
	}

	c.Port = k.Int("exporter.port")
	c.MetricsPrefix = k.String("exporter.metricsPrefix")
	l.Info("Config: metricsPrefix", slog.String("metricsPrefix", c.MetricsPrefix))

	return c.readBackups(k, l)
}

func (c *Config) readIntervals(k *koanf.Koanf, l *logger.Logger) error {
	var err error

	// Interval
	c.Interval, err = getConfigDuration(k, "interval")
	if err != nil {
		l.Error("Failed to parse duration 'interval'", slog.Any("error", err))
		return err
	}
	l.Info("Config: interval", slog.String("interval", c.Interval.String()))

	// RetryInterval
	c.RetryInterval, err = getConfigDuration(k, "retryInterval")
	if err != nil {
		l.Error("Failed to parse duration 'retryInterval'", slog.Any("error", err))
		return err
	}
	l.Info("Config: retryInterval", slog.String("retryInterval", c.RetryInterval.String()))

	return nil
}

func (c *Config) readBackups(k *koanf.Koanf, l *logger.Logger) error {
	c.Backups = make([]Backup, 0)

	for _, id := range k.MapKeys("backups") {
		backup, err := readBackup(k, l, id)
		if err != nil {
			return err
		}
		c.Backups = append(c.Backups, backup)
		l.Info("Config: backup url", slog.String("url", logger.RedactURL(backup.URL)))
	}
	return nil
}

func readBackup(k *koanf.Koanf, l *logger.Logger, id string) (Backup, error) {
	prefix := "backups." + id + "."
	timeoutKey := prefix + "timeout"
	if !k.Exists(timeoutKey) {
		timeoutKey = "backupTimeout"
	}
	timeout, err := getConfigDuration(k, timeoutKey)
	if err != nil {
		l.Error("Failed to parse duration 'timeout' for backup", slog.String("id", id), slog.Any("error", err))
		return Backup{}, err
	}

	url := k.String(prefix + "url")
	if url == "" {
		return Backup{}, fmt.Errorf("backup %q: url is required", id)
	}
	outputFile := k.String(prefix + "outputFile")
	if outputFile == "" {
		return Backup{}, fmt.Errorf("backup %q: outputFile is required", id)
	}

	return Backup{
		ID:         id,
		URL:        url,
		Username:   k.String(prefix + "username"),
		Password:   k.String(prefix + "password"),
		OutputFile: outputFile,
		Timeout:    timeout,
	}, nil
}
