package main

import (
	"embed"

	"github.com/ORAITApps/document-uploader/internal/auth"
	"github.com/ORAITApps/document-uploader/internal/config"
	"github.com/ORAITApps/document-uploader/internal/gui"
	logging "github.com/ORAITApps/document-uploader/internal/logger"
	"github.com/ORAITApps/document-uploader/internal/processor"
)

//go:embed .env
var env embed.FS

func main() {
	config.LoadEnv(env)
	app := gui.NewApp()

	logger := logging.GetLogger()
	defer logger.Close()

	app.SetProcessingHandler(func() {
		logger.Info("Starting authentication process...")
		app.SetStatus("Authenticating...")
		app.SetProgress(0.1)

		tokenResp, err := auth.Authenticate()
		if err != nil {
			logger.Error("Authentication failed: %v", err)
			app.ShowError("Authentication Error", err.Error())
			app.Reset()
			return
		}

		logger.Success("✅ Authentication successful")

		if err := processor.ProcessDocuments(tokenResp.AccessToken, app.GetDocumentsPath(), app); err != nil {
			logger.Error("Processing failed: %v", err)
			app.ShowError("Processing Error", err.Error())
			app.Reset()
			return
		}

		logger.Success("🎉 Processing completed successfully!")
		app.SetProgress(1.0)
		app.SetStatus("Completed")
	})

	app.Run()
}
