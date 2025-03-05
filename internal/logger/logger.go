package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/fyne/v2/widget"
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARNING
	ERROR
	SUCCESS
)

type Logger struct {
	logFile    *os.File
	guiLogView *widget.TextGrid
	mutex      sync.Mutex
}

var instance *Logger
var once sync.Once

func GetLogger() *Logger {
	once.Do(func() {
		instance = &Logger{}
	})
	return instance
}

func (l *Logger) Initialize(guiLogView *widget.TextGrid) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %v", err)
	}

	logsDir := filepath.Join(cwd, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %v", err)
	}

	logFileName := filepath.Join(logsDir, fmt.Sprintf("document_uploader_%s.log",
		time.Now().Format("2006-01-02")))
	logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to create log file: %v", err)
	}

	l.logFile = logFile
	l.guiLogView = guiLogView
	return nil
}

func (l *Logger) log(level LogLevel, format string, args ...any) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	timestamp := time.Now().Format("15:04:05")
	message := fmt.Sprintf(format, args...)

	fileLog := fmt.Sprintf("[%s] [%s] %s\n", timestamp, getLevelString(level), message)
	if l.logFile != nil {
		l.logFile.WriteString(fileLog)
	}

	if l.guiLogView != nil {
		emoji := getEmojiForLevel(level)
		guiLog := fmt.Sprintf("%s %s %s\n", timestamp, emoji, message)

		currentText := l.guiLogView.Text()
		l.guiLogView.SetText(currentText + guiLog)
		l.guiLogView.Refresh()
	}
}

func getEmojiForLevel(level LogLevel) string {
	switch level {
	case DEBUG:
		return "🔍"
	case INFO:
		return "ℹ️"
	case WARNING:
		return "⚠️"
	case ERROR:
		return "❌"
	case SUCCESS:
		return "✅"
	default:
		return "•"
	}
}

func getLevelString(level LogLevel) string {
	switch level {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARNING:
		return "WARN"
	case ERROR:
		return "ERROR"
	case SUCCESS:
		return "SUCCESS"
	default:
		return "UNKNOWN"
	}
}

func (l *Logger) Debug(format string, args ...any) {
	l.log(DEBUG, format, args...)
}

func (l *Logger) Info(format string, args ...any) {
	l.log(INFO, format, args...)
}

func (l *Logger) Warning(format string, args ...any) {
	l.log(WARNING, format, args...)
}

func (l *Logger) Error(format string, args ...any) {
	l.log(ERROR, format, args...)
}

func (l *Logger) Success(format string, args ...any) {
	l.log(SUCCESS, format, args...)
}

func (l *Logger) Close() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.logFile != nil {
		l.logFile.Close()
	}
}
