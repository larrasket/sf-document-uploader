package filestructure

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ORAITApps/document-uploader/internal/models"
)

type DocumentWalker struct {
	documentsDir string
	documents    []models.DocumentInfo
}

func NewDocumentWalker(documentsDir string) *DocumentWalker {
	return &DocumentWalker{
		documentsDir: documentsDir,
		documents:    make([]models.DocumentInfo, 0),
	}
}

func (w *DocumentWalker) Walk() ([]models.DocumentInfo, error) {
	err := filepath.Walk(w.documentsDir, w.processPath)
	if err != nil {
		return nil, err
	}
	return w.documents, nil
}

func (w *DocumentWalker) processPath(path string, info os.FileInfo, err error) error {
	if err != nil {
		return err
	}

	if info.IsDir() || strings.HasPrefix(info.Name(), ".") {
		return nil
	}

	// Get relative path from documents directory
	relPath, err := filepath.Rel(w.documentsDir, path)
	if err != nil {
		return err
	}

	pathComponents := strings.Split(filepath.Dir(relPath), string(os.PathSeparator))
	fileName := info.Name()

	docInfo, err := parseDocument(fileName, pathComponents)
	if err != nil {
		return err
	}

	if docInfo != nil {
		docInfo.FilePath = fileName
		docInfo.RelativePath = relPath
		w.documents = append(w.documents, *docInfo)
	}

	return nil
}

func parseDocument(fileName string, pathComponents []string) (*models.DocumentInfo, error) {
	parts := strings.Split(strings.TrimSuffix(fileName, filepath.Ext(fileName)), "_")
	if len(parts) < 1 {
		return nil, nil
	}

	docInfo := &models.DocumentInfo{
		FilePath:      fileName,
		NamePath:      make(map[string]string),
		SalesforceIds: make(map[string]string),
	}

	prefix := parts[0]
	if err := setDocumentType(prefix, docInfo); err != nil {
		return nil, err
	}

	return processPathComponents(docInfo, pathComponents, parts)
}

func processPathComponents(docInfo *models.DocumentInfo, pathComponents []string, fileNameParts []string) (*models.DocumentInfo, error) {
	if len(pathComponents) < 1 {
		return nil, fmt.Errorf("invalid path structure: missing project name")
	}
	docInfo.NamePath["project"] = pathComponents[0]

	if len(pathComponents) >= 3 && pathComponents[2] == "design_types" {
		docInfo.EntityType = "DESIGN_TYPE"
		docInfo.NamePath["phase"] = pathComponents[1]
		if len(fileNameParts) > 1 {
			docInfo.NamePath["designType"] = strings.Join(fileNameParts[1:], "_")
		}
		return docInfo, nil
	}

	prefix := fileNameParts[0]
	if err := setDocumentType(prefix, docInfo); err != nil {
		return nil, err
	}

	switch {
	case len(pathComponents) == 2 && prefix == "pp":
		docInfo.EntityType = "PHASE"
		docInfo.NamePath["phase"] = pathComponents[1]

	case len(pathComponents) == 3 && prefix == "f":
		docInfo.EntityType = "ZONE"
		docInfo.NamePath["phase"] = pathComponents[1]
		docInfo.NamePath["zone"] = pathComponents[2]

	case len(pathComponents) >= 4:
		docInfo.NamePath["phase"] = pathComponents[1]
		docInfo.NamePath["zone"] = pathComponents[2]
		docInfo.NamePath["building"] = pathComponents[3]

		if len(pathComponents) >= 5 && pathComponents[4] == "units" {
			docInfo.EntityType = "UNIT"
			if len(fileNameParts) > 1 {
				unitName := strings.Join(fileNameParts[1:], "_")
				unitName = strings.TrimSuffix(unitName, filepath.Ext(unitName))
				docInfo.NamePath["unit"] = unitName
			} else {
				return nil, fmt.Errorf("invalid unit filename format: %v", fileNameParts)
			}
		} else {
			docInfo.EntityType = "BUILDING"
		}

	default:
		return nil, fmt.Errorf("invalid path structure: %v", pathComponents)
	}

	if docInfo.EntityType == "" {
		return nil, fmt.Errorf("could not determine entity type for path: %v", pathComponents)
	}

	fmt.Printf("Processed document:\n")
	fmt.Printf("  Entity Type: %s\n", docInfo.EntityType)
	fmt.Printf("  Name Path: %+v\n", docInfo.NamePath)

	return docInfo, nil
}

func setDocumentType(prefix string, docInfo *models.DocumentInfo) error {
	switch prefix {
	case "bl":
		docInfo.DocumentType = "Building Location"
	case "f":
		docInfo.DocumentType = "Finish"
	case "fp":
		docInfo.DocumentType = "Floor Plan"
	case "g":
		docInfo.DocumentType = "Gallery"
	case "pp":
		docInfo.DocumentType = "Project Plan"
	case "up":
		docInfo.DocumentType = "Unit Plan"
	default:
		return fmt.Errorf("unknown document type prefix: %s", prefix)
	}
	return nil
}
