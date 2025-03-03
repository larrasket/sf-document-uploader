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

	gui, err := gui.New()
	if err != nil {
		panic(err)
	}
	defer gui.Close()

	go func() {
		gui.Log("Starting document processing...")
		gui.SetStatus("Authenticating...")
		gui.SetProgress(0.1)

		tokenResp, err := auth.Authenticate()
		if err != nil {
			gui.Log("Authentication failed: %v", err)
			gui.ShowError("Authentication Error", err.Error())
			gui.Quit()
			return
		}
		gui.Log("Successfully authenticated")
		gui.SetProgress(0.3)

		gui.SetStatus("Processing documents...")
		err = processor.ProcessDocuments(tokenResp.AccessToken, "./documents", gui)
		if err != nil {
			gui.Log("Document processing failed: %v", err)
			gui.ShowError("Processing Error", err.Error())
			gui.Quit()
			return
		}

		gui.SetProgress(1.0)
		gui.SetStatus("Completed")
		gui.Log("Document processing completed successfully")
	}()

	gui.Show()

}
