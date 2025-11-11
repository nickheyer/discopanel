package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/nickheyer/discopanel/internal/db"
)

type contextKey string

const (
	UserContextKey contextKey = "user"
)

// Middleware provides authentication middleware for HTTP handlers
type Middleware struct {
	authManager *Manager
	store       *db.Store
}

// NewMiddleware creates a new authentication middleware
func NewMiddleware(authManager *Manager, store *db.Store) *Middleware {
	return &Middleware{
		authManager: authManager,
		store:       store,
	}
}

// RequireAuth middleware checks if authentication is enabled and validates the user
func (m *Middleware) RequireAuth(requiredRole db.UserRole) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if auth is enabled
			authConfig, _, err := m.store.GetAuthConfig(r.Context())
			if err != nil {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// If auth is disabled, allow unrestricted access
			if !authConfig.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Extract token from Authorization header
			token := extractToken(r)
			if token == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Validate session
			user, err := m.authManager.ValidateSession(r.Context(), token)
			if err != nil {
				http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
				return
			}

			// Check permission
			if !CheckPermission(user, requiredRole) {
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}

			// Add user to context
			ctx := context.WithValue(r.Context(), UserContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuth middleware checks authentication if present but doesn't require it
func (m *Middleware) OptionalAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if auth is enabled
			authConfig, _, err := m.store.GetAuthConfig(r.Context())
			if err != nil {
				// Continue without auth on error
				next.ServeHTTP(w, r)
				return
			}

			// If auth is disabled, continue without user
			if !authConfig.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Extract token from Authorization header
			token := extractToken(r)
			if token != "" {
				// Try to validate session
				user, err := m.authManager.ValidateSession(r.Context(), token)
				if err == nil && user != nil {
					// Add user to context if valid
					ctx := context.WithValue(r.Context(), UserContextKey, user)
					r = r.WithContext(ctx)
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CheckAuthStatus returns a middleware that adds auth status to response headers
func (m *Middleware) CheckAuthStatus() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if auth is enabled
			authConfig, _, err := m.store.GetAuthConfig(r.Context())
			if err != nil {
				w.Header().Set("X-Auth-Enabled", "error")
			} else if authConfig.Enabled {
				w.Header().Set("X-Auth-Enabled", "true")

				// Check if this is the first user setup
				userCount, _ := m.store.CountUsers(r.Context())
				if userCount == 0 {
					w.Header().Set("X-Auth-First-User", "true")
				}
			} else {
				w.Header().Set("X-Auth-Enabled", "false")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractToken extracts the JWT token from the Authorization header
func extractToken(r *http.Request) string {
	// Check Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		// Bearer token
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
	}

	// Check cookie as fallback
	cookie, err := r.Cookie("auth_token")
	if err == nil && cookie != nil {
		return cookie.Value
	}

	return ""
}

// GetUserFromContext retrieves the user from the request context
func GetUserFromContext(ctx context.Context) *db.User {
	user, ok := ctx.Value(UserContextKey).(*db.User)
	if !ok {
		return nil
	}
	return user
}
