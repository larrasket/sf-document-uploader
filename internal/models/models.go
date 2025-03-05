package models

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
	RelativePath      string // Add this field
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
