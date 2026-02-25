package indexers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// HTTPClient wraps http.Client with common indexer request logic.
type HTTPClient struct {
	client       *http.Client
	userAgent    string
	indexer      string
	extraHeaders map[string]string
}

// NewHTTPClient creates a shared HTTP client for an indexer.
func NewHTTPClient(indexer string, userAgent string, extraHeaders map[string]string) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		userAgent:    userAgent,
		indexer:      indexer,
		extraHeaders: extraHeaders,
	}
}

// DoJSON performs a GET request, checks the status, and JSON-decodes into dest.
// It returns structured IndexerErrors for all failure modes.
func (h *HTTPClient) DoJSON(ctx context.Context, url string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return &IndexerError{Kind: ErrNetwork, Indexer: h.indexer, URL: url, Err: err}
	}

	req.Header.Set("Accept", "application/json")
	if h.userAgent != "" {
		req.Header.Set("User-Agent", h.userAgent)
	}
	for k, v := range h.extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return NewNetworkError(h.indexer, url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return NewAPIError(h.indexer, resp.StatusCode, url, string(bodyBytes))
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return NewDecodeError(h.indexer, url, err)
	}

	return nil
}
