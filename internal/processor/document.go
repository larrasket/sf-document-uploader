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
	"github.com/ORAITApps/document-uploader/internal/gui"
	"github.com/ORAITApps/document-uploader/internal/models"
	"github.com/gabriel-vasile/mimetype"
)

func collectDocuments(documentsDir string, gui *gui.GUI) ([]models.DocumentInfo, error) {
	gui.Log("Reading documents from directory: %s", documentsDir)

	files, err := os.ReadDir(documentsDir)
	if err != nil {
		return nil, fmt.Errorf("error reading documents directory: %v", err)
	}

	var documents []models.DocumentInfo
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		gui.Log("Reading file: %s", file.Name())

		docInfo, err := ParseFileName(file.Name())
		if err != nil {
			return nil, fmt.Errorf("error parsing filename %s: %v", file.Name(), err)
		}

		documents = append(documents, *docInfo)
	}

	gui.Log("Collected %d documents for processing", len(documents))
	return documents, nil
}

func ProcessDocuments(accessToken, documentsDir string, gui *gui.GUI) error {
	gui.SetStatus("Collecting documents...")
	documents, err := collectDocuments(documentsDir, gui)
	if err != nil {
		return fmt.Errorf("error collecting documents: %v", err)
	}
	gui.SetProgress(0.2)

	gui.SetStatus("Looking up entities...")
	if err := bulkLookupEntities(accessToken, documents, gui); err != nil {
		return fmt.Errorf("bulk lookup failed: %v", err)
	}
	gui.SetProgress(0.4)

	gui.SetStatus("Uploading content...")
	if err := bulkUploadContentVersions(accessToken, documents, gui); err != nil {
		return fmt.Errorf("bulk content upload failed: %v", err)
	}
	gui.SetProgress(0.8)

	gui.SetStatus("Creating attachment records...")
	if err := bulkCreateAttachmentUploaders(accessToken, documents, gui); err != nil {
		return fmt.Errorf("bulk attachment uploader creation failed: %v", err)
	}
	gui.SetProgress(1.0)

	return nil
}

func bulkLookupEntities(accessToken string, documents []models.DocumentInfo, gui *gui.GUI) error {
	gui.Log("Starting bulk entity lookup for %d documents", len(documents))

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

	gui.Log("Sending bulk lookup request to Salesforce")
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

	gui.Log("Processing lookup results")
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
					errMsg := fmt.Sprintf("lookup failed for %s: %s", documents[i].FilePath, resultId)
					gui.Log("Error: %s", errMsg)
					return fmt.Errorf(errMsg)
				}
				documents[i].SalesforceIds[strings.ToLower(documents[i].EntityType)] = resultId
				gui.Log("Found ID for %s: %s", documents[i].FilePath, resultId)
				found = true
				break
			}
		}

		if !found {
			errMsg := fmt.Sprintf("no lookup result found for %s", documents[i].FilePath)
			gui.Log("Error: %s", errMsg)
			return fmt.Errorf(errMsg)
		}
	}

	return nil
}

