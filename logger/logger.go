package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

type ctxKey struct{}

var (
	mu     sync.RWMutex
	logger *Logger
)

type Logger struct {
	*slog.Logger
}

type LogOptions struct {
	JSON       bool
	Level      string
	Filename   string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
}

// newLogger creates a new logger based on opts or environment variables if opts is nil.
func newLogger(opts *LogOptions) *Logger {
	// Define log level
	level := slog.LevelInfo
	var levelStr string
	if opts != nil && opts.Level != "" {
		levelStr = opts.Level
	} else {
		levelStr = os.Getenv("LOG_LEVEL")
	}

	if levelStr != "" {
		var l slog.Level
		if err := l.UnmarshalText([]byte(strings.ToUpper(levelStr))); err != nil {
			log.Println(fmt.Errorf("invalid level, defaulting to INFO: %w", err))
		} else {
			level = l
		}
	}

	handlerOpts := &slog.HandlerOptions{
		Level: level,
	}

	// Define log writer
	var w io.Writer = os.Stderr
	var filename string
	if opts != nil && opts.Filename != "" {
		filename = opts.Filename
	}

	if filename != "" {
		switch filename {
		case "stdout":
			w = os.Stdout
		case "stderr":
			w = os.Stderr
		default:
			l := &lumberjack.Logger{
				Filename: filename,
			}
			if opts != nil {
				l.MaxSize = opts.MaxSize
				l.MaxBackups = opts.MaxBackups
				l.MaxAge = opts.MaxAge
				l.Compress = opts.Compress
			} else {
				// Defaults if no opts provided
				l.MaxSize = 5
				l.MaxBackups = 10
				l.MaxAge = 14
				l.Compress = true
			}
			w = l
		}
	} else if opts != nil {
		// If filename is empty in opts, it means we might want to default to stderr
		// or maybe the user just didn't specify it in the config file.
		// If we are here, filename is empty.
		w = os.Stderr
	}

	// Create new logger
	var handler slog.Handler
	if opts != nil && opts.JSON {
		handler = slog.NewJSONHandler(w, handlerOpts)
	} else {
		handler = slog.NewTextHandler(w, handlerOpts)
	}
	return &Logger{slog.New(handler)}
}

// Get initializes a Logger instance if it has not been initialized
// already and returns the same instance for subsequent calls.
func Get() *Logger {
	mu.RLock()
	l := logger
	mu.RUnlock()

	if l != nil {
		return l
	}

	mu.Lock()
	defer mu.Unlock()
	if logger == nil {
		logger = newLogger(nil)
	}
	return logger
}

// Reset re-initializes the global logger with the provided options.
func Reset(opts *LogOptions) {
	mu.Lock()
	defer mu.Unlock()
	logger = newLogger(opts)
}

// FromCtx returns the Logger associated with the ctx. If no logger
// is associated, the default logger is returned.
func FromCtx(ctx context.Context) *Logger {
	if l, ok := ctx.Value(ctxKey{}).(*Logger); ok {
		return l
	}
	return Get()
}

// WithCtx returns a copy of ctx with the Logger attached.
func WithCtx(ctx context.Context, l *Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}
