package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/pkg/browser"
)

const (
	sfInstanceURL = "https://ora-egypt--mas.sandbox.my.salesforce-setup.com"
	authURL       = sfInstanceURL + "/services/oauth2/authorize"
	tokenURL      = sfInstanceURL + "/services/oauth2/token"
	bulkLookupURL = sfInstanceURL + "/services/apexrest/admin/bulk-lookup"

	clientID    = "3MVG9CmdhfW8tOGCdf5CPNqN58A4eHJFXV_qMITEB_XMGHAUPHLFTWOejtNXvvOjaLCl0s41X6OPPB.U._3xO"
	redirectURI = "http://localhost:8080/oauth/callback"

	DocTypeBuildingLocation = "Building Location"
	DocTypeFinish           = "Finish"
	DocTypeFloorPlan        = "Floor Plan"
	DocTypeGallery          = "Gallery"
	DocTypeProjectPlan      = "Project Plan"
	DocTypeUnitPlan         = "Unit Plan"
	DocTypeGeneric          = "Generic Document"

	ContentTypeImage = "Image"
	ContentTypePDF   = "PDF"
	ContentTypeVideo = "Video"
)

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type BulkLookupRequest struct {
	Lookups []EntityLookup `json:"lookups"`
}

type EntityLookup struct {
	EntityType string            `json:"entityType"`
	NamePath   map[string]string `json:"namePath"`
}

type DocumentInfo struct {
	FilePath          string
	EntityType        string
	NamePath          map[string]string
	DocumentType      string
	ContentType       string
	SalesforceIds     map[string]string
	ContentDocumentId string
}

type AttachmentUploader struct {
	AttachmentType    string `json:"Attachment_Type__c"`
	AttachmentUrl     string `json:"Attachment_Url__c"`
	Name              string `json:"Name"`
	BuildingId        string `json:"Building__c,omitempty"`
	ContentType       string `json:"Content_Type__c"`
	ContentDocumentId string `json:"ContentDocumentId__c"`
	DesignTypeId      string `json:"Design_Type__c,omitempty"`
	PhaseId           string `json:"Phase__c,omitempty"`
	ProjectId         string `json:"Project__c,omitempty"`
	UnitId            string `json:"Unit__c,omitempty"`
	ZoneId            string `json:"Zone__c,omitempty"`
}

func main() {
	logger := NewLogger()
	logger.Info("Starting document processing...")

	tokenResp, err := authenticate()
	if err != nil {
		logger.Error("Authentication failed: %v", err)
		return
	}
	logger.Info("Successfully authenticated")

	err = processDocuments(tokenResp.AccessToken, "./documents")
	if err != nil {
		logger.Error("Document processing failed: %v", err)
		return
	}

	logger.Info("Document processing completed successfully")
}

func authenticate() (*TokenResponse, error) {
	codeChan := make(chan string)
	server := &http.Server{Addr: ":8080"}

	codeVerifier := generateCodeVerifier(64)
	codeChallenge := generateCodeChallenge(codeVerifier)

	http.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			w.Write([]byte("Error: No authorization code received"))
			return
		}
		codeChan <- code

		html := `
    <html>
        <head>
            <title>Authentication Successful</title>
        </head>
        <body>
            <h3>Authentication successful! This window will close automatically.</h3>
            <script>
                setTimeout(function() {
                    window.close();
                }, 1000);
            </script>
        </body>
    </html>
    `
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	})

	go server.ListenAndServe()
	defer server.Shutdown(context.Background())

	authURLFull := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256",
		authURL, clientID, redirectURI, codeChallenge)

	if err := browser.OpenURL(authURLFull); err != nil {
		return nil, fmt.Errorf("failed to open browser: %v", err)
	}

	code := <-codeChan
	return exchangeCodeForToken(code, codeVerifier)
}

func generateCodeVerifier(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return base64.URLEncoding.EncodeToString(bytes)[:length]
}

func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
}

