package logger

import (
	"fmt"
	"log"
	"os"
	"time"
)

type Logger struct {
	*log.Logger
}

func New() *Logger {
	return &Logger{
		Logger: log.New(os.Stdout, "", 0),
	}
}

func (l *Logger) log(level, format string, args ...any) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)
	l.Printf("[%s] %s: %s", timestamp, level, message)
}

func (l *Logger) Info(format string, args ...any) {
	l.log("INFO", format, args...)
}

func (l *Logger) Error(format string, args ...any) {
	l.log("ERROR", format, args...)
}

func (l *Logger) Warn(format string, args ...any) {
	l.log("WARN", format, args...)
}

func (l *Logger) Debug(format string, args ...any) {
	l.log("DEBUG", format, args...)
}

func (l *Logger) Fatal(format string, args ...any) {
	l.log("FATAL", format, args...)
	os.Exit(1)
}
