package logging

import (
	"fmt"
	"io"
	"log"
	"strings"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

type Logger struct {
	level  Level
	logger *log.Logger
}

func New(w io.Writer, level string) *Logger {
	return &Logger{
		level:  parseLevel(level),
		logger: log.New(w, "", 0),
	}
}

func (l *Logger) Debugf(format string, args ...any) {
	l.logf(LevelDebug, "DEBUG", format, args...)
}

func (l *Logger) Infof(format string, args ...any) {
	l.logf(LevelInfo, "INFO", format, args...)
}

func (l *Logger) Warnf(format string, args ...any) {
	l.logf(LevelWarn, "WARN", format, args...)
}

func (l *Logger) Errorf(format string, args ...any) {
	l.logf(LevelError, "ERROR", format, args...)
}

func (l *Logger) logf(level Level, label, format string, args ...any) {
	if l == nil || level < l.level {
		return
	}

	l.logger.Printf("[%s] %s", label, fmt.Sprintf(format, args...))
}

func parseLevel(value string) Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return LevelDebug
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}
