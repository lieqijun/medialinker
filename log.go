package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	levelDebug = iota
	levelInfo
	levelWarn
	levelError
)

// Logger 极简日志
type Logger struct {
	mu    sync.Mutex
	level int
}

func NewLogger(level string) *Logger {
	l := levelInfo
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		l = levelDebug
	case "warn", "warning":
		l = levelWarn
	case "error":
		l = levelError
	}
	return &Logger{level: l}
}

func (l *Logger) log(level int, tag, format string, args ...any) {
	if level < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stdout, "%s %s %s\n", time.Now().Format("2006-01-02 15:04:05"), tag, msg)
}

func (l *Logger) Debugf(format string, args ...any) { l.log(levelDebug, "[DEBUG]", format, args...) }
func (l *Logger) Infof(format string, args ...any)  { l.log(levelInfo, "[INFO ]", format, args...) }
func (l *Logger) Warnf(format string, args ...any)  { l.log(levelWarn, "[WARN ]", format, args...) }
func (l *Logger) Errorf(format string, args ...any) { l.log(levelError, "[ERROR]", format, args...) }
