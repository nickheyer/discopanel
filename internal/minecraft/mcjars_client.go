package minecraft

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const (
	mcjarsAPI = "https://mcjars.app"
)

type Version struct {
	Version string `json:"version"`
}

// compareVersions compares two Minecraft version strings
func compareVersions(a, b string) bool {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		aNum, _ := strconv.Atoi(aParts[i])
		bNum, _ := strconv.Atoi(bParts[i])
		if aNum != bNum {
			return aNum > bNum
		}
	}
	return len(aParts) > len(bParts)
}

func GetVersions(serverType string) ([]string, error) {
	resp, err := http.Get(fmt.Sprintf("%s/api/v2/builds/%s", mcjarsAPI, serverType))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch %s versions: %s", serverType, resp.Status)
	}

	var result struct {
		Success bool                   `json:"success"`
		Builds  map[string]interface{} `json:"builds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("API returned unsuccessful response")
	}

	var versionStrings []string
	for version := range result.Builds {
		versionStrings = append(versionStrings, version)
	}

	// Sort versions in descending order (newest first)
	sort.Slice(versionStrings, func(i, j int) bool {
		return compareVersions(versionStrings[i], versionStrings[j])
	})

	return versionStrings, nil
}

func GetServerTypes() (*TypesResponse, error) {
	resp, err := http.Get(fmt.Sprintf("%s/api/v2/types", mcjarsAPI))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch server types: %s", resp.Status)
	}

	var result TypesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("API returned unsuccessful response")
	}

	return &result, nil
}
