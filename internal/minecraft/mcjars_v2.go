package minecraft

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"
)

// Minimal client for MCJars v2
const defaultMCJarsBaseV2 = "https://mcjars.app"

type MCJarsClient struct {
    baseURL string
    client  *http.Client
}

func NewMCJarsClient() *MCJarsClient {
    return &MCJarsClient{
        baseURL: defaultMCJarsBaseV2,
        client: &http.Client{
            Timeout: 15 * time.Second,
        },
    }
}

// Models (minimal, expand as needed)
type GenericResponse struct {
    Success bool `json:"success"`
}

type TypesResponse struct {
    Success bool                   `json:"success"`
    Types   map[string]interface{} `json:"types"`
}

type BuildRequest struct {
    ID              *int64                 `json:"id,omitempty"`
    Type            *string                `json:"type,omitempty"`
    VersionID       *string                `json:"versionId,omitempty"`
    ProjectVersion  *string                `json:"projectVersionId,omitempty"`
    Name            *string                `json:"name,omitempty"`
    BuildNumber     *int                   `json:"buildNumber,omitempty"`
    Experimental    *bool                  `json:"experimental,omitempty"`
    JarUrl          *string                `json:"jarUrl,omitempty"`
    JarSize         *int64                 `json:"jarSize,omitempty"`
    ZipUrl          *string                `json:"zipUrl,omitempty"`
    ZipSize         *int64                 `json:"zipSize,omitempty"`
    Hash            map[string]*string     `json:"hash,omitempty"`
}

type BuildInfo struct {
    ID            int64   `json:"id"`
    VersionID     string  `json:"versionId"`
    ProjectVerID  *string `json:"projectVersionId"`
    Type          string  `json:"type"`
    Experimental  bool    `json:"experimental"`
    Name          string  `json:"name"`
    BuildNumber   int     `json:"buildNumber"`
    JarUrl        *string `json:"jarUrl"`
    JarSize       *int64  `json:"jarSize"`
    ZipUrl        *string `json:"zipUrl"`
    ZipSize       *int64  `json:"zipSize"`
    Created       *string `json:"created"`
}

type PostBuildResponse struct {
    Success bool       `json:"success"`
    Build   *BuildInfo `json:"build,omitempty"`
    Latest  *BuildInfo `json:"latest,omitempty"`
    // configs and other fields intentionally omitted for simplicity
}

// PostBuild calls POST /api/v2/build (create or lookup)
func (c *MCJarsClient) PostBuild(ctx context.Context, reqBody *BuildRequest) (*PostBuildResponse, error) {
    payload, err := json.Marshal(reqBody)
    if err != nil {
        return nil, err
    }
    url := c.baseURL + "/api/v2/build"
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
    if err != nil {
        return nil, err
    }
    req.Header.Set("Content-Type", "application/json")
    resp, err := c.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    // Accept 200 and 207 as valid successful responses per API surface
    if resp.StatusCode != http.StatusOK && resp.StatusCode != 207 {
        return nil, fmt.Errorf("mcjars: unexpected status %d", resp.StatusCode)
    }

    var out PostBuildResponse
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, err
    }
    return &out, nil
}

// GetBuildsByType calls GET /api/v2/builds/{type}
func (c *MCJarsClient) GetBuildsByType(ctx context.Context, typ string) (map[string]json.RawMessage, error) {
    url := fmt.Sprintf("%s/api/v2/builds/%s", c.baseURL, typ)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, err
    }
    resp, err := c.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("mcjars: unexpected status %d", resp.StatusCode)
    }
    var out struct {
        Success bool                            `json:"success"`
        Builds  map[string]json.RawMessage `json:"builds"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, err
    }
    return out.Builds, nil
}

// GetTypes calls GET /api/v2/types
func (c *MCJarsClient) GetTypes(ctx context.Context) (*TypesResponse, error) {
    url := c.baseURL + "/api/v2/types"
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, err
    }
    resp, err := c.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("mcjars: unexpected status %d", resp.StatusCode)
    }
    var tr TypesResponse
    if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
        return nil, err
    }
    return &tr, nil
}

// GetV1Build calls GET /api/v1/build/{build}
func (c *MCJarsClient) GetV1Build(ctx context.Context, build string) (*PostBuildResponse, error) {
    url := fmt.Sprintf("%s/api/v1/build/%s", c.baseURL, build)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, err
    }
    resp, err := c.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusNotFound {
        return nil, nil
    }
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("mcjars: unexpected status %d", resp.StatusCode)
    }

    var out PostBuildResponse
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, err
    }
    return &out, nil
}

// GetBuildsByTypeAndVersion calls GET /api/v2/builds/{type}/{version}
func (c *MCJarsClient) GetBuildsByTypeAndVersion(ctx context.Context, typ, version string) (json.RawMessage, error) {
    url := fmt.Sprintf("%s/api/v2/builds/%s/%s", c.baseURL, typ, version)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return nil, err
    }
    resp, err := c.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode == http.StatusNotFound {
        return nil, nil
    }
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("mcjars: unexpected status %d", resp.StatusCode)
    }
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, err
    }
    return json.RawMessage(body), nil
}

// GetScript fetches script text from /api/v1/script/{build}/{shell}?echo={true|false}
func (c *MCJarsClient) GetScript(ctx context.Context, build, shell string, echo bool) (string, error) {
    echoParam := "false"
    if echo {
        echoParam = "true"
    }
    url := fmt.Sprintf("%s/api/v1/script/%s/%s?echo=%s", c.baseURL, build, shell, echoParam)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return "", err
    }
    resp, err := c.client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusNotFound {
        return "", nil
    }
    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("mcjars: unexpected status %d", resp.StatusCode)
    }
    b, err := io.ReadAll(resp.Body)
    if err != nil {
        return "", err
    }
    return string(b), nil
}