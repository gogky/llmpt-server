package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// HFTreeEntry represents a single entry in the /tree API response
type HFTreeEntry struct {
	Type string `json:"type"` // "file" or "directory"
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// FetchHFManifest recursively fetches all files from the given Hugging Face repo tree, handling pagination via Link header.
// It returns a map of file path to its exact size.
func FetchHFManifest(repoType, repoID, revision, hfToken string) (map[string]int64, int, error) {
	hfEndpoint := os.Getenv("HF_ENDPOINT")
	if hfEndpoint == "" {
		hfEndpoint = "https://huggingface.co"
	}
	baseURL := fmt.Sprintf("%s/api/%ss/%s/tree/%s?recursive=true&expand=false", hfEndpoint, repoType, repoID, revision)

	client := &http.Client{}
	fileMap := make(map[string]int64)

	nextURL := baseURL
	status := 0

	for nextURL != "" {
		req, err := http.NewRequest("GET", nextURL, nil)
		if err != nil {
			return nil, status, fmt.Errorf("failed to create request: %w", err)
		}

		if hfToken != "" {
			req.Header.Set("Authorization", "Bearer "+hfToken)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, status, fmt.Errorf("failed to execute request: %w", err)
		}
		status = resp.StatusCode

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, status, fmt.Errorf("HF API returned status %d: %s", resp.StatusCode, string(body))
		}

		var entries []HFTreeEntry
		if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
			resp.Body.Close()
			return nil, status, fmt.Errorf("failed to decode HF response: %w", err)
		}

		// Parse Link header for pagination
		linkHeader := resp.Header.Get("Link")
		nextURL = getNextPageURL(linkHeader)
		resp.Body.Close()

		for _, entry := range entries {
			if entry.Type == "file" {
				// optional: skip specific hidden files if needed, but client shouldn't have them anyway
				fileMap[entry.Path] = entry.Size
			}
		}
	}

	return fileMap, status, nil
}

// getNextPageURL parses the GitHub-style Link header to find the next page URL
func getNextPageURL(linkHeader string) string {
	if linkHeader == "" {
		return ""
	}

	// Example Link header: <https://huggingface.co/api/...&cursor=XYZ>; rel="next"
	links := strings.Split(linkHeader, ",")
	for _, l := range links {
		parts := strings.Split(l, ";")
		if len(parts) >= 2 {
			urlPart := strings.TrimSpace(parts[0])
			relPart := strings.TrimSpace(parts[1])

			if strings.HasPrefix(urlPart, "<") && strings.HasSuffix(urlPart, ">") && strings.Contains(relPart, `rel="next"`) {
				return urlPart[1 : len(urlPart)-1]
			}
		}
	}
	return ""
}
