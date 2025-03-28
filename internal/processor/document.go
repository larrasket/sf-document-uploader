package processor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ORAITApps/document-uploader/internal/config"
	"github.com/ORAITApps/document-uploader/internal/filestructure"
	"github.com/ORAITApps/document-uploader/internal/gui"
	logging "github.com/ORAITApps/document-uploader/internal/logger"
	"github.com/ORAITApps/document-uploader/internal/models"
	"github.com/gabriel-vasile/mimetype"
)

type LookupError struct {
	EntityType string
	Path       string
	Details    string
}

type EntityHierarchy struct {
	Type    string
	Parent  string
	IDKey   string
	NameKey string
}

var hierarchyDefinition = []EntityHierarchy{
	{Type: "PROJECT", Parent: "", IDKey: "project", NameKey: "project"},
	{Type: "PHASE", Parent: "PROJECT", IDKey: "phase", NameKey: "phase"},
	{Type: "ZONE", Parent: "PHASE", IDKey: "zone", NameKey: "zone"},
	{Type: "BUILDING", Parent: "ZONE", IDKey: "building", NameKey: "building"},
	{Type: "UNIT", Parent: "BUILDING", IDKey: "unit", NameKey: "unit"},
	{Type: "DESIGN_TYPE", Parent: "PHASE", IDKey: "design_type", NameKey: "designType"},
}

func ProcessDocuments(accessToken, documentsDir string, app *gui.App) error {
	logger := logging.GetLogger()

	if documentsDir == "" {
		return fmt.Errorf("no documents directory selected")
	}

	if _, err := os.Stat(documentsDir); os.IsNotExist(err) {
		return fmt.Errorf("documents directory does not exist: %s", documentsDir)
	}

	app.SetStatus("Collecting documents...")
	documents, err := collectDocuments(documentsDir, logger)
	if err != nil {
		return fmt.Errorf("error collecting documents: %v", err)
	}
	app.SetProgress(0.2)

	app.SetStatus("Looking up entities...")
	if err := bulkLookupEntities(accessToken, documents, logger); err != nil {
		return fmt.Errorf("bulk lookup failed: %v", err)
	}
	app.SetProgress(0.4)

	app.SetStatus("Uploading content...")
	if err := bulkUploadContentVersions(accessToken, documentsDir, documents, logger, app); err != nil {
		logger.Error("Bulk content upload failed: %v", err)
		return fmt.Errorf("bulk content upload failed: %v", err)
	}
	app.SetProgress(0.8)

	app.SetStatus("Creating attachment records...")
	if err := bulkCreateAttachmentUploaders(accessToken, documents, logger); err != nil {
		logger.Error("Bulk attachment uploader creation failed: %v", err)
		return fmt.Errorf("bulk attachment uploader creation failed: %v", err)
	}
	app.SetProgress(1.0)

	logger.Info("Document processing completed successfully")
	return nil
}

func collectDocuments(documentsDir string, logger *logging.Logger) ([]models.DocumentInfo, error) {
	logger.Info("Reading documents from directory: %s", documentsDir)

	if _, err := os.Stat(documentsDir); os.IsNotExist(err) {
		logger.Error("Directory not found: %s", documentsDir)
		return nil, fmt.Errorf("directory not found: %s", documentsDir)
	}

	walker := filestructure.NewDocumentWalker(documentsDir)
	documents, err := walker.Walk()
	if err != nil {
		logger.Error("Failed to walk documents directory: %v", err)
		return nil, fmt.Errorf("error walking documents directory: %v", err)
	}

	if len(documents) == 0 {
		logger.Warning("No documents found in directory")
		return nil, fmt.Errorf("no documents found in directory: %s", documentsDir)
	}

	logger.Info("Collected %d documents for processing", len(documents))
	return documents, nil
}

