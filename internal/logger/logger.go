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

type LogEntry struct {
	Timestamp time.Time
	Level     LogLevel
	Message   string
}

type LogConfig struct {
	Level     LogLevel
	ShowInGUI bool
	Emoji     string
}

var logConfigs = map[LogLevel]LogConfig{
	DEBUG: {
		Level:     DEBUG,
		ShowInGUI: false,
		Emoji:     "🔍",
	},
	INFO: {
		Level:     INFO,
		ShowInGUI: true,
		Emoji:     "📝",
	},
	WARNING: {
		Level:     WARNING,
		ShowInGUI: true,
		Emoji:     "⚠️",
	},
	ERROR: {
		Level:     ERROR,
		ShowInGUI: true,
		Emoji:     "❌",
	},
	SUCCESS: {
		Level:     SUCCESS,
		ShowInGUI: true,
		Emoji:     "✅",
	},
}

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
		if err := instance.initLogFile(); err != nil {
			fmt.Printf("Failed to initialize logger: %v\n", err)
		}
	})
	return instance
}

func (l *Logger) initLogFile() error {
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
		return fmt.Errorf("failed to create/open log file: %v", err)
	}

	l.logFile = logFile
	return nil
}

func (l *Logger) SetGuiLogView(logView *widget.TextGrid) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.guiLogView = logView
}

func (l *Logger) logWithConfig(level LogLevel, showInGUI bool, emoji string, format string, args ...any) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   fmt.Sprintf(format, args...),
	}

	// Write to file
	if l.logFile != nil {
		fileLog := fmt.Sprintf("[%s] [%s] %s\n",
			entry.Timestamp.Format("2006-01-02 15:04:05"),
			getLevelString(level),
			entry.Message)

		if _, err := l.logFile.WriteString(fileLog); err != nil {
			fmt.Printf("Error writing to log file: %v\n", err)
		}
		l.logFile.Sync()
	}

	// Show in GUI if configured
	if showInGUI && l.guiLogView != nil {
		timeStr := entry.Timestamp.Format("15:04:05")
		guiLog := fmt.Sprintf("%s %s %s", timeStr, emoji, entry.Message)

		currentText := l.guiLogView.Text()
		if currentText != "" {
			currentText += "\n"
		}
		l.guiLogView.SetText(currentText + guiLog)
		l.guiLogView.Refresh()
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

// Regular logging methods (file only)
func (l *Logger) Debug(format string, args ...any) {
	config := logConfigs[DEBUG]
	l.logWithConfig(DEBUG, config.ShowInGUI, config.Emoji, format, args...)
}

func (l *Logger) Info(format string, args ...any) {
	config := logConfigs[INFO]
	l.logWithConfig(INFO, config.ShowInGUI, config.Emoji, format, args...)
}

func (l *Logger) Warning(format string, args ...any) {
	config := logConfigs[WARNING]
	l.logWithConfig(WARNING, config.ShowInGUI, config.Emoji, format, args...)
}

func (l *Logger) Error(format string, args ...any) {
	config := logConfigs[ERROR]
	l.logWithConfig(ERROR, config.ShowInGUI, config.Emoji, format, args...)
}

func (l *Logger) Success(format string, args ...any) {
	config := logConfigs[SUCCESS]
	l.logWithConfig(SUCCESS, config.ShowInGUI, config.Emoji, format, args...)
}

func (l *Logger) Close() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.logFile != nil {
		l.logFile.Close()
	}
}
