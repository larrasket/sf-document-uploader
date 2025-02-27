package main

import (
	"embed"

	"github.com/ORAITApps/document-uploader/internal/auth"
	"github.com/ORAITApps/document-uploader/internal/config"
	"github.com/ORAITApps/document-uploader/internal/logger"
	"github.com/ORAITApps/document-uploader/internal/processor"
)

//go:embed .env
var env embed.FS

func main() {
	config.LoadEnv(env)

	logger := logger.New()
	logger.Info("Starting document processing...")

	tokenResp, err := auth.Authenticate()
	if err != nil {
		logger.Error("Authentication failed: %v", err)
		return
	}
	logger.Info("Successfully authenticated")

	err = processor.ProcessDocuments(tokenResp.AccessToken, "./documents")
	if err != nil {
		logger.Error("Document processing failed: %v", err)
		return
	}

	logger.Info("Document processing completed successfully")
}
