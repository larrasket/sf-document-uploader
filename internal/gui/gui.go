package gui

import (
	"fmt"
	"os"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type GUI struct {
	window      fyne.Window
	logView     *widget.TextGrid
	progress    *widget.ProgressBar
	statusLabel *widget.Label
	logFile     *os.File
	mutex       sync.Mutex
	app         fyne.App
}

func New() (*GUI, error) {
	application := app.New()
	window := application.NewWindow("Document Uploader")

	logFile, err := os.OpenFile(
		fmt.Sprintf("upload_log_%s.txt", time.Now().Format("2006-01-02_15-04-05")),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0666,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %v", err)
	}

	gui := &GUI{
		window:  window,
		logFile: logFile,
		app:     application,
	}

	gui.setupUI()
	return gui, nil
}

func (g *GUI) setupUI() {
	g.logView = widget.NewTextGrid()
	g.logView.SetText("")
	g.progress = widget.NewProgressBar()
	g.statusLabel = widget.NewLabel("Ready")

	scrollContainer := container.NewScroll(g.logView)
	scrollContainer.SetMinSize(fyne.NewSize(600, 300))

	content := container.NewVBox(
		widget.NewLabel("Document Upload Progress"),
		g.progress,
		g.statusLabel,
		scrollContainer,
	)

	g.window.SetContent(content)
	g.window.Resize(fyne.NewSize(700, 400))
}

func (g *GUI) Show() {
	g.window.ShowAndRun()
}

func (g *GUI) Log(format string, v ...interface{}) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	message := fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, v...))

	// Update GUI
	currentText := g.logView.Text()
	g.logView.SetText(currentText + message)

	// Write to log file
	g.logFile.WriteString(message)

	// Auto-scroll to bottom
	g.logView.Refresh()
}

func (g *GUI) SetProgress(value float64) {
	g.progress.SetValue(value)
}

func (g *GUI) SetStatus(status string) {
	g.statusLabel.SetText(status)
}

func (g *GUI) ShowError(title, message string) {
	dialog.ShowError(fmt.Errorf(message), g.window)
}

func (g *GUI) Close() {
	if g.logFile != nil {
		g.logFile.Close()
	}
}

func (g *GUI) Quit() {
	g.app.Quit()
}
