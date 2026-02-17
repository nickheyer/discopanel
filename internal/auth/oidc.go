package auth

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/internal/config"
	"github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
	"golang.org/x/oauth2"
)

type OIDCHandler struct {
	manager      *Manager
	store        *db.Store
	config       *config.OIDCConfig
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Config *oauth2.Config
	httpClient   *http.Client
	log          *logger.Logger
}

func NewOIDCHandler(manager *Manager, store *db.Store, cfg *config.OIDCConfig, log *logger.Logger) (*OIDCHandler, error) {
	if !cfg.Enabled {
		return &OIDCHandler{
			manager: manager,
			store:   store,
			config:  cfg,
			log:     log,
		}, nil
	}

	var httpClient *http.Client
	if cfg.SkipTLSVerify {
		httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
		log.Warn("OIDC: TLS verification disabled")
	}

	ctx := context.Background()
	if httpClient != nil {
		ctx = oidc.ClientContext(ctx, httpClient)
	}
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURI)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	oauth2Config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}

	return &OIDCHandler{
		manager:      manager,
		store:        store,
		config:       cfg,
		provider:     provider,
		verifier:     verifier,
		oauth2Config: oauth2Config,
		httpClient:   httpClient,
		log:          log,
	}, nil
}

func (h *OIDCHandler) IsEnabled() bool {
	return h.config.Enabled && h.provider != nil
}

func (h *OIDCHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if !h.IsEnabled() {
		http.Error(w, "OIDC is not enabled", http.StatusBadRequest)
		return
	}

	state, err := generateState()
	if err != nil {
		http.Error(w, "Failed to generate state", http.StatusInternalServerError)
		return
	}

	// Store state in cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    state,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, h.oauth2Config.AuthCodeURL(state), http.StatusFound)
}

