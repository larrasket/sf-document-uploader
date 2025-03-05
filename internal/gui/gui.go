package gui

import (
	"fmt"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	logging "github.com/ORAITApps/document-uploader/internal/logger"
)

type App struct {
	fyneApp           fyne.App
	window            fyne.Window
	logView           *widget.TextGrid
	progress          *widget.ProgressBar
	status            *widget.Label
	pathLabel         *widget.Label
	startBtn          *widget.Button
	documentsPath     string
	processStarted    bool
	processingHandler func()
}

func NewApp() *App {
	a := app.NewWithID("com.orait.document-uploader")
	w := a.NewWindow("Document Uploader")

	return &App{
		fyneApp:   a,
		window:    w,
		logView:   widget.NewTextGrid(),
		progress:  widget.NewProgressBar(),
		status:    widget.NewLabel("Select documents directory to begin"),
		pathLabel: widget.NewLabel("No directory selected"),
	}
}

func (a *App) Run() {
	selectBtn := widget.NewButton("Select Directory", a.handleDirectorySelection)
	a.startBtn = widget.NewButton("Start Processing", a.handleStartProcessing)
	a.startBtn.Disable()

	buttons := container.NewHBox(selectBtn, a.startBtn)

	pathInfo := container.NewHBox(
		widget.NewLabel("Selected Directory:"),
		a.pathLabel,
	)

	progressSection := container.NewVBox(
		a.status,
		a.progress,
	)

	logScroll := container.NewScroll(a.logView)
	logScroll.SetMinSize(fyne.NewSize(600, 300))

	content := container.NewVBox(
		buttons,
		pathInfo,
		progressSection,
		logScroll,
	)

	a.window.SetContent(content)
	a.window.Resize(fyne.NewSize(700, 500))
	a.window.ShowAndRun()
}

func (a *App) AddLog(message string) {
	currentText := a.logView.Text()
	if currentText != "" {
		currentText += "\n"
	}
	a.logView.SetText(currentText + message)
}

func (a *App) SetStatus(status string) {
	a.status.SetText(status)
}

func (a *App) SetProgress(value float64) {
	a.progress.SetValue(value)
}

func (a *App) GetLogView() *widget.TextGrid {
	return a.logView
}

func (a *App) ShowError(title, message string) {
	dialog.ShowError(fmt.Errorf(message), a.window)
}

func (a *App) SetProcessingHandler(handler func()) {
	a.processingHandler = handler
}

func (a *App) Reset() {
	a.processStarted = false
	a.progress.SetValue(0)
	a.SetStatus("Ready to start")
	a.startBtn.Enable()
	a.logView.SetText("")
}

func (a *App) handleStartProcessing() {
	if a.processStarted {
		return
	}

	if a.documentsPath == "" {
		a.ShowError("Error", "Please select a documents directory first")
		return
	}

	if _, err := os.Stat(a.documentsPath); os.IsNotExist(err) {
		a.ShowError("Error", fmt.Sprintf("Selected directory no longer exists: %s", a.documentsPath))
		a.documentsPath = ""
		a.pathLabel.SetText("No directory selected")
		a.startBtn.Disable()
		return
	}

	a.processStarted = true
	a.startBtn.Disable()
	a.progress.SetValue(0)
	a.SetStatus("Starting process...")

	if a.processingHandler != nil {
		go a.processingHandler()
	}
}

func (a *App) handleDirectorySelection() {
	cwd, err := os.Getwd()
	if err != nil {
		a.ShowError("Error", "Failed to get current directory: "+err.Error())
		return
	}

	folderDialog := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			a.ShowError("Directory Selection Error", err.Error())
			return
		}
		if uri == nil {
			return
		}

		a.Reset()

		path := uri.Path()
		if _, err := os.Stat(path); os.IsNotExist(err) {
			a.ShowError("Error", "Selected directory does not exist")
			return
		}

		a.documentsPath = path
		a.pathLabel.SetText(filepath.Base(path))
		logger := logging.GetLogger()
		logger.Success("Selected directory: " + path)
		a.startBtn.Enable()
	}, a.window)

	startURI, err := storage.ParseURI("file://" + cwd)
	if err == nil {
		listURI, err := storage.ListerForURI(startURI)
		if err == nil && listURI != nil {
			folderDialog.SetLocation(listURI)
		}
	}

	folderDialog.Show()
}

func (a *App) GetDocumentsPath() string {
	if a.documentsPath == "" {
		return ""
	}
	if _, err := os.Stat(a.documentsPath); os.IsNotExist(err) {
		return ""
	}
	return a.documentsPath
}
