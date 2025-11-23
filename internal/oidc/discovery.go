package oidc

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/nickheyer/discopanel/internal/cache"
	"github.com/nickheyer/discopanel/internal/config"
	"github.com/nickheyer/discopanel/pkg/logger"
)

const (
	// Cache key for the OIDC provider
	oidcProviderCacheKey = "oidc_provider"
	// Cache TTL - OpenID configuration rarely changes, cache for 24 hours
	oidcProviderCacheTTL = 24 * time.Hour
)

// DiscoveryService handles fetching and caching OpenID Connect configuration
type DiscoveryService struct {
	config *config.Config
	cache  *cache.TTLCache[string, *oidc.Provider]
	log    *logger.Logger
}

// NewDiscoveryService creates a new OIDC discovery service
func NewDiscoveryService(cfg *config.Config, log *logger.Logger) *DiscoveryService {
	return &DiscoveryService{
		config: cfg,
		cache:  cache.NewTTLCache[string, *oidc.Provider](),
		log:    log,
	}
}

// GetProvider retrieves the OIDC provider from cache or fetches it if not available
func (s *DiscoveryService) GetProvider(ctx context.Context) (*oidc.Provider, error) {
	// Check if OIDC is configured
	if s.config.OIDC.IssuerURI == "" {
		return nil, fmt.Errorf("OIDC issuer URI not configured")
	}

	// Try to get from cache first
	if provider, ok := s.cache.Get(oidcProviderCacheKey); ok {
		s.log.Debug("Using cached OIDC provider configuration")
		return provider, nil
	}

	// Cache miss - fetch from issuer
	s.log.Info("Fetching OpenID configuration from %s", s.config.OIDC.IssuerURI)
	provider, err := oidc.NewProvider(ctx, s.config.OIDC.IssuerURI)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OpenID configuration: %w", err)
	}

	// Store in cache
	s.cache.Set(oidcProviderCacheKey, provider, oidcProviderCacheTTL)
	s.log.Info("Successfully fetched and cached OpenID configuration")

	return provider, nil
}

// LoadProviderOnStartup fetches and caches the OIDC provider configuration on startup
func (s *DiscoveryService) LoadProviderOnStartup(ctx context.Context) error {
	// Only load if OIDC is configured
	if s.config.OIDC.IssuerURI == "" {
		s.log.Debug("OIDC not configured, skipping provider discovery")
		return nil
	}

	// Check if client ID and secret are also configured
	if s.config.OIDC.ClientID == "" || s.config.OIDC.ClientSecret == "" {
		s.log.Warn("OIDC issuer URI is configured but client credentials are missing")
		return nil
	}

	// Fetch and cache the provider
	_, err := s.GetProvider(ctx)
	if err != nil {
		return fmt.Errorf("failed to load OIDC provider on startup: %w", err)
	}

	return nil
}

// ClearCache clears the cached OIDC provider
func (s *DiscoveryService) ClearCache() {
	s.cache.Delete(oidcProviderCacheKey)
	s.log.Debug("Cleared OIDC provider cache")
}
