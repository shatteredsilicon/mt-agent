package logger

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

var (
	AllowedLevels  = []string{"debug", "info", "warn", "error"}
	AllowedFormats = []string{"text", "json"}
)

// AllowedLevel is a settable identifier for the minimum level a log entry
type AllowedLevel struct {
	s   string
	lvl *slog.LevelVar
}

func (l *AllowedLevel) String() string {
	return l.s
}

// Set updates the value of the allowed level.
func (l *AllowedLevel) Set(s string) error {
	if l.lvl == nil {
		l.lvl = &slog.LevelVar{}
	}

	switch strings.ToLower(s) {
	case "debug":
		l.lvl.Set(slog.LevelDebug)
	case "info":
		l.lvl.Set(slog.LevelInfo)
	case "warn":
		l.lvl.Set(slog.LevelWarn)
	case "error":
		l.lvl.Set(slog.LevelError)
	default:
		return fmt.Errorf("unrecognized log level %s", s)
	}
	l.s = s
	return nil
}

type AllowedFormat struct {
	s string
}

func (f *AllowedFormat) String() string {
	return f.s
}

// Set updates the value of the allowed format.
func (f *AllowedFormat) Set(s string) error {
	switch s {
	case "text", "json":
		f.s = s
	default:
		return fmt.Errorf("unrecognized log format %s", s)
	}
	return nil
}

func NewLogger(level *AllowedLevel, format *AllowedFormat) *slog.Logger {
	if level == nil {
		level = &AllowedLevel{}
		level.lvl.Set(slog.LevelInfo)
	}

	logHandlerOpts := &slog.HandlerOptions{
		Level:     level.lvl,
		AddSource: true,
	}

	if format != nil && format.s == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, logHandlerOpts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, logHandlerOpts))
}