func exchangeCodeForToken(code, codeVerifier string) (*TokenResponse, error) {
	data := fmt.Sprintf("grant_type=authorization_code&code=%s&client_id=%s&redirect_uri=%s&code_verifier=%s",
		code, clientID, redirectURI, codeVerifier)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

type Logger struct {
	*log.Logger
}

func NewLogger() *Logger {
	return &Logger{
		Logger: log.New(os.Stdout, "", log.LstdFlags),
	}
}

func (l *Logger) Info(format string, v ...any) {
	l.Printf("[INFO] "+format, v...)
}

func (l *Logger) Error(format string, v ...any) {
	l.Printf("[ERROR] "+format, v...)
}

func init() {
	if err := os.MkdirAll("./documents", 0755); err != nil {
		log.Fatalf("Failed to create documents directory: %v", err)
	}
}

func performBulkLookup(accessToken string, request BulkLookupRequest) (map[string]string, error) {
	logger := NewLogger()

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("error marshaling bulk lookup request: %v", err)
	}

	logger.Info("Sending bulk lookup request: %s", string(jsonData))

	req, err := http.NewRequest("POST", bulkLookupURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("error creating bulk lookup request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bulk lookup request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	logger.Info("Received bulk lookup response: %s", string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bulk lookup failed with status %d: %s", resp.StatusCode, string(body))
	}

	var results map[string]string
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("error decoding bulk lookup response: %v", err)
	}

	for key, value := range results {
		if strings.HasPrefix(value, "ERROR:") {
			logger.Error("Lookup error for %s: %s", key, value)
		} else {
			logger.Info("Found ID for %s: %s", key, value)
		}
	}

	return results, nil
}

func parseFileName(filename string) (*DocumentInfo, error) {
	parts := strings.Split(filename, "_")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid filename format: %s", filename)
	}

	info := &DocumentInfo{
		FilePath:      filename,
		NamePath:      make(map[string]string),
		SalesforceIds: make(map[string]string),
	}

	switch parts[0] {
	case "bl":
		info.DocumentType = DocTypeBuildingLocation
	case "f":
		info.DocumentType = DocTypeFinish
	case "fp":
		info.DocumentType = DocTypeFloorPlan
	case "g":
		info.DocumentType = DocTypeGallery
	case "pp":
		info.DocumentType = DocTypeProjectPlan
	case "up":
		info.DocumentType = DocTypeUnitPlan
	case "gd":
		info.DocumentType = DocTypeGeneric
	default:
		return nil, fmt.Errorf("unknown document type prefix: %s", parts[0])
	}

	switch parts[1] {
	case "u": // Unit
		info.EntityType = "UNIT"
		if len(parts) != 7 {
			return nil, fmt.Errorf("invalid unit filename format")
		}
		info.NamePath["project"] = parts[2]
		info.NamePath["phase"] = parts[3]
		info.NamePath["building"] = parts[5]
		info.NamePath["unit"] = strings.TrimSuffix(parts[6], filepath.Ext(parts[6]))

	case "p": // Phase
		info.EntityType = "PHASE"
		if len(parts) != 4 {
			return nil, fmt.Errorf("invalid phase filename format")
		}
		info.NamePath["project"] = parts[2]
		info.NamePath["phase"] = strings.TrimSuffix(parts[3], filepath.Ext(parts[3]))

	case "z": // Zone
		info.EntityType = "ZONE"
		if len(parts) != 5 {
			return nil, fmt.Errorf("invalid zone filename format")
		}
		info.NamePath["project"] = parts[2]
		info.NamePath["phase"] = parts[3]
		info.NamePath["zone"] = strings.TrimSuffix(parts[4], filepath.Ext(parts[4]))

	case "b": // Building
		info.EntityType = "BUILDING"
		if len(parts) != 6 {
			return nil, fmt.Errorf("invalid building filename format")
		}
		info.NamePath["project"] = parts[2]
		info.NamePath["phase"] = parts[3]
		info.NamePath["zone"] = parts[4]
		info.NamePath["building"] = strings.TrimSuffix(parts[5], filepath.Ext(parts[5]))

	case "dt": // Design Type
		info.EntityType = "DESIGN_TYPE"
		if len(parts) != 5 {
			return nil, fmt.Errorf("invalid design type filename format")
		}
		info.NamePath["project"] = parts[2]
		info.NamePath["phase"] = parts[3]
		info.NamePath["designType"] = strings.TrimSuffix(parts[4], filepath.Ext(parts[4]))

	default:
		return nil, fmt.Errorf("unknown entity type identifier: %s", parts[1])
	}

	return info, nil
}

