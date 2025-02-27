package salesforce

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"document-uploader/internal/config"
	"document-uploader/internal/models"
)

type Client struct {
	accessToken string
	httpClient  *http.Client
}

func NewClient(accessToken string) *Client {
	return &Client{
		accessToken: accessToken,
		httpClient:  &http.Client{},
	}
}

func (c *Client) MakeRequest(method, url string, body interface{}) (*http.Response, error) {
	var bodyReader *bytes.Buffer
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")

	return c.httpClient.Do(req)
}

func (c *Client) BulkLookup(request models.BulkLookupRequest) (map[string]string, error) {
	resp, err := c.MakeRequest("POST", config.BulkLookupURL, request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var results map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}

	return results, nil
}
