package main

import (
	"embed"

	"github.com/ORAITApps/document-uploader/internal/auth"
	"github.com/ORAITApps/document-uploader/internal/config"
	"github.com/ORAITApps/document-uploader/internal/gui"
	"github.com/ORAITApps/document-uploader/internal/processor"
)

//go:embed .env
var env embed.FS

func main() {
	config.LoadEnv(env)
	app := gui.NewApp()

	// Add processing functionality
	app.SetProcessingHandler(func() {
		app.SetStatus("Authenticating...")
		app.SetProgress(0.1)

		tokenResp, err := auth.Authenticate()
		if err != nil {
			app.AddLog("Authentication failed: " + err.Error())
			app.ShowError("Authentication Error", err.Error())
			app.Reset()
			return
		}

		app.AddLog("Authentication successful")

		if err := processor.ProcessDocuments(tokenResp.AccessToken, app.GetDocumentsPath(), app); err != nil {
			app.AddLog("Processing failed: " + err.Error())
			app.ShowError("Processing Error", err.Error())
			app.Reset()
			return
		}

		app.SetProgress(1.0)
		app.SetStatus("Completed")
		app.AddLog("Processing completed successfully")
	})

	app.Run()
}
