package cloudflare

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	baseURL = "https://api.cloudflare.com/client/v4"
	defaultTimeout = 30 * time.Second
)

// Client represents a Cloudflare API client
type Client struct {
	accountID  string
	apiToken   string
	httpClient *http.Client
}

// NewClient creates a new Cloudflare API client
func NewClient(accountID, apiToken string) *Client {
	return &Client{
		accountID: accountID,
		apiToken:  apiToken,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// Zone represents a Cloudflare zone/domain
type Zone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Type   string `json:"type"`
}

// ZonesResponse represents the API response for listing zones
type ZonesResponse struct {
	Success bool   `json:"success"`
	Errors  []Error `json:"errors"`
	Result  []Zone  `json:"result"`
}

// Tunnel represents a Cloudflare tunnel
type Tunnel struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	DeletedAt   *time.Time `json:"deleted_at"`
	Connections []TunnelConnection `json:"connections"`
}

// TunnelConnection represents a tunnel connection
type TunnelConnection struct {
	ID              string    `json:"id"`
	ClientID        string    `json:"client_id"`
	ClientVersion   string    `json:"client_version"`
	OpenedAt        time.Time `json:"opened_at"`
	OriginIP        string    `json:"origin_ip"`
}

// TunnelResponse represents the API response for a single tunnel
type TunnelResponse struct {
	Success bool    `json:"success"`
	Errors  []Error `json:"errors"`
	Result  Tunnel  `json:"result"`
}

// TunnelsResponse represents the API response for listing tunnels
type TunnelsResponse struct {
	Success bool     `json:"success"`
	Errors  []Error  `json:"errors"`
	Result  []Tunnel `json:"result"`
}

// TunnelTokenResponse represents the API response for getting a tunnel token
type TunnelTokenResponse struct {
	Success bool    `json:"success"`
	Errors  []Error `json:"errors"`
	Result  string  `json:"result"` // The token is returned directly as a string
}

// TunnelConfiguration represents tunnel ingress configuration
type TunnelConfiguration struct {
	Config TunnelConfig `json:"config"`
}

// TunnelConfig contains the tunnel configuration details
type TunnelConfig struct {
	Ingress []IngressRule `json:"ingress"`
}

// IngressRule represents a tunnel ingress rule
type IngressRule struct {
	Hostname string  `json:"hostname,omitempty"`
	Service  string  `json:"service"`
	Path     string  `json:"path,omitempty"`
}

// DNSRecord represents a Cloudflare DNS record
type DNSRecord struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Proxied  bool   `json:"proxied"`
	TTL      int    `json:"ttl"`
	ZoneID   string `json:"zone_id"`
	ZoneName string `json:"zone_name"`
}

// DNSRecordResponse represents the API response for a DNS record
type DNSRecordResponse struct {
	Success bool      `json:"success"`
	Errors  []Error   `json:"errors"`
	Result  DNSRecord `json:"result"`
}

// DNSRecordsResponse represents the API response for listing DNS records
type DNSRecordsResponse struct {
	Success bool        `json:"success"`
	Errors  []Error     `json:"errors"`
	Result  []DNSRecord `json:"result"`
}

// Error represents a Cloudflare API error
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ListZones lists all zones accessible with the API token
func (c *Client) ListZones(ctx context.Context) ([]Zone, error) {
	req, err := c.newRequest(ctx, "GET", "/zones", nil)
	if err != nil {
		return nil, err
	}

	var resp ZonesResponse
	if err := c.doRequest(req, &resp); err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to list zones: %v", resp.Errors)
	}

	return resp.Result, nil
}

// ListTunnels lists all tunnels in the account
func (c *Client) ListTunnels(ctx context.Context) ([]Tunnel, error) {
	url := fmt.Sprintf("/accounts/%s/cfd_tunnel", c.accountID)
	req, err := c.newRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var resp TunnelsResponse
	if err := c.doRequest(req, &resp); err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to list tunnels: %v", resp.Errors)
	}

	return resp.Result, nil
}

// GetTunnel gets a tunnel by ID
func (c *Client) GetTunnel(ctx context.Context, tunnelID string) (*Tunnel, error) {
	url := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", c.accountID, tunnelID)
	req, err := c.newRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var resp TunnelResponse
	if err := c.doRequest(req, &resp); err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to get tunnel: %v", resp.Errors)
	}

	return &resp.Result, nil
}

