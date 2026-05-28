package usagehistory

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// Compact removes JSONL files older than retentionDays.
// If retentionDays <= 0, no files are removed.
func Compact(dir string, retentionDays int) {
	if retentionDays <= 0 {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.WithError(err).Warn("usagehistory: failed to read history dir for compaction")
		}
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "usage-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		dateStr := strings.TrimPrefix(name, "usage-")
		dateStr = strings.TrimSuffix(dateStr, ".jsonl")
		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if fileDate.Before(cutoff) {
			path := filepath.Join(dir, name)
			if err := os.Remove(path); err != nil {
				log.WithError(err).WithField("file", name).Warn("usagehistory: failed to remove old file")
			} else {
				log.WithField("file", name).Info("usagehistory: removed expired file")
			}
		}
	}
}