func executeBulkLookup(accessToken string, bulkRequest models.BulkLookupRequest, logger *logging.Logger) (map[string]string, error) {
	jsonData, err := json.Marshal(bulkRequest)
	if err != nil {
		logger.Error("Failed to marshal bulk lookup request: %v", err)
		return nil, fmt.Errorf("error marshaling bulk lookup request: %v", err)
	}

	logger.Debug("Bulk lookup request payload: %s", string(jsonData))

	logger.Debug("Sending bulk lookup request to Salesforce")
	req, err := http.NewRequest("POST", config.BulkLookupURL, bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Error("Failed to create bulk lookup request: %v", err)
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Error("Failed to execute bulk lookup request: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	logger.Debug("Bulk lookup response: %s", string(respBody))

	var results map[string]string
	if err := json.Unmarshal(respBody, &results); err != nil {
		logger.Error("Failed to decode bulk lookup response: %v", err)
		return nil, err
	}

	return results, nil
}

func bulkLookupEntities(accessToken string, documents []models.DocumentInfo, logger *logging.Logger) error {
	logger.Info("Starting bulk entity lookup for %d documents", len(documents))

	pathsByLevel := make(map[string]map[string]models.DocumentInfo)
	foundIds := make(map[string]string)
	var lookupErrors []LookupError

	levels := []string{"PHASE", "ZONE", "BUILDING", "UNIT", "DESIGN_TYPE"}
	for _, level := range levels {
		pathsByLevel[level] = make(map[string]models.DocumentInfo)
	}

	for _, doc := range documents {
		switch doc.EntityType {
		case "UNIT":
			pathsByLevel["PHASE"][doc.NamePath["phase"]] = models.DocumentInfo{
				EntityType: "PHASE",
				NamePath: map[string]string{
					"project": doc.NamePath["project"],
					"phase":   doc.NamePath["phase"],
				},
			}
			pathsByLevel["ZONE"][doc.NamePath["zone"]] = models.DocumentInfo{
				EntityType: "ZONE",
				NamePath: map[string]string{
					"project": doc.NamePath["project"],
					"phase":   doc.NamePath["phase"],
					"zone":    doc.NamePath["zone"],
				},
			}
			pathsByLevel["BUILDING"][doc.NamePath["building"]] = models.DocumentInfo{
				EntityType: "BUILDING",
				NamePath: map[string]string{
					"project":  doc.NamePath["project"],
					"phase":    doc.NamePath["phase"],
					"zone":     doc.NamePath["zone"],
					"building": doc.NamePath["building"],
				},
			}
			pathsByLevel["UNIT"][doc.NamePath["unit"]] = doc

		case "BUILDING":
			pathsByLevel["PHASE"][doc.NamePath["phase"]] = models.DocumentInfo{
				EntityType: "PHASE",
				NamePath: map[string]string{
					"project": doc.NamePath["project"],
					"phase":   doc.NamePath["phase"],
				},
			}
			pathsByLevel["ZONE"][doc.NamePath["zone"]] = models.DocumentInfo{
				EntityType: "ZONE",
				NamePath: map[string]string{
					"project": doc.NamePath["project"],
					"phase":   doc.NamePath["phase"],
					"zone":    doc.NamePath["zone"],
				},
			}
			pathsByLevel["BUILDING"][doc.NamePath["building"]] = doc

		case "ZONE":
			pathsByLevel["PHASE"][doc.NamePath["phase"]] = models.DocumentInfo{
				EntityType: "PHASE",
				NamePath: map[string]string{
					"project": doc.NamePath["project"],
					"phase":   doc.NamePath["phase"],
				},
			}
			pathsByLevel["ZONE"][doc.NamePath["zone"]] = doc

		case "PHASE":
			pathsByLevel["PHASE"][doc.NamePath["phase"]] = doc

		case "DESIGN_TYPE":
			pathsByLevel["PHASE"][doc.NamePath["phase"]] = models.DocumentInfo{
				EntityType: "PHASE",
				NamePath: map[string]string{
					"project": doc.NamePath["project"],
					"phase":   doc.NamePath["phase"],
				},
			}
			pathsByLevel["DESIGN_TYPE"][doc.NamePath["designType"]] = doc
		}
	}

	for _, entityType := range levels {
		paths := pathsByLevel[entityType]
		if len(paths) == 0 {
			continue
		}

		logger.Info("Processing entity type: %s", entityType)

		var bulkRequest models.BulkLookupRequest
		for _, doc := range paths {
			parentKey := getParentKey(entityType, doc.NamePath)
			if parentKey != "" {
				if _, exists := foundIds[parentKey]; !exists {
					logger.Error("Parent not found: %s for %s", parentKey, generateFullPath(doc))
					continue
				}
			}
			bulkRequest.Lookups = append(bulkRequest.Lookups, models.EntityLookup{
				EntityType: entityType,
				NamePath:   doc.NamePath,
			})
		}

		if len(bulkRequest.Lookups) == 0 {
			continue
		}

		results, err := executeBulkLookup(accessToken, bulkRequest, logger)
		if err != nil {
			return err
		}

		for resultKey, resultId := range results {
			var resultLookup models.EntityLookup
			if err := json.Unmarshal([]byte(resultKey), &resultLookup); err != nil {
				continue
			}

			if strings.HasPrefix(resultId, "ERROR:") {
				lookupErrors = append(lookupErrors, LookupError{
					EntityType: entityType,
					Path:       generateFullPath(models.DocumentInfo{EntityType: entityType, NamePath: resultLookup.NamePath}),
					Details:    resultId,
				})
				continue
			}

			idKey := strings.ToLower(entityType)
			key := fmt.Sprintf("%s_%s", entityType, resultLookup.NamePath[idKey])
			foundIds[key] = resultId
			logger.Debug("Found ID for %s: %s", key, resultId)

			for i := range documents {
				if documents[i].EntityType == entityType && compareNamePaths(resultLookup.NamePath, documents[i].NamePath) {
					documents[i].SalesforceIds[idKey] = resultId
				}
			}
		}
	}

	if len(lookupErrors) > 0 {
		errorMsg := "The following entities were not found:\n"
		for _, err := range lookupErrors {
			errorMsg += fmt.Sprintf("- %s: %s\n  Details: %s\n",
				err.EntityType, err.Path, err.Details)
		}
		return fmt.Errorf(errorMsg)
	}

	logger.Info("Successfully completed bulk entity lookup")
	return nil
}

func getParentKey(entityType string, namePath map[string]string) string {
	switch entityType {
	case "PHASE":
		return ""
	case "ZONE":
		return fmt.Sprintf("PHASE_%s", namePath["phase"])
	case "BUILDING":
		return fmt.Sprintf("ZONE_%s", namePath["zone"])
	case "UNIT":
		return fmt.Sprintf("BUILDING_%s", namePath["building"])
	case "DESIGN_TYPE":
		return fmt.Sprintf("PHASE_%s", namePath["phase"])
	default:
		return ""
	}
}

func bulkUploadContentVersions(accessToken string, documentsDir string, documents []models.DocumentInfo, logger *logging.Logger, app *gui.App) error {
	const batchSize = 25

	var allRequests []map[string]any
	logger.Info("Preparing content version upload requests")

	totalBatches := (len(documents) + batchSize - 1) / batchSize
	currentBatch := 0
	progressStart := 0.4
	progressEnd := 0.8
	progressPerBatch := (progressEnd - progressStart) / float64(totalBatches)

	for i, doc := range documents {
		fullPath := filepath.Clean(filepath.Join(documentsDir, doc.RelativePath))

		logger.Debug("Reading file from path: %s", fullPath)
		fileBytes, err := os.ReadFile(fullPath)
		if err != nil {
			errMsg := fmt.Sprintf("error reading file %s: %v", fullPath, err)
			logger.Error(errMsg)
			return fmt.Errorf(errMsg)
		}

		request := map[string]any{
			"method":      "POST",
			"url":         "/services/data/v57.0/sobjects/ContentVersion",
			"referenceId": fmt.Sprintf("ref%d", i),
			"body": map[string]any{
				"Title":                  filepath.Base(doc.FilePath),
				"PathOnClient":           filepath.Base(doc.FilePath),
				"VersionData":            base64.StdEncoding.EncodeToString(fileBytes),
				"FirstPublishLocationId": doc.SalesforceIds[strings.ToLower(doc.EntityType)],
			},
		}
		allRequests = append(allRequests, request)
		logger.Debug("Prepared request for file: %s", fullPath)
	}

	for i := 0; i < len(allRequests); i += batchSize {
		currentBatch++
		end := min(i+batchSize, len(allRequests))
		logger.Info("Processing batch %d of %d (%d files)", currentBatch, totalBatches, end-i)

		progress := progressStart + (float64(currentBatch) * progressPerBatch)
		app.SetProgress(progress)

		logger.Info("Processing batch %d of %d (%d files)", currentBatch, totalBatches, end-i)

		batchRequests := allRequests[i:end]
		compositeRequest := map[string]any{
			"allOrNone":        true,
			"compositeRequest": batchRequests,
		}

		jsonBody, err := json.Marshal(compositeRequest)
		if err != nil {
			logger.Error("Failed to marshal composite request: %v", err)
			return fmt.Errorf("error marshaling request: %v", err)
		}

		req, err := http.NewRequest("POST",
			config.SFInstanceURL+"/services/data/v57.0/composite",
			bytes.NewBuffer(jsonBody))
		if err != nil {
			logger.Error("Failed to create composite request: %v", err)
			return fmt.Errorf("error creating composite request: %v", err)
		}

		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		logger.Debug("Sending batch request to Salesforce")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logger.Error("Failed to execute composite request: %v", err)
			return fmt.Errorf("composite request failed: %v", err)
		}
		defer resp.Body.Close()

		var compositeResponse struct {
			CompositeResponse []struct {
				Body           any    `json:"body"`
				HttpStatusCode int    `json:"httpStatusCode"`
				ReferenceId    string `json:"referenceId"`
			} `json:"compositeResponse"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&compositeResponse); err != nil {
			logger.Error("Failed to decode composite response: %v", err)
			return fmt.Errorf("error decoding composite response: %v", err)
		}

		for _, result := range compositeResponse.CompositeResponse {
			if result.HttpStatusCode != 201 {
				errMsg := fmt.Sprintf("failed to create ContentVersion for reference %s: status %d",
					result.ReferenceId, result.HttpStatusCode)
				logger.Error(errMsg)
				return fmt.Errorf(errMsg)
			}

			if successBody, ok := result.Body.(map[string]any); ok {
				refIndex, _ := strconv.Atoi(strings.TrimPrefix(result.ReferenceId, "ref"))
				if refIndex < len(documents) {
					versionId := successBody["id"].(string)
					documents[refIndex].SalesforceIds["contentVersionId"] = versionId
					logger.Debug("Created ContentVersion with ID: %s for file: %s",
						versionId, documents[refIndex].FilePath)
				}
			}
		}
	}

	logger.Info("Successfully completed content version uploads")

	if err := createContentDistributions(accessToken, documents, logger); err != nil {
		logger.Error("Failed to create content distributions: %v", err)
		return fmt.Errorf("failed to create content distributions: %v", err)
	}

	return nil
}

func bulkCreateAttachmentUploaders(accessToken string, documents []models.DocumentInfo, logger *logging.Logger) error {
	const batchSize = 25
	logger.Info("Starting attachment uploader creation")

	var allRequests []map[string]any
	for i, doc := range documents {
		logger.Debug("Processing document: %s", doc.FilePath)
		logger.Debug("ContentDocumentId: %s", doc.ContentDocumentId)
		logger.Debug("EntityType: %s", doc.EntityType)
		logger.Debug("Available SalesforceIds: %+v", doc.SalesforceIds)

		if doc.ContentDocumentId == "" {
			errMsg := fmt.Sprintf("Missing ContentDocumentId for document: %s", doc.FilePath)
			logger.Error(errMsg)
			continue
		}

		var entityId string
		switch doc.EntityType {
		case "UNIT":
			entityId = doc.SalesforceIds["unit"]
		case "PHASE":
			entityId = doc.SalesforceIds["phase"]
		case "ZONE":
			entityId = doc.SalesforceIds["zone"]
		case "BUILDING":
			entityId = doc.SalesforceIds["building"]
		case "DESIGN_TYPE":
			entityId = doc.SalesforceIds["design_type"]
		}

		if entityId == "" {
			errMsg := fmt.Sprintf("Missing %s ID for document: %s", doc.EntityType, doc.FilePath)
			logger.Error(errMsg)
			continue
		}

		displayValue := generateDisplayValue(doc)

		distributionUrl := doc.SalesforceIds["distributionUrl"]
		if distributionUrl == "" {
			logger.Warning("No distribution URL found for document: %s", doc.FilePath)
			distributionUrl = fmt.Sprintf("/lightning/r/ContentDocument/%s/view", doc.ContentDocumentId)
		}

		record := map[string]any{
			"Name":                    entityId,
			"Attachment_Type__c":      doc.DocumentType,
			"Content_Type__c":         getContentType(doc.FilePath),
			"ContentDocumentId__c":    doc.ContentDocumentId,
			"Attachment_Url__c":       distributionUrl,
			"Display_Value__c":        displayValue,
			"Display_Value_Arabic__c": displayValue,
		}

		switch doc.EntityType {
		case "UNIT":
			record["Unit__c"] = entityId
		case "PHASE":
			record["Phase__c"] = entityId
		case "ZONE":
			record["Zone__c"] = entityId
		case "BUILDING":
			record["Building__c"] = entityId
		case "DESIGN_TYPE":
			record["Design_Type__c"] = entityId
		}

		logger.Debug("Creating attachment uploader record for: %s", doc.FilePath)

		request := map[string]any{
			"method":      "POST",
			"url":         "/services/data/v57.0/sobjects/Attachments_Uploader__c",
			"referenceId": fmt.Sprintf("attRef%d", i),
			"body":        record,
		}
		allRequests = append(allRequests, request)
	}

	if len(allRequests) == 0 {
		errMsg := "no valid records to create"
		logger.Error(errMsg)
		return fmt.Errorf(errMsg)
	}

	totalBatches := (len(allRequests) + batchSize - 1) / batchSize
	currentBatch := 0

	for i := 0; i < len(allRequests); i += batchSize {
		currentBatch++
		end := min(i+batchSize, len(allRequests))
		logger.Info("Processing batch %d of %d (%d records)", currentBatch, totalBatches, end-i)

		batchRequests := allRequests[i:end]
		compositeRequest := map[string]any{
			"allOrNone":        true,
			"compositeRequest": batchRequests,
		}

		jsonBody, _ := json.Marshal(compositeRequest)
		logger.Debug("Sending attachment uploader request")

		req, err := http.NewRequest("POST",
			config.SFInstanceURL+"/services/data/v57.0/composite",
			bytes.NewBuffer(jsonBody))
		if err != nil {
			logger.Error("Failed to create composite request: %v", err)
			return fmt.Errorf("error creating composite request: %v", err)
		}

		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logger.Error("Failed to execute composite request: %v", err)
			return fmt.Errorf("composite request failed: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		logger.Debug("Received attachment uploader response: %s", string(body))

		var compositeResponse struct {
			CompositeResponse []struct {
				Body           any    `json:"body"`
				HttpStatusCode int    `json:"httpStatusCode"`
				ReferenceId    string `json:"referenceId"`
			} `json:"compositeResponse"`
		}

		if err := json.Unmarshal(body, &compositeResponse); err != nil {
			logger.Error("Failed to decode composite response: %v", err)
			return fmt.Errorf("error decoding composite response: %v", err)
		}

		for _, result := range compositeResponse.CompositeResponse {
			if result.HttpStatusCode != 201 {
				if errArray, ok := result.Body.([]any); ok && len(errArray) > 0 {
					if errMap, ok := errArray[0].(map[string]any); ok {
						errMsg := fmt.Sprintf("failed to create Attachments_Uploader__c: %v - %v",
							errMap["errorCode"], errMap["message"])
						logger.Error(errMsg)
						return fmt.Errorf(errMsg)
					}
				}
				errMsg := fmt.Sprintf("failed to create Attachments_Uploader__c for reference %s: status %d",
					result.ReferenceId, result.HttpStatusCode)
				logger.Error(errMsg)
				return fmt.Errorf(errMsg)
			}

			if successBody, ok := result.Body.(map[string]any); ok {
				logger.Debug("Created Attachments_Uploader__c with ID: %v", successBody["id"])
			}
		}
	}

	logger.Info("Successfully created all attachment uploaders")
	return nil
}

func compareNamePaths(path1, path2 map[string]string) bool {
	if len(path1) != len(path2) {
		return false
	}
	for k, v1 := range path1 {
		if v2, ok := path2[k]; !ok || v1 != v2 {
			return false
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func getContentType(filename string) string {
	mime, err := mimetype.DetectFile(filename)
	if err != nil {
		return config.ContentTypeImage
	}

	mainType := strings.Split(mime.String(), "/")[0]

	switch {
	case mainType == "image":
		return config.ContentTypeImage
	case mime.String() == "application/pdf":
		return config.ContentTypePDF
	case mainType == "video":
		return config.ContentTypeVideo
	default:
		return config.ContentTypeImage
	}
}

func generateDisplayValue(doc models.DocumentInfo) string {
	switch doc.EntityType {
	case "UNIT":
		return fmt.Sprintf("%s for Unit %s of Building %s in Phase %s of %s",
			doc.DocumentType,
			doc.NamePath["unit"],
			doc.NamePath["building"],
			doc.NamePath["phase"],
			doc.NamePath["project"])

	case "BUILDING":
		return fmt.Sprintf("%s for Building %s in Zone %s of Phase %s - %s",
			doc.DocumentType,
			doc.NamePath["building"],
			doc.NamePath["zone"],
			doc.NamePath["phase"],
			doc.NamePath["project"])

	case "ZONE":
		return fmt.Sprintf("%s for Zone %s in Phase %s of %s",
			doc.DocumentType,
			doc.NamePath["zone"],
			doc.NamePath["phase"],
			doc.NamePath["project"])

	case "PHASE":
		return fmt.Sprintf("%s for Phase %s of %s",
			doc.DocumentType,
			doc.NamePath["phase"],
			doc.NamePath["project"])

	case "DESIGN_TYPE":
		return fmt.Sprintf("%s for Design Type %s in Phase %s of %s",
			doc.DocumentType,
			doc.NamePath["designType"],
			doc.NamePath["phase"],
			doc.NamePath["project"])

	default:
		return strings.TrimSuffix(filepath.Base(doc.FilePath), filepath.Ext(doc.FilePath))
	}
}

func generateFullPath(doc models.DocumentInfo) string {
	switch doc.EntityType {
	case "UNIT":
		return fmt.Sprintf("%s/Phase %s/Zone %s/Building %s/Unit %s",
			doc.NamePath["project"],
			doc.NamePath["phase"],
			doc.NamePath["zone"],
			doc.NamePath["building"],
			doc.NamePath["unit"])
	case "BUILDING":
		return fmt.Sprintf("%s/Phase %s/Zone %s/Building %s",
			doc.NamePath["project"],
			doc.NamePath["phase"],
			doc.NamePath["zone"],
			doc.NamePath["building"])
	case "ZONE":
		return fmt.Sprintf("%s/Phase %s/Zone %s",
			doc.NamePath["project"],
			doc.NamePath["phase"],
			doc.NamePath["zone"])
	case "PHASE":
		return fmt.Sprintf("%s/Phase %s",
			doc.NamePath["project"],
			doc.NamePath["phase"])
	case "DESIGN_TYPE":
		return fmt.Sprintf("%s/Phase %s/Design Type %s",
			doc.NamePath["project"],
			doc.NamePath["phase"],
			doc.NamePath["designType"])
	default:
		return doc.FilePath
	}
}

func createContentDistributions(accessToken string, documents []models.DocumentInfo, logger *logging.Logger) error {
	logger.Info("Creating content distributions")

	var requests []map[string]any
	for i, doc := range documents {
		if doc.ContentDocumentId == "" {
			continue
		}

		request := map[string]any{
			"method":      "POST",
			"url":         "/services/data/v57.0/sobjects/ContentDistribution",
			"referenceId": fmt.Sprintf("distRef%d", i),
			"body": map[string]any{
				"ContentVersionId":                 doc.SalesforceIds["contentVersionId"],
				"Name":                             filepath.Base(doc.FilePath),
				"PreferencesAllowViewInBrowser":    true,
				"PreferencesLinkLatestVersion":     true,
				"PreferencesNotifyOnVisit":         false,
				"PreferencesPasswordRequired":      false,
				"PreferencesAllowOriginalDownload": true,
			},
		}
		requests = append(requests, request)
	}

	if len(requests) == 0 {
		return fmt.Errorf("no documents to create distributions for")
	}

	compositeRequest := map[string]any{
		"allOrNone":        true,
		"compositeRequest": requests,
	}

	jsonBody, _ := json.Marshal(compositeRequest)
	logger.Debug("Content distribution request: %s", string(jsonBody))

	req, err := http.NewRequest("POST",
		config.SFInstanceURL+"/services/data/v57.0/composite",
		bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("error creating distribution request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("distribution request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	logger.Debug("Content distribution response: %s", string(respBody))

	var result struct {
		CompositeResponse []struct {
			Body struct {
				Id                    string `json:"id"`
				ContentDistributionId string `json:"contentDistributionId"`
			} `json:"body"`
			HttpStatusCode int    `json:"httpStatusCode"`
			ReferenceId    string `json:"referenceId"`
		} `json:"compositeResponse"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("error decoding distribution response: %v", err)
	}

	for _, response := range result.CompositeResponse {
		if response.HttpStatusCode != 201 {
			continue
		}

		distributionUrl := fmt.Sprintf("%s/services/data/v57.0/sobjects/ContentDistribution/%s",
			config.SFInstanceURL, response.Body.Id)

		req, err := http.NewRequest("GET", distributionUrl, nil)
		if err != nil {
			logger.Error("Failed to create distribution details request: %v", err)
			continue
		}

		req.Header.Set("Authorization", "Bearer "+accessToken)

		detailsResp, err := http.DefaultClient.Do(req)
		if err != nil {
			logger.Error("Failed to get distribution details: %v", err)
			continue
		}
		defer detailsResp.Body.Close()

		var distributionDetails struct {
			DistributionPublicUrl string `json:"DistributionPublicUrl"`
			ContentDownloadUrl    string `json:"ContentDownloadUrl"`
		}

		if err := json.NewDecoder(detailsResp.Body).Decode(&distributionDetails); err != nil {
			logger.Error("Failed to decode distribution details: %v", err)
			continue
		}

		refIndex, _ := strconv.Atoi(strings.TrimPrefix(response.ReferenceId, "distRef"))
		if refIndex < len(documents) {
			documents[refIndex].SalesforceIds["distributionUrl"] = distributionDetails.ContentDownloadUrl
			logger.Debug("Set distribution URL for %s: %s",
				documents[refIndex].FilePath,
				distributionDetails.ContentDownloadUrl)
		}
	}

	for _, doc := range documents {
		if doc.SalesforceIds["distributionUrl"] == "" {
			logger.Warning("Document %s does not have a distribution URL", doc.FilePath)
		}
	}

	return nil
}