func processDocuments(accessToken string, documentsDir string) error {

	documents, err := collectDocuments(documentsDir)
	if err != nil {
		return fmt.Errorf("error collecting documents: %v", err)
	}

	err = bulkLookupEntities(accessToken, documents)
	if err != nil {
		return fmt.Errorf("bulk lookup failed: %v", err)
	}

	err = bulkUploadContentVersions(accessToken, documents)
	if err != nil {
		return fmt.Errorf("bulk content upload failed: %v", err)
	}

	err = bulkCreateAttachmentUploaders(accessToken, documents)
	if err != nil {
		return fmt.Errorf("bulk attachment uploader creation failed: %v", err)
	}

	return nil
}

func bulkLookupEntities(accessToken string, documents []DocumentInfo) error {
	logger := NewLogger()

	var bulkRequest BulkLookupRequest
	for _, doc := range documents {
		bulkRequest.Lookups = append(bulkRequest.Lookups, EntityLookup{
			EntityType: doc.EntityType,
			NamePath:   doc.NamePath,
		})
	}

	results, err := performBulkLookup(accessToken, bulkRequest)
	if err != nil {
		return err
	}

	for i := range documents {
		found := false
		for resultKey, resultId := range results {
			var resultLookup EntityLookup
			if err := json.Unmarshal([]byte(resultKey), &resultLookup); err != nil {
				continue
			}

			if resultLookup.EntityType == documents[i].EntityType &&
				compareNamePaths(resultLookup.NamePath, documents[i].NamePath) {

				if strings.HasPrefix(resultId, "ERROR:") {
					return fmt.Errorf("lookup failed for %s: %s", documents[i].FilePath, resultId)
				}

				entityType := strings.ToLower(documents[i].EntityType)
				documents[i].SalesforceIds[entityType] = resultId
				logger.Info("Stored %s ID: %s for document: %s",
					documents[i].EntityType, resultId, documents[i].FilePath)
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

func bulkUploadContentVersions(accessToken string, documents []DocumentInfo) error {
	logger := NewLogger()
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
			sfInstanceURL+"/services/data/v57.0/composite",
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
			sfInstanceURL,
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

func generateDisplayValue(doc DocumentInfo) string {
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

func bulkCreateAttachmentUploaders(accessToken string, documents []DocumentInfo) error {
	logger := NewLogger()
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
			sfInstanceURL+"/services/data/v57.0/composite",
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

func getContentType(filename string) string {
	mime, err := mimetype.DetectFile(filename)
	if err != nil {
		return "Image"
	}

	mainType := strings.Split(mime.String(), "/")[0]

	switch {
	case mainType == "image":
		return "Image"
	case mime.String() == "application/pdf":
		return "PDF"
	case mainType == "video":
		return "Video"
	default:
		return "Image"
	}
}

type BulkJobResponse struct {
	Id             string    `json:"id"`
	Operation      string    `json:"operation"`
	Object         string    `json:"object"`
	CreatedById    string    `json:"createdById"`
	CreatedDate    time.Time `json:"createdDate"`
	SystemModstamp time.Time `json:"systemModstamp"`
	State          string    `json:"state"`
	ContentType    string    `json:"contentType"`
	ApiVersion     float64   `json:"apiVersion"`
}

func collectDocuments(documentsDir string) ([]DocumentInfo, error) {
	logger := NewLogger()
	files, err := os.ReadDir(documentsDir)
	if err != nil {
		return nil, fmt.Errorf("error reading documents directory: %v", err)
	}

	var documents []DocumentInfo
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		logger.Info("Reading file: %s", file.Name())

		docInfo, err := parseFileName(file.Name())
		if err != nil {
			return nil, fmt.Errorf("error parsing filename %s: %v", file.Name(), err)
		}

		documents = append(documents, *docInfo)
	}

	logger.Info("Collected %d documents for processing", len(documents))
	return documents, nil
}
