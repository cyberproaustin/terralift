package core

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Level is a log severity.
type Level int

const (
	LevelDebug Level = iota
	LevelVerbose
	LevelInfo
	LevelWarn
	LevelError
)

var levelTags = []string{"DEBUG", "VERBOSE", "INFO", "WARN", "ERROR"}

// ParseLevel maps a string to a Level (defaults to Info).
func ParseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return LevelDebug
	case "verbose":
		return LevelVerbose
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// Logger is a minimal leveled, phase-tagged logger. Writes to stderr so stdout
// stays clean for machine-readable output.
type Logger struct {
	Min Level
}

// NewLogger returns a logger that emits records at or above min.
func NewLogger(min Level) *Logger { return &Logger{Min: min} }

func (l *Logger) log(lvl Level, phase, msg string) {
	if lvl < l.Min {
		return
	}
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	prefix := ""
	if phase != "" {
		prefix = "[" + phase + "] "
	}
	fmt.Fprintf(os.Stderr, "%s %-7s %s%s\n", ts, levelTags[lvl], prefix, msg)
}

func (l *Logger) Verbose(phase, format string, a ...any) {
	l.log(LevelVerbose, phase, fmt.Sprintf(format, a...))
}
func (l *Logger) Info(phase, format string, a ...any) {
	l.log(LevelInfo, phase, fmt.Sprintf(format, a...))
}
func (l *Logger) Warn(phase, format string, a ...any) {
	l.log(LevelWarn, phase, fmt.Sprintf(format, a...))
}
func (l *Logger) Error(phase, format string, a ...any) {
	l.log(LevelError, phase, fmt.Sprintf(format, a...))
}
