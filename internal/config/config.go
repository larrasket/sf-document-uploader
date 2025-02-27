package config

import (
	"embed"
	"log"
	"strings"
)

var (
	SFInstanceURL string
	AuthURL       string
	TokenURL      string
	BulkLookupURL string
	ClientID      string
	RedirectURI   string
	Environment   string
	envMap        map[string]string
)

// Document Types
const (
	DocTypeBuildingLocation = "Building Location"
	DocTypeFinish           = "Finish"
	DocTypeFloorPlan        = "Floor Plan"
	DocTypeGallery          = "Gallery"
	DocTypeProjectPlan      = "Project Plan"
	DocTypeUnitPlan         = "Unit Plan"
	DocTypeGeneric          = "Generic Document"
)

const (
	ContentTypeImage = "Image"
	ContentTypePDF   = "PDF"
	ContentTypeVideo = "Video"
)

func LoadEnv(configFS embed.FS) {
	envMap = make(map[string]string)

	embeddedEnv, err := configFS.ReadFile(".env")
	if err != nil {
		log.Fatal("Error loading embedded .env file", err)
	}
	parseEnvFile(string(embeddedEnv), envMap)

	SFInstanceURL = getEnv("SF_INSTANCE_URL")
	ClientID = getEnv("CLIENT_ID")
	RedirectURI = getEnv("REDIRECT_URI")
	Environment = getEnv("ENV")

	AuthURL = SFInstanceURL + "/services/oauth2/authorize"
	TokenURL = SFInstanceURL + "/services/oauth2/token"
	BulkLookupURL = SFInstanceURL + "/services/apexrest/admin/bulk-lookup"
}

func parseEnvFile(content string, envMap map[string]string) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		value = strings.Trim(value, `"'`)

		envMap[key] = value
	}
}

func getEnv(key string) string {
	if value, exists := envMap[key]; exists {
		return value
	}
	log.Fatal("Error loading env: ", key)
	return ""
}

func IsDevelopment() bool {
	return Environment == "development"
}
