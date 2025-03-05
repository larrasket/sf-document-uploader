package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/ORAITApps/document-uploader/internal/config"
	"github.com/ORAITApps/document-uploader/internal/models"
	"github.com/pkg/browser"
)

var (
	server   *http.Server
	serverMu sync.Mutex
	mux      *http.ServeMux
)

func init() {
	mux = http.NewServeMux()
	server = &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
}

func Authenticate() (*models.TokenResponse, error) {
	serverMu.Lock()
	defer serverMu.Unlock()

	// Stop any existing server
	if server != nil {
		server.Shutdown(context.Background())
	}

	// Create new server and mux
	mux = http.NewServeMux()
	server = &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	codeChan := make(chan string)

	codeVerifier := generateCodeVerifier(64)
	codeChallenge := generateCodeChallenge(codeVerifier)

	mux.HandleFunc("/oauth/callback", createCallbackHandler(codeChan))

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	authURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256",
		config.AuthURL, config.ClientID, config.RedirectURI, codeChallenge)

	if err := browser.OpenURL(authURL); err != nil {
		return nil, fmt.Errorf("failed to open browser: %v", err)
	}

	code := <-codeChan

	if err := server.Shutdown(context.Background()); err != nil {
		fmt.Printf("Error shutting down server: %v\n", err)
	}

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

func createCallbackHandler(codeChan chan string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			w.Write([]byte("Error: No authorization code received"))
			return
		}
		codeChan <- code

		html := `
        <html>
            <head><title>Authentication Successful</title></head>
            <body>
                <h3>Authentication successful! This window will close automatically.</h3>
                <script>setTimeout(function() { window.close(); }, 2000);</script>
            </body>
        </html>
        `
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}
}

func exchangeCodeForToken(code, codeVerifier string) (*models.TokenResponse, error) {
	data := fmt.Sprintf("grant_type=authorization_code&code=%s&client_id=%s&redirect_uri=%s&code_verifier=%s",
		code, config.ClientID, config.RedirectURI, codeVerifier)

	req, err := http.NewRequest("POST", config.TokenURL, strings.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tokenResp models.TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}
