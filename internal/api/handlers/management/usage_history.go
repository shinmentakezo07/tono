package management

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usagehistory"
)

// GetUsageHistory reads all JSONL history files and returns them as a merged array.
func (h *Handler) GetUsageHistory(c *gin.Context) {
	if !usagehistory.Enabled() {
		c.JSON(http.StatusOK, gin.H{"records": []interface{}{}})
		return
	}

	dir := h.cfg.UsageHistoryDir
	if dir == "" {
		dir = "usage-history"
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, gin.H{"records": []interface{}{}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read history directory"})
		return
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "usage-") && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)

	var records []json.RawMessage
	for _, path := range files {
		readJSONLFile(path, &records)
	}

	c.JSON(http.StatusOK, gin.H{"records": records})
}

func readJSONLFile(path string, out *[]json.RawMessage) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		raw := make(json.RawMessage, len(line))
		copy(raw, line)
		*out = append(*out, raw)
	}
}
