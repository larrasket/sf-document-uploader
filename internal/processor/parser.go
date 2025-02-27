package processor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ORAITApps/document-uploader/internal/config"
	"github.com/ORAITApps/document-uploader/internal/models"
)

func ParseFileName(filename string) (*models.DocumentInfo, error) {
	parts := strings.Split(filename, "_")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid filename format: %s", filename)
	}

	info := &models.DocumentInfo{
		FilePath:      filename,
		NamePath:      make(map[string]string),
		SalesforceIds: make(map[string]string),
	}

	if err := parseDocumentType(parts[0], info); err != nil {
		return nil, err
	}

	if err := parseEntityType(parts[1:], info); err != nil {
		return nil, err
	}

	return info, nil
}

func parseDocumentType(prefix string, info *models.DocumentInfo) error {
	docTypes := map[string]string{
		"bl": config.DocTypeBuildingLocation,
		"f":  config.DocTypeFinish,
		"fp": config.DocTypeFloorPlan,
		"g":  config.DocTypeGallery,
		"pp": config.DocTypeProjectPlan,
		"up": config.DocTypeUnitPlan,
		"gd": config.DocTypeGeneric,
	}

	if docType, ok := docTypes[prefix]; ok {
		info.DocumentType = docType
		return nil
	}
	return fmt.Errorf("unknown document type prefix: %s", prefix)
}

func parseEntityType(parts []string, info *models.DocumentInfo) error {
	entityType := parts[0]
	remaining := parts[1:]

	switch entityType {
	case "u": // Unit
		if len(remaining) != 5 {
			return fmt.Errorf("invalid unit filename format: expected 5 parts, got %d", len(remaining))
		}
		info.EntityType = "UNIT"
		info.NamePath["project"] = remaining[0]
		info.NamePath["phase"] = remaining[1]
		info.NamePath["building"] = remaining[3]
		info.NamePath["unit"] = strings.TrimSuffix(remaining[4], filepath.Ext(remaining[4]))

	case "p": // Phase
		if len(remaining) != 2 {
			return fmt.Errorf("invalid phase filename format: expected 2 parts, got %d", len(remaining))
		}
		info.EntityType = "PHASE"
		info.NamePath["project"] = remaining[0]
		info.NamePath["phase"] = strings.TrimSuffix(remaining[1], filepath.Ext(remaining[1]))

	case "z": // Zone
		if len(remaining) != 3 {
			return fmt.Errorf("invalid zone filename format: expected 3 parts, got %d", len(remaining))
		}
		info.EntityType = "ZONE"
		info.NamePath["project"] = remaining[0]
		info.NamePath["phase"] = remaining[1]
		info.NamePath["zone"] = strings.TrimSuffix(remaining[2], filepath.Ext(remaining[2]))

	case "b": // Building
		if len(remaining) != 4 {
			return fmt.Errorf("invalid building filename format: expected 4 parts, got %d", len(remaining))
		}
		info.EntityType = "BUILDING"
		info.NamePath["project"] = remaining[0]
		info.NamePath["phase"] = remaining[1]
		info.NamePath["zone"] = remaining[2]
		info.NamePath["building"] = strings.TrimSuffix(remaining[3], filepath.Ext(remaining[3]))

	case "dt": // Design Type
		if len(remaining) != 3 {
			return fmt.Errorf("invalid design type filename format: expected 3 parts, got %d", len(remaining))
		}
		info.EntityType = "DESIGN_TYPE"
		info.NamePath["project"] = remaining[0]
		info.NamePath["phase"] = remaining[1]
		info.NamePath["designType"] = strings.TrimSuffix(remaining[2], filepath.Ext(remaining[2]))

	default:
		return fmt.Errorf("unknown entity type identifier: %s", entityType)
	}

	return nil
}
