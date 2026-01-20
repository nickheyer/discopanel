package modrinth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nickheyer/discopanel/internal/config"
)

const (
	BaseURL = "https://api.modrinth.com/v2"
)

type Client struct {
	config     *config.Config
	httpClient *http.Client
}

func NewClient(cfg *config.Config) *Client {
	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SearchResponse represents the Modrinth search API response
type SearchResponse struct {
	Hits      []Project `json:"hits"`
	Offset    int       `json:"offset"`
	Limit     int       `json:"limit"`
	TotalHits int       `json:"total_hits"`
}

// Project represents a Modrinth project (modpack)
type Project struct {
	Slug              string   `json:"slug"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Categories        []string `json:"categories"`
	ClientSide        string   `json:"client_side"`
	ServerSide        string   `json:"server_side"`
	ProjectType       string   `json:"project_type"`
	Downloads         int64    `json:"downloads"`
	IconURL           string   `json:"icon_url"`
	ProjectID         string   `json:"project_id"`
	Author            string   `json:"author"`
	DisplayCategories []string `json:"display_categories"`
	Versions          []string `json:"versions"`
	Follows           int      `json:"follows"`
	DateCreated       string   `json:"date_created"`
	DateModified      string   `json:"date_modified"`
	LatestVersion     string   `json:"latest_version"`
	License           string   `json:"license"`
	Gallery           []string `json:"gallery"`
	FeaturedGallery   string   `json:"featured_gallery"`
	Color             *int     `json:"color"`
}

// ProjectDetails represents full project details from GET /project/{id}
type ProjectDetails struct {
	ID                   string        `json:"id"`
	Slug                 string        `json:"slug"`
	Title                string        `json:"title"`
	Description          string        `json:"description"`
	Body                 string        `json:"body"`
	ProjectType          string        `json:"project_type"`
	ClientSide           string        `json:"client_side"`
	ServerSide           string        `json:"server_side"`
	GameVersions         []string      `json:"game_versions"`
	Loaders              []string      `json:"loaders"`
	Categories           []string      `json:"categories"`
	AdditionalCategories []string      `json:"additional_categories"`
	Status               string        `json:"status"`
	Published            string        `json:"published"`
	Updated              string        `json:"updated"`
	Downloads            int64         `json:"downloads"`
	Followers            int           `json:"followers"`
	IconURL              string        `json:"icon_url"`
	Color                *int          `json:"color"`
	Gallery              []GalleryItem `json:"gallery"`
	SourceURL            string        `json:"source_url"`
	IssuesURL            string        `json:"issues_url"`
	WikiURL              string        `json:"wiki_url"`
	DiscordURL           string        `json:"discord_url"`
	DonationURLs         []DonationURL `json:"donation_urls"`
	Team                 string        `json:"team"`
	Versions             []string      `json:"versions"`
	License              License       `json:"license"`
}

type GalleryItem struct {
	URL         string  `json:"url"`
	Featured    bool    `json:"featured"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Created     string  `json:"created"`
	Ordering    int     `json:"ordering"`
}

type DonationURL struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	URL      string `json:"url"`
}

type License struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Version represents a project version
type Version struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	VersionNumber string       `json:"version_number"`
	ProjectID     string       `json:"project_id"`
	GameVersions  []string     `json:"game_versions"`
	Loaders       []string     `json:"loaders"`
	VersionType   string       `json:"version_type"`
	Featured      bool         `json:"featured"`
	Status        string       `json:"status"`
	Files         []File       `json:"files"`
	Downloads     int64        `json:"downloads"`
	DatePublished string       `json:"date_published"`
	Changelog     *string      `json:"changelog"`
	Dependencies  []Dependency `json:"dependencies"`
}

type File struct {
	Hashes   Hashes `json:"hashes"`
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
	Size     int64  `json:"size"`
	FileType string `json:"file_type"`
}

type Hashes struct {
	SHA512 string `json:"sha512"`
	SHA1   string `json:"sha1"`
}

type Dependency struct {
	VersionID      *string `json:"version_id"`
	ProjectID      *string `json:"project_id"`
	FileName       *string `json:"file_name"`
	DependencyType string  `json:"dependency_type"`
}

// SearchModpacks searches for modpacks on Modrinth
func (c *Client) SearchModpacks(ctx context.Context, query string, gameVersion string, modLoader string, offset, limit int) (*SearchResponse, error) {
	// Build facets for filtering
	facets := [][]string{
		{"project_type:modpack"},
	}

	if gameVersion != "" {
		facets = append(facets, []string{fmt.Sprintf("versions:%s", gameVersion)})
	}

	if modLoader != "" {
		// Modrinth uses categories for loaders
		facets = append(facets, []string{fmt.Sprintf("categories:%s", strings.ToLower(modLoader))})
	}

	// Convert facets to JSON string
	facetsJSON, err := json.Marshal(facets)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal facets: %w", err)
	}

	// Build URL with query parameters
	params := url.Values{}
	if query != "" {
		params.Set("query", query)
	}
	params.Set("facets", string(facetsJSON))
	params.Set("index", "downloads") // Sort by downloads
	params.Set("offset", strconv.Itoa(offset))
	params.Set("limit", strconv.Itoa(limit))

	reqURL := fmt.Sprintf("%s/search?%s", BaseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", c.config.Server.UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, c.formatError(req, resp)
	}

	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &searchResp, nil
}

// GetModpack retrieves detailed information about a specific modpack
func (c *Client) GetModpack(ctx context.Context, modpackID string) (*ProjectDetails, error) {
	reqURL := fmt.Sprintf("%s/project/%s", BaseURL, modpackID)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", c.config.Server.UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, c.formatError(req, resp)
	}

	var project ProjectDetails
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &project, nil
}

// GetModpackVersions retrieves all versions for a specific modpack
func (c *Client) GetModpackVersions(ctx context.Context, modpackID string) ([]Version, error) {
	reqURL := fmt.Sprintf("%s/project/%s/version", BaseURL, modpackID)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", c.config.Server.UserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, c.formatError(req, resp)
	}

	var versions []Version
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return versions, nil
}

func (c *Client) formatError(req *http.Request, resp *http.Response) error {
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	body := string(bodyBytes)
	if body != "" {
		return fmt.Errorf("modrinth API error: %s (url=%s body=%s)", resp.Status, req.URL.String(), body)
	}
	return fmt.Errorf("modrinth API error: %s (url=%s)", resp.Status, req.URL.String())
}
