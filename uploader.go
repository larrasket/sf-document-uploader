package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/pkg/browser"
)

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

func main() {
	clientID := "3MVG9CmdhfW8tOGCdf5CPNqN58A4eHJFXV_qMITEB_XMGHAUPHLFTWOejtNXvvOjaLCl0s41X6OPPB.U._3xO"
	if clientID == "" {
		log.Fatal("SALESFORCE_CLIENT_ID environment variable must be set")
	}

	codeChan := make(chan string)
	server := &http.Server{Addr: ":8080"}

	codeVerifier := generateCodeVerifier(64)
	codeChallenge := generateCodeChallenge(codeVerifier)

	verifierStore := codeVerifier

	http.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errDesc := r.URL.Query().Get("error_description")
			fmt.Printf("Error in callback: %s - %s\n", errMsg, errDesc)
			w.Write([]byte(fmt.Sprintf("Authorization failed: %s - %s", errMsg, errDesc)))
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			fmt.Println("No code parameter found in callback URL")
			w.Write([]byte("Error: No authorization code received"))
			return
		}

		codeChan <- code
		w.Write([]byte("Authentication successful! You can close this window now."))
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	redirectURI := "http://localhost:8080/oauth/callback"

	authURL := "https://ora-egypt--mas.sandbox.my.salesforce-setup.com/services/oauth2/authorize"

	fullAuthURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256",
		authURL, clientID, redirectURI, codeChallenge)

	fmt.Println("Opening browser for Salesforce login...")
	fmt.Println("Using code verifier:", verifierStore)
	fmt.Println("Using code challenge:", codeChallenge)
	if err := browser.OpenURL(fullAuthURL); err != nil {
		log.Fatalf("Failed to open browser: %v", err)
	}

	code := <-codeChan

	fmt.Println("Received authorization code from Salesforce")
	fmt.Println("Authorization code:", code)

	// Exchange the code for a token
	tokenResp, err := exchangeCodeForToken(code, verifierStore, clientID)
	if err != nil {
		log.Fatalf("Failed to exchange code for token: %v", err)
	}

	fmt.Println("Successfully obtained access token")

	// Make the zones API request
	err = getZones(tokenResp.AccessToken)
	if err != nil {
		log.Fatalf("Failed to get zones: %v", err)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		log.Printf("Error shutting down server: %v", err)
	}

	fmt.Println("Authentication flow and API request completed successfully")
}

func generateCodeVerifier(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		log.Fatalf("Error generating random bytes: %v", err)
	}
	encoded := base64.URLEncoding.EncodeToString(bytes)
	return encoded[:length]
}

func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
}

func exchangeCodeForToken(code, codeVerifier, clientID string) (*TokenResponse, error) {
	tokenURL := "https://ora-egypt--mas.sandbox.my.salesforce-setup.com/services/oauth2/token"

	// Remove client_secret from the request
	data := fmt.Sprintf("grant_type=authorization_code&code=%s&client_id=%s&redirect_uri=http://localhost:8080/oauth/callback&code_verifier=%s",
		code, clientID, codeVerifier)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("error creating token request: %v", err)
	}

	// Add additional debugging
	fmt.Println("Token request data:", data)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making token request: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading token response: %v", err)
	}

	// Add response debugging
	fmt.Println("Token response status:", resp.Status)
	fmt.Println("Token response body:", string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("error parsing token response: %v", err)
	}

	return &tokenResp, nil
}

func getZones(accessToken string) error {
	apiURL := "https://ora-egypt--mas.sandbox.my.salesforce-setup.com/services/apexrest/admin/zones"

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("error creating API request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error making API request: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading API response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("Zones API Response: %s\n", string(body))
	return nil

}
