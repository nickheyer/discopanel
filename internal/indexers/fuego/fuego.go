package fuego

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	BaseURL         = "https://api.curseforge.com/v1"
	MinecraftGameID = 432
	ModpackClassID  = 4471
)

type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type SearchModsResponse struct {
	Data       []Modpack  `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type Pagination struct {
	Index       int `json:"index"`
	PageSize    int `json:"pageSize"`
	ResultCount int `json:"resultCount"`
	TotalCount  int `json:"totalCount"`
}

type Modpack struct {
	ID                 int          `json:"id"`
	GameID             int          `json:"gameId"`
	Name               string       `json:"name"`
	Slug               string       `json:"slug"`
	Links              Links        `json:"links"`
	Summary            string       `json:"summary"`
	Status             int          `json:"status"`
	DownloadCount      float64      `json:"downloadCount"`
	IsFeatured         bool         `json:"isFeatured"`
	PrimaryCategoryID  int          `json:"primaryCategoryId"`
	Categories         []Category   `json:"categories"`
	Authors            []Author     `json:"authors"`
	Logo               Logo         `json:"logo"`
	Screenshots        []Screenshot `json:"screenshots"`
	MainFileID         int          `json:"mainFileId"`
	LatestFiles        []File       `json:"latestFiles"`
	LatestFilesIndexes []FileIndex  `json:"latestFilesIndexes"`
	DateCreated        time.Time    `json:"dateCreated"`
	DateModified       time.Time    `json:"dateModified"`
	DateReleased       time.Time    `json:"dateReleased"`
}

type Links struct {
	WebsiteURL string `json:"websiteUrl"`
	WikiURL    string `json:"wikiUrl"`
	IssuesURL  string `json:"issuesUrl"`
	SourceURL  string `json:"sourceUrl"`
}

type Category struct {
	ID      int    `json:"id"`
	GameID  int    `json:"gameId"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	URL     string `json:"url"`
	IconURL string `json:"iconUrl"`
}

type Author struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Logo struct {
	ID           int    `json:"id"`
	ModID        int    `json:"modId"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	ThumbnailURL string `json:"thumbnailUrl"`
	URL          string `json:"url"`
}

type Screenshot struct {
	ID           int    `json:"id"`
	ModID        int    `json:"modId"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	ThumbnailURL string `json:"thumbnailUrl"`
	URL          string `json:"url"`
}

type File struct {
	ID                   int                   `json:"id"`
	GameID               int                   `json:"gameId"`
	ModID                int                   `json:"modId"`
	IsAvailable          bool                  `json:"isAvailable"`
	DisplayName          string                `json:"displayName"`
	FileName             string                `json:"fileName"`
	ReleaseType          int                   `json:"releaseType"`
	FileStatus           int                   `json:"fileStatus"`
	Hashes               []Hash                `json:"hashes"`
	FileDate             time.Time             `json:"fileDate"`
	FileLength           int64                 `json:"fileLength"`
	DownloadCount        int64                 `json:"downloadCount"`
	DownloadURL          string                `json:"downloadUrl"`
	GameVersions         []string              `json:"gameVersions"`
	SortableGameVersions []SortableGameVersion `json:"sortableGameVersions"`
	Dependencies         []Dependency          `json:"dependencies"`
	AlternateFileID      int                   `json:"alternateFileId"`
	ServerPackFileID     *int                  `json:"serverPackFileId"`
}

type Hash struct {
	Value string `json:"value"`
	Algo  int    `json:"algo"`
}

type SortableGameVersion struct {
	GameVersionName        string    `json:"gameVersionName"`
	GameVersionPadded      string    `json:"gameVersionPadded"`
	GameVersion            string    `json:"gameVersion"`
	GameVersionReleaseDate time.Time `json:"gameVersionReleaseDate"`
}

type Dependency struct {
	ModID        int `json:"modId"`
	RelationType int `json:"relationType"`
}

type FileIndex struct {
	GameVersion       string `json:"gameVersion"`
	FileID            int    `json:"fileId"`
	Filename          string `json:"filename"`
	ReleaseType       int    `json:"releaseType"`
	GameVersionTypeID *int   `json:"gameVersionTypeId"`
	ModLoader         *int   `json:"modLoader"`
}

type ModLoaderType int

const (
	ModLoaderAny        ModLoaderType = 0
	ModLoaderForge      ModLoaderType = 1
	ModLoaderCauldron   ModLoaderType = 2
	ModLoaderLiteLoader ModLoaderType = 3
	ModLoaderFabric     ModLoaderType = 4
	ModLoaderQuilt      ModLoaderType = 5
	ModLoaderNeoForge   ModLoaderType = 6
)

func (c *Client) SearchModpacks(ctx context.Context, query string, gameVersion string, modLoader ModLoaderType, index, pageSize int) (*SearchModsResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("fuego API key not configured")
	}

	params := url.Values{}
	params.Set("gameId", strconv.Itoa(MinecraftGameID))
	params.Set("classId", strconv.Itoa(ModpackClassID))
	params.Set("index", strconv.Itoa(index))
	params.Set("pageSize", strconv.Itoa(pageSize))
	params.Set("sortField", "2") // Sort by popularity
	params.Set("sortOrder", "desc")

	if query != "" {
		params.Set("searchFilter", query)
	}

	if gameVersion != "" {
		params.Set("gameVersion", gameVersion)
	}

	if modLoader != ModLoaderAny {
		params.Set("modLoaderType", strconv.Itoa(int(modLoader)))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/mods/search?%s", BaseURL, params.Encode()), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fuego API error: %s", resp.Status)
	}

	var result SearchModsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *Client) GetModpackFiles(ctx context.Context, modID int) ([]File, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("fuego API key not configured")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/mods/%d/files", BaseURL, modID), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fuego API error: %s", resp.Status)
	}

	var result struct {
		Data []File `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

func (c *Client) GetModpack(ctx context.Context, modID int) (*Modpack, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("fuego API key not configured")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/mods/%d", BaseURL, modID), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fuego API error: %s", resp.Status)
	}

	var result struct {
		Data Modpack `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result.Data, nil
}
