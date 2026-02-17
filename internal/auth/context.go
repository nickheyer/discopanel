package auth

import "context"

type contextKey string

const UserContextKey contextKey = "authenticated_user"

// AuthenticatedUser represents a validated user in context
type AuthenticatedUser struct {
	ID       string
	Username string
	Email    string
	Roles    []string
	Provider string // "local" or "oidc"
}

// GetUserFromContext retrieves the authenticated user from context
func GetUserFromContext(ctx context.Context) *AuthenticatedUser {
	user, ok := ctx.Value(UserContextKey).(*AuthenticatedUser)
	if !ok {
		return nil
	}
	return user
}

// WithUser adds the authenticated user to context
func WithUser(ctx context.Context, user *AuthenticatedUser) context.Context {
	return context.WithValue(ctx, UserContextKey, user)
}