func bulkUploadContentVersions(accessToken string, documents []models.DocumentInfo, gui *gui.GUI) error {
	const batchSize = 25

	var allRequests []map[string]any
	gui.Log("Preparing content version upload requests")
	for i, doc := range documents {
		fileBytes, err := os.ReadFile(filepath.Join("./documents", doc.FilePath))
		if err != nil {
			errMsg := fmt.Sprintf("error reading file %s: %v", doc.FilePath, err)
			gui.Log("Error: %s", errMsg)
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
		gui.Log("Prepared request for file: %s", doc.FilePath)
	}

	totalBatches := (len(allRequests) + batchSize - 1) / batchSize
	currentBatch := 0

	for i := 0; i < len(allRequests); i += batchSize {
		currentBatch++
		end := min(i+batchSize, len(allRequests))
		gui.Log("Processing batch %d of %d (%d files)", currentBatch, totalBatches, end-i)

		batchRequests := allRequests[i:end]
		compositeRequest := map[string]any{
			"allOrNone":        true,
			"compositeRequest": batchRequests,
		}

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

		gui.Log("Sending batch request to Salesforce")
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
				errMsg := fmt.Sprintf("failed to create ContentVersion for reference %s: status %d",
					result.ReferenceId, result.HttpStatusCode)
				gui.Log("Error: %s", errMsg)
				return fmt.Errorf(errMsg)
			}

			if successBody, ok := result.Body.(map[string]any); ok {
				refIndex, _ := strconv.Atoi(strings.TrimPrefix(result.ReferenceId, "ref"))
				if refIndex < len(documents) {
					versionId := successBody["id"].(string)
					documents[refIndex].SalesforceIds["contentVersionId"] = versionId
					gui.Log("Created ContentVersion with ID: %s for file: %s",
						versionId, documents[refIndex].FilePath)
				}
			}
		}
	}

	gui.Log("Retrieving ContentDocument IDs")
	var versionIds []string
	for _, doc := range documents {
		if id, ok := doc.SalesforceIds["contentVersionId"]; ok {
			versionIds = append(versionIds, "'"+id+"'")
		}
	}

	if len(versionIds) > 0 {
		soql := fmt.Sprintf(
			"SELECT Id, ContentDocumentId FROM ContentVersion WHERE Id IN (%s)",
			strings.Join(versionIds, ","))

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
			gui.Log("Mapping ContentVersion %s to ContentDocument %s",
				record.Id, record.ContentDocumentId)
		}

		for i := range documents {
			if versionId, ok := documents[i].SalesforceIds["contentVersionId"]; ok {
				if contentDocId, ok := cdIdMap[versionId]; ok {
					documents[i].ContentDocumentId = contentDocId
					gui.Log("Set ContentDocumentId %s for file %s",
						contentDocId, documents[i].FilePath)
				}
			}
		}
	}

	gui.Log("Verifying ContentDocument IDs")
	for i := range documents {
		if documents[i].ContentDocumentId == "" {
			errMsg := fmt.Sprintf("ContentDocumentId not set for file: %s (VersionId: %s)",
				documents[i].FilePath, documents[i].SalesforceIds["contentVersionId"])
			gui.Log("Error: %s", errMsg)
			return fmt.Errorf("failed to get ContentDocumentId for file: %s", documents[i].FilePath)
		}
	}

	return nil
}

func bulkCreateAttachmentUploaders(accessToken string, documents []models.DocumentInfo, gui *gui.GUI) error {
	const batchSize = 25
	gui.Log("Starting attachment uploader creation")

	var allRequests []map[string]any
	for i, doc := range documents {
		gui.Log("Processing document: %s", doc.FilePath)
		gui.Log("ContentDocumentId: %s", doc.ContentDocumentId)
		gui.Log("EntityType: %s", doc.EntityType)
		gui.Log("Available SalesforceIds: %+v", doc.SalesforceIds)

		if doc.ContentDocumentId == "" {
			errMsg := fmt.Sprintf("Missing ContentDocumentId for document: %s", doc.FilePath)
			gui.Log("Error: %s", errMsg)
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
			gui.Log("Error: %s", errMsg)
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

		gui.Log("Creating attachment uploader record for: %s", doc.FilePath)

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

	totalBatches := (len(allRequests) + batchSize - 1) / batchSize
	currentBatch := 0

	for i := 0; i < len(allRequests); i += batchSize {
		currentBatch++
		end := min(i+batchSize, len(allRequests))
		gui.Log("Processing batch %d of %d (%d records)", currentBatch, totalBatches, end-i)

		batchRequests := allRequests[i:end]
		compositeRequest := map[string]any{
			"allOrNone":        true,
			"compositeRequest": batchRequests,
		}

		jsonBody, _ := json.Marshal(compositeRequest)
		gui.Log("Sending attachment uploader request")

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
		gui.Log("Received attachment uploader response: %s", string(body))

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
						errMsg := fmt.Sprintf("failed to create Attachments_Uploader__c: %v - %v",
							errMap["errorCode"], errMap["message"])
						gui.Log("Error: %s", errMsg)
						return fmt.Errorf(errMsg)
					}
				}
				errMsg := fmt.Sprintf("failed to create Attachments_Uploader__c for reference %s: status %d",
					result.ReferenceId, result.HttpStatusCode)
				gui.Log("Error: %s", errMsg)
				return fmt.Errorf(errMsg)
			}

			if successBody, ok := result.Body.(map[string]any); ok {
				gui.Log("Created Attachments_Uploader__c with ID: %v", successBody["id"])
			}
		}
	}

	gui.Log("Successfully created all attachment uploaders")
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
