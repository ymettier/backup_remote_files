package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

type ctxKey struct{}

var once sync.Once
var logger *slog.Logger

func getGitRevision() string {
	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		for _, v := range buildInfo.Settings {
			if v.Key == "vcs.revision" {
				return v.Value
			}
		}
	}
	return ""
}

type TeeHandler struct {
	handlers []slog.Handler
}

func (h *TeeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *TeeHandler) Handle(ctx context.Context, record slog.Record) error {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, record.Level) {
			if err := handler.Handle(ctx, record.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *TeeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		newHandlers[i] = handler.WithAttrs(attrs)
	}
	return &TeeHandler{handlers: newHandlers}
}

func (h *TeeHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		newHandlers[i] = handler.WithGroup(name)
	}
	return &TeeHandler{handlers: newHandlers}
}

func newLogger() *slog.Logger {
	var level slog.Level
	levelEnv := os.Getenv("LOG_LEVEL")
	if levelEnv != "" {
		if err := level.UnmarshalText([]byte(strings.ToUpper(levelEnv))); err != nil {
			log.Println(fmt.Errorf("invalid level, defaulting to INFO: %w", err))
			level = slog.LevelInfo
		}
	} else {
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{Key: "timestamp", Value: slog.StringValue(a.Value.Time().Format("2006-01-02T15:04:05.000-0700"))}
			}
			if a.Key == slog.LevelKey {
				return slog.Attr{Key: a.Key, Value: slog.StringValue(strings.ToUpper(a.Value.String()))}
			}
			return a
		},
	}

	logWriters := make(map[string]io.Writer)
	for _, t := range []string{"TXT", "JSON"} {
		filename := os.Getenv("LOG_" + t + "_FILENAME")
		if filename != "" {
			switch filename {
			case "stdout":
				logWriters[t] = os.Stdout
			case "stderr":
				logWriters[t] = os.Stderr
			default:
				logWriters[t] = &lumberjack.Logger{
					Filename:   filename,
					MaxSize:    5,
					MaxBackups: 10,
					MaxAge:     14,
					Compress:   true,
				}
			}
		}
	}
	if len(logWriters) == 0 {
		logWriters["TXT"] = os.Stderr
	}

	var handlers []slog.Handler
	if writer, ok := logWriters["TXT"]; ok {
		handlers = append(handlers, slog.NewTextHandler(writer, opts))
	}
	if writer, ok := logWriters["JSON"]; ok {
		gitRevision := getGitRevision()
		jsonHandler := slog.NewJSONHandler(writer, opts).WithAttrs([]slog.Attr{
			slog.String("git_revision", gitRevision),
			slog.String("go_version", runtime.Version()),
		})
		handlers = append(handlers, jsonHandler)
	}

	var handler slog.Handler
	if len(handlers) == 1 {
		handler = handlers[0]
	} else {
		handler = &TeeHandler{handlers: handlers}
	}
	return slog.New(handler)
}

func Get() *slog.Logger {
	once.Do(func() {
		logger = newLogger()
	})
	return logger
}

func FromCtx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return Get()
}

func WithCtx(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}
