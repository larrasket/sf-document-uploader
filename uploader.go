package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"

	"github.com/pkg/browser"
)

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
	fmt.Println("For token exchange, you'll need to use the code_verifier:", verifierStore)

	if err := server.Shutdown(context.Background()); err != nil {
		log.Printf("Error shutting down server: %v", err)
	}

	fmt.Println("Authentication flow completed successfully")
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
