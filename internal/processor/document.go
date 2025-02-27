package processor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ORAITApps/document-uploader/internal/config"
	"github.com/ORAITApps/document-uploader/internal/logger"
	"github.com/ORAITApps/document-uploader/internal/models"
	"github.com/gabriel-vasile/mimetype"
)

func collectDocuments(documentsDir string) ([]models.DocumentInfo, error) {
	logger := logger.New()
	files, err := os.ReadDir(documentsDir)
	if err != nil {
		return nil, fmt.Errorf("error reading documents directory: %v", err)
	}

	var documents []models.DocumentInfo
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		logger.Info("Reading file: %s", file.Name())

		docInfo, err := ParseFileName(file.Name())
		if err != nil {
			return nil, fmt.Errorf("error parsing filename %s: %v", file.Name(), err)
		}

		documents = append(documents, *docInfo)
	}

	logger.Info("Collected %d documents for processing", len(documents))
	return documents, nil
}

func ProcessDocuments(accessToken, documentsDir string) error {
	documents, err := collectDocuments(documentsDir)
	if err != nil {
		return fmt.Errorf("error collecting documents: %v", err)
	}

	if err := bulkLookupEntities(accessToken, documents); err != nil {
		return fmt.Errorf("bulk lookup failed: %v", err)
	}

	if err := bulkUploadContentVersions(accessToken, documents); err != nil {
		return fmt.Errorf("bulk content upload failed: %v", err)
	}

	if err := bulkCreateAttachmentUploaders(accessToken, documents); err != nil {
		return fmt.Errorf("bulk attachment uploader creation failed: %v", err)
	}

	return nil
}

