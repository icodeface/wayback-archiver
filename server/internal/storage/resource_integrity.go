package storage

import (
	"fmt"
	"os"
)

type IntegrityScanSummary struct {
	ResourceType     string
	ScannedResources int
	ScannedFiles     int
	CorruptedFiles   int
	MissingFiles     int
	QuarantinedFiles int
}

func (d *Deduplicator) QuarantineCorruptedResources(resourceType string, batchSize int) (*IntegrityScanSummary, error) {
	if batchSize <= 0 {
		batchSize = 200
	}

	summary := &IntegrityScanSummary{ResourceType: resourceType}
	handled := make(map[string]struct{})
	var lastID int64

	for {
		resources, err := d.db.ListResourcesForIntegrityCheck(resourceType, lastID, batchSize)
		if err != nil {
			return nil, err
		}
		if len(resources) == 0 {
			return summary, nil
		}

		for _, resource := range resources {
			lastID = resource.ID
			summary.ScannedResources++
			if resource.FilePath == "" {
				continue
			}
			if _, seen := handled[resource.FilePath]; seen {
				continue
			}
			handled[resource.FilePath] = struct{}{}
			summary.ScannedFiles++

			actualHash, err := d.storage.ResourceHash(resource.FilePath)
			if err == nil && actualHash == resource.ContentHash {
				continue
			}

			reason := "resource file missing"
			if err == nil {
				reason = fmt.Sprintf("hash mismatch expected=%s actual=%s", shortHashForLog(resource.ContentHash), shortHashForLog(actualHash))
				summary.CorruptedFiles++
			} else if os.IsNotExist(err) {
				summary.MissingFiles++
			} else {
				reason = fmt.Sprintf("integrity check failed: %v", err)
				summary.CorruptedFiles++
			}

			if quarantineErr := d.quarantineResourceFile(resource.FilePath, reason); quarantineErr != nil {
				return nil, quarantineErr
			}
			summary.QuarantinedFiles++
		}
	}
}

func (d *Deduplicator) QuarantineCorruptedCSS(batchSize int) (*IntegrityScanSummary, error) {
	return d.QuarantineCorruptedResources("css", batchSize)
}