func (h *OIDCHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	if !h.IsEnabled() {
		http.Error(w, "OIDC is not enabled", http.StatusBadRequest)
		return
	}

	// Verify state
	stateCookie, err := r.Cookie("oidc_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	// Exchange code for token
	ctx := r.Context()
	if h.httpClient != nil {
		ctx = oidc.ClientContext(ctx, h.httpClient)
	}
	oauth2Token, err := h.oauth2Config.Exchange(ctx, r.URL.Query().Get("code"))
	if err != nil {
		h.log.Error("OIDC: failed to exchange code for token: %v", err)
		http.Error(w, "Failed to exchange code for token", http.StatusInternalServerError)
		return
	}

	// Extract ID token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		h.log.Error("OIDC: no id_token in token response")
		http.Error(w, "No id_token in response", http.StatusInternalServerError)
		return
	}

	// Verify ID token
	idToken, err := h.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		h.log.Error("OIDC: failed to verify ID token: %v", err)
		http.Error(w, "Failed to verify ID token", http.StatusInternalServerError)
		return
	}

	// Extract claims from ID token
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		h.log.Error("OIDC: failed to parse claims: %v", err)
		http.Error(w, "Failed to parse claims", http.StatusInternalServerError)
		return
	}

	// Fetch UserInfo - some oidc sets role/groups here
	tokenSource := h.oauth2Config.TokenSource(ctx, oauth2Token)
	userInfo, err := h.provider.UserInfo(ctx, tokenSource)
	if err == nil {
		var uiClaims map[string]any
		if err := userInfo.Claims(&uiClaims); err == nil {
			for k, v := range uiClaims {
				if _, exists := claims[k]; !exists {
					claims[k] = v
				}
			}
		}
	}

	// Extract user info from claims
	sub := idToken.Subject
	email, _ := claims["email"].(string)
	username, _ := claims["preferred_username"].(string)
	if username == "" {
		username, _ = claims["name"].(string)
	}
	if username == "" {
		username = email
	}
	if username == "" {
		username = sub
	}

	user, err := h.findOrCreateOIDCUser(ctx, sub, username, email)
	if err != nil {
		h.log.Error("OIDC: failed to find or create user (sub=%s, username=%s): %v", sub, username, err)
		http.Error(w, "Failed to authenticate user", http.StatusInternalServerError)
		return
	}

	// Map OIDC claims to roles
	h.mapClaimsToRoles(ctx, user.ID, claims)

	// Get user roles
	roleNames, err := h.store.GetUserRoleNames(ctx, user.ID)
	if err != nil {
		roleNames = []string{}
	}

	// Generate session token
	expiresAt := time.Now().Add(time.Duration(h.manager.config.SessionTimeout) * time.Second)
	token, err := h.manager.generateJWT(user.ID, user.Username, roleNames, expiresAt)
	if err != nil {
		h.log.Error("OIDC: failed to generate JWT: %v", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Create session
	session := &db.Session{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: expiresAt,
	}
	if err := h.store.CreateSession(ctx, session); err != nil {
		h.log.Error("OIDC: failed to create session: %v", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	h.log.Info("OIDC: user %s authenticated successfully", user.Username)

	// Redirect to frontend with token in query param
	http.Redirect(w, r, fmt.Sprintf("/login?token=%s", token), http.StatusFound)
}

// findOrCreateOIDCUser looks up a user by OIDC subject (returning user),
// or creates a new OIDC user. Local users with the same username are not
// affected — the composite unique constraint (username, auth_provider)
// allows both to coexist.
func (h *OIDCHandler) findOrCreateOIDCUser(ctx context.Context, sub, username, email string) (*db.User, error) {
	// Step 1: try to find by OIDC subject (returning user)
	if user, err := h.store.GetUserByOIDCSubject(ctx, sub); err == nil {
		if !user.IsActive {
			return nil, ErrUserNotActive
		}
		// Update email/last login on returning users
		if email != "" {
			user.Email = &email
		}
		now := time.Now()
		user.LastLogin = &now
		_ = h.store.UpdateUser(ctx, user)
		return user, nil
	}

	// Step 2: create a new OIDC user
	var emailPtr *string
	if email != "" {
		emailPtr = &email
	}
	user := &db.User{
		ID:           uuid.New().String(),
		Username:     username,
		Email:        emailPtr,
		AuthProvider: "oidc",
		OIDCSubject:  sub,
		OIDCIssuer:   h.config.IssuerURI,
		IsActive:     true,
	}
	if err := h.store.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create OIDC user: %w", err)
	}

	// Assign default roles to new user
	defaultRoles, _ := h.store.GetDefaultRoles(ctx)
	for _, role := range defaultRoles {
		_ = h.store.AssignRole(ctx, user.ID, role.Name, "oidc")
	}

	h.log.Info("OIDC: created new user %s", user.Username)
	return user, nil
}

func (h *OIDCHandler) mapClaimsToRoles(ctx context.Context, userID string, claims map[string]any) {
	if h.config.RoleClaim == "" {
		return
	}

	// Extract groups/roles from claims
	var claimValues []string
	claimValue, ok := claims[h.config.RoleClaim]
	if !ok {
		h.log.Warn("OIDC: role claim %q not found in token claims", h.config.RoleClaim)
		return
	}
	switch v := claimValue.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				claimValues = append(claimValues, s)
			}
		}
	case string:
		// Try JSON array
		var arr []string
		if err := json.Unmarshal([]byte(v), &arr); err == nil {
			claimValues = arr
		} else {
			claimValues = []string{v}
		}
	}

	// Map OIDC claims to local roles + use role mappings if provided in cfg
	for _, claimVal := range claimValues {
		if len(h.config.RoleMapping) > 0 {
			if localRole, ok := h.config.RoleMapping[claimVal]; ok {
				_ = h.store.AssignRole(ctx, userID, localRole, "oidc")
			}
		} else {
			_ = h.store.AssignRole(ctx, userID, claimVal, "oidc")
		}
	}
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
