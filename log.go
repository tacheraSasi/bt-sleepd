package main

import (
	"fmt"
	"os"
	"time"
)

// Logger writes timestamped messages to stderr. When verbose is false, only
// warnings and errors are printed.
type Logger struct {
	verbose bool
}

func (l *Logger) Info(format string, args ...any) {
	if !l.verbose {
		return
	}
	fmt.Fprintf(os.Stderr, "[%s] ", time.Now().Format(time.TimeOnly))
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func (l *Logger) Warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[%s] WARN ", time.Now().Format(time.TimeOnly))
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func (l *Logger) Error(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[%s] ERROR ", time.Now().Format(time.TimeOnly))
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