// CreateTunnel creates a new Cloudflare tunnel
func (c *Client) CreateTunnel(ctx context.Context, name string) (*Tunnel, string, error) {
	// Generate tunnel secret
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, "", fmt.Errorf("failed to generate tunnel secret: %w", err)
	}
	tunnelSecret := base64.StdEncoding.EncodeToString(secretBytes)

	payload := map[string]interface{}{
		"name":          name,
		"tunnel_secret": tunnelSecret,
	}

	url := fmt.Sprintf("/accounts/%s/cfd_tunnel", c.accountID)
	req, err := c.newRequest(ctx, "POST", url, payload)
	if err != nil {
		return nil, "", err
	}

	var resp TunnelResponse
	if err := c.doRequest(req, &resp); err != nil {
		return nil, "", err
	}

	if !resp.Success {
		return nil, "", fmt.Errorf("failed to create tunnel: %v", resp.Errors)
	}

	return &resp.Result, tunnelSecret, nil
}

// DeleteTunnel deletes a tunnel
func (c *Client) DeleteTunnel(ctx context.Context, tunnelID string) error {
	url := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", c.accountID, tunnelID)
	req, err := c.newRequest(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	var resp struct {
		Success bool    `json:"success"`
		Errors  []Error `json:"errors"`
	}

	if err := c.doRequest(req, &resp); err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to delete tunnel: %v", resp.Errors)
	}

	return nil
}

// GetTunnelToken gets a token for running the tunnel
func (c *Client) GetTunnelToken(ctx context.Context, tunnelID string) (string, error) {
	url := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/token", c.accountID, tunnelID)
	req, err := c.newRequest(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	var resp TunnelTokenResponse
	if err := c.doRequest(req, &resp); err != nil {
		return "", err
	}

	if !resp.Success {
		return "", fmt.Errorf("failed to get tunnel token: %v", resp.Errors)
	}

	return resp.Result, nil // Token is directly in Result field as a string
}

// UpdateTunnelConfiguration updates the tunnel's ingress configuration
func (c *Client) UpdateTunnelConfiguration(ctx context.Context, tunnelID string, ingress []IngressRule) error {
	config := TunnelConfiguration{
		Config: TunnelConfig{
			Ingress: ingress,
		},
	}

	url := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/configurations", c.accountID, tunnelID)
	req, err := c.newRequest(ctx, "PUT", url, config)
	if err != nil {
		return err
	}

	var resp struct {
		Success bool    `json:"success"`
		Errors  []Error `json:"errors"`
	}

	if err := c.doRequest(req, &resp); err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to update tunnel configuration: %v", resp.Errors)
	}

	return nil
}

// CreateDNSRecord creates a CNAME record for the tunnel
func (c *Client) CreateDNSRecord(ctx context.Context, zoneID, hostname, tunnelID string) (*DNSRecord, error) {
	payload := map[string]interface{}{
		"type":    "CNAME",
		"name":    hostname,
		"content": fmt.Sprintf("%s.cfargotunnel.com", tunnelID),
		"proxied": true,
		"ttl":     1, // Auto TTL when proxied
	}

	url := fmt.Sprintf("/zones/%s/dns_records", zoneID)
	req, err := c.newRequest(ctx, "POST", url, payload)
	if err != nil {
		return nil, err
	}

	var resp DNSRecordResponse
	if err := c.doRequest(req, &resp); err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to create DNS record: %v", resp.Errors)
	}

	return &resp.Result, nil
}

// ListDNSRecords lists all DNS records for a zone
func (c *Client) ListDNSRecords(ctx context.Context, zoneID string, recordType string, content string) ([]DNSRecord, error) {
	url := fmt.Sprintf("/zones/%s/dns_records", zoneID)
	if recordType != "" {
		url += fmt.Sprintf("?type=%s", recordType)
		if content != "" {
			url += fmt.Sprintf("&content=%s", content)
		}
	} else if content != "" {
		url += fmt.Sprintf("?content=%s", content)
	}

	req, err := c.newRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var resp DNSRecordsResponse
	if err := c.doRequest(req, &resp); err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("failed to list DNS records: %v", resp.Errors)
	}

	return resp.Result, nil
}

// DeleteDNSRecord deletes a DNS record
func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	url := fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID)
	req, err := c.newRequest(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	var resp struct {
		Success bool    `json:"success"`
		Errors  []Error `json:"errors"`
	}

	if err := c.doRequest(req, &resp); err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to delete DNS record: %v", resp.Errors)
	}

	return nil
}

// newRequest creates a new HTTP request with authentication
func (c *Client) newRequest(ctx context.Context, method, path string, body interface{}) (*http.Request, error) {
	url := baseURL + path

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiToken))
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

// doRequest executes an HTTP request and decodes the response
func (c *Client) doRequest(req *http.Request, v interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}