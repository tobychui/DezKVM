package logger

import (
	"log"
	"os"
)

type LogLevel int

const (
	DebugLevel LogLevel = iota
	InfoLevel
	ErrorLevel
)

type Logger struct {
	level  LogLevel
	logger *log.Logger
}

type LogFunc func(format string, v ...interface{})

type LoggerOption func(*Logger)

func WithLogLevel(level LogLevel) LoggerOption {
	return func(l *Logger) {
		l.level = level
	}
}

func WithOutput(output *os.File) LoggerOption {
	return func(l *Logger) {
		l.logger.SetOutput(output)
	}
}

func NewLogger(opts ...LoggerOption) *Logger {
	l := &Logger{
		level:  InfoLevel,
		logger: log.New(os.Stdout, "", log.LstdFlags),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

func (l *Logger) Debug(format string, v ...interface{}) {
	if l.level <= DebugLevel {
		l.logger.Printf("[DEBUG] "+format, v...)
	}
}

func (l *Logger) Info(format string, v ...interface{}) {
	if l.level <= InfoLevel {
		l.logger.Printf("[INFO] "+format, v...)
	}
}

func (l *Logger) Error(format string, v ...interface{}) {
	if l.level <= ErrorLevel {
		l.logger.Printf("[ERROR] "+format, v...)
	}
}

func (l *Logger) LogFunc(level LogLevel) LogFunc {
	return func(format string, v ...interface{}) {
		switch level {
		case DebugLevel:
			l.Debug(format, v...)
		case InfoLevel:
			l.Info(format, v...)
		case ErrorLevel:
			l.Error(format, v...)
		}
	}
}