func bulkLookupEntities(accessToken string, documents []models.DocumentInfo) error {

	var bulkRequest models.BulkLookupRequest
	for _, doc := range documents {
		bulkRequest.Lookups = append(bulkRequest.Lookups, models.EntityLookup{
			EntityType: doc.EntityType,
			NamePath:   doc.NamePath,
		})
	}

	jsonData, err := json.Marshal(bulkRequest)
	if err != nil {
		return fmt.Errorf("error marshaling bulk lookup request: %v", err)
	}

	req, err := http.NewRequest("POST", config.BulkLookupURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var results map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return err
	}

	for i := range documents {
		found := false
		for resultKey, resultId := range results {
			var resultLookup models.EntityLookup
			if err := json.Unmarshal([]byte(resultKey), &resultLookup); err != nil {
				continue
			}

			if resultLookup.EntityType == documents[i].EntityType &&
				compareNamePaths(resultLookup.NamePath, documents[i].NamePath) {
				if strings.HasPrefix(resultId, "ERROR:") {
					return fmt.Errorf("lookup failed for %s: %s", documents[i].FilePath, resultId)
				}
				documents[i].SalesforceIds[strings.ToLower(documents[i].EntityType)] = resultId
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("no lookup result found for %s", documents[i].FilePath)
		}
	}

	return nil
}

func bulkUploadContentVersions(accessToken string, documents []models.DocumentInfo) error {
	logger := logger.New()
	const batchSize = 25

	var allRequests []map[string]any
	for i, doc := range documents {
		fileBytes, err := os.ReadFile(filepath.Join("./documents", doc.FilePath))
		if err != nil {
			return fmt.Errorf("error reading file %s: %v", doc.FilePath, err)
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
		logger.Info("Prepared request for file: %s", doc.FilePath)
	}

	for i := 0; i < len(allRequests); i += batchSize {
		end := min(i+batchSize, len(allRequests))

		batchRequests := allRequests[i:end]
		compositeRequest := map[string]any{
			"allOrNone":        true,
			"compositeRequest": batchRequests,
		}

		logger.Info("Sending ContentVersion request for %d files", len(batchRequests))

		jsonBody, err := json.Marshal(compositeRequest)
		if err != nil {
			return fmt.Errorf("error marshaling request: %v", err)
		}

		req, err := http.NewRequest("POST",
			config.SFInstanceURL+"/services/data/v57.0/composite",
			bytes.NewBuffer(jsonBody))
		if err != nil {
			return fmt.Errorf("error creating composite request: %v", err)
		}

		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
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
			return fmt.Errorf("error decoding composite response: %v", err)
		}

		for _, result := range compositeResponse.CompositeResponse {
			if result.HttpStatusCode != 201 {
				return fmt.Errorf("failed to create ContentVersion for reference %s: status %d",
					result.ReferenceId, result.HttpStatusCode)
			}

			if successBody, ok := result.Body.(map[string]any); ok {
				refIndex, _ := strconv.Atoi(strings.TrimPrefix(result.ReferenceId, "ref"))
				if refIndex < len(documents) {
					versionId := successBody["id"].(string)
					documents[refIndex].SalesforceIds["contentVersionId"] = versionId
					logger.Info("Created ContentVersion with ID: %s for file: %s",
						versionId, documents[refIndex].FilePath)
				}
			}
		}
	}

	var versionIds []string
	for _, doc := range documents {
		if id, ok := doc.SalesforceIds["contentVersionId"]; ok {
			versionIds = append(versionIds, "'"+id+"'")
		}
	}

	if len(versionIds) > 0 {
		soql := fmt.Sprintf(
			"SELECT Id, ContentDocumentId FROM ContentVersion WHERE Id IN (%s)",
			strings.Join(versionIds, ","),
		)

		queryURL := fmt.Sprintf("%s/services/data/v57.0/query?q=%s",
			config.SFInstanceURL,
			url.QueryEscape(soql))

		req, err := http.NewRequest("GET", queryURL, nil)
		if err != nil {
			return fmt.Errorf("error creating query request: %v", err)
		}

		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("query request failed: %v", err)
		}
		defer resp.Body.Close()

		var queryResult struct {
			Records []struct {
				Id                string `json:"Id"`
				ContentDocumentId string `json:"ContentDocumentId"`
			} `json:"records"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&queryResult); err != nil {
			return fmt.Errorf("error decoding query response: %v", err)
		}

		cdIdMap := make(map[string]string)
		for _, record := range queryResult.Records {
			cdIdMap[record.Id] = record.ContentDocumentId
			logger.Info("Mapping ContentVersion %s to ContentDocument %s",
				record.Id, record.ContentDocumentId)
		}

		logger.Info("ContentDocument mapping: %+v", cdIdMap)

		for i := range documents {
			if versionId, ok := documents[i].SalesforceIds["contentVersionId"]; ok {
				if contentDocId, ok := cdIdMap[versionId]; ok {
					documents[i].ContentDocumentId = contentDocId
					logger.Info("Set ContentDocumentId %s for file %s (Version ID: %s)",
						contentDocId, documents[i].FilePath, versionId)
				}
			}
		}
	}

	for i := range documents {
		logger.Info("Verifying document %s - ContentDocumentId: %s",
			documents[i].FilePath, documents[i].ContentDocumentId)
		if documents[i].ContentDocumentId == "" {
			logger.Error("ContentDocumentId not set for file: %s (VersionId: %s)",
				documents[i].FilePath, documents[i].SalesforceIds["contentVersionId"])
			return fmt.Errorf("failed to get ContentDocumentId for file: %s", documents[i].FilePath)
		}
	}

	return nil
}

func bulkCreateAttachmentUploaders(accessToken string, documents []models.DocumentInfo) error {
	logger := logger.New()
	const batchSize = 25

	var allRequests []map[string]any
	for i, doc := range documents {
		logger.Info("Processing document: %s", doc.FilePath)
		logger.Info("ContentDocumentId: %s", doc.ContentDocumentId)
		logger.Info("EntityType: %s", doc.EntityType)
		logger.Info("Available SalesforceIds: %+v", doc.SalesforceIds)

		if doc.ContentDocumentId == "" {
			logger.Error("Missing ContentDocumentId for document: %s", doc.FilePath)
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
			logger.Error("Missing %s ID for document: %s", doc.EntityType, doc.FilePath)
			continue
		}

		displayValue := generateDisplayValue(doc)

		record := map[string]any{
			"Name":                    entityId,
			"Attachment_Type__c":      doc.DocumentType,
			"Content_Type__c":         getContentType(doc.FilePath),
			"ContentDocumentId__c":    doc.ContentDocumentId,
			"Attachment_Url__c":       fmt.Sprintf("/lightning/r/ContentDocument/%s/view", doc.ContentDocumentId),
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

		logger.Info("Creating attachment uploader record: %+v", record)

		request := map[string]any{
			"method":      "POST",
			"url":         "/services/data/v57.0/sobjects/Attachments_Uploader__c",
			"referenceId": fmt.Sprintf("attRef%d", i),
			"body":        record,
		}
		allRequests = append(allRequests, request)
	}

	if len(allRequests) == 0 {
		return fmt.Errorf("no valid records to create")
	}

	for i := 0; i < len(allRequests); i += batchSize {
		end := min(i+batchSize, len(allRequests))

		batchRequests := allRequests[i:end]
		compositeRequest := map[string]any{
			"allOrNone":        true,
			"compositeRequest": batchRequests,
		}

		jsonBody, _ := json.Marshal(compositeRequest)
		logger.Info("Sending attachment uploader request: %s", string(jsonBody))

		req, err := http.NewRequest("POST",
			config.SFInstanceURL+"/services/data/v57.0/composite",
			bytes.NewBuffer(jsonBody))
		if err != nil {
			return fmt.Errorf("error creating composite request: %v", err)
		}

		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("composite request failed: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		logger.Info("Received attachment uploader response: %s", string(body))

		var compositeResponse struct {
			CompositeResponse []struct {
				Body           any    `json:"body"`
				HttpStatusCode int    `json:"httpStatusCode"`
				ReferenceId    string `json:"referenceId"`
			} `json:"compositeResponse"`
		}

		if err := json.Unmarshal(body, &compositeResponse); err != nil {
			return fmt.Errorf("error decoding composite response: %v", err)
		}

		for _, result := range compositeResponse.CompositeResponse {
			if result.HttpStatusCode != 201 {
				if errArray, ok := result.Body.([]any); ok && len(errArray) > 0 {
					if errMap, ok := errArray[0].(map[string]any); ok {
						return fmt.Errorf("failed to create Attachments_Uploader__c: %v - %v",
							errMap["errorCode"], errMap["message"])
					}
				}
				return fmt.Errorf("failed to create Attachments_Uploader__c for reference %s: status %d",
					result.ReferenceId, result.HttpStatusCode)
			}

			if successBody, ok := result.Body.(map[string]any); ok {
				logger.Info("Created Attachments_Uploader__c with ID: %v", successBody["id"])
			}
		}
	}

	logger.Info("Successfully created %d attachment uploaders", len(allRequests))
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
		return config.ContentTypeImage // default to image if detection fails
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
