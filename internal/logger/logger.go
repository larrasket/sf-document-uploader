package logger

import (
	"log"
	"os"
)

type Logger struct {
	*log.Logger
}

func New() *Logger {
	return &Logger{
		Logger: log.New(os.Stdout, "", log.LstdFlags),
	}
}

func (l *Logger) Info(format string, v ...any) {
	l.Printf("[INFO] "+format, v...)
}

func (l *Logger) Error(format string, v ...any) {
	l.Printf("[ERROR] "+format, v...)
}
