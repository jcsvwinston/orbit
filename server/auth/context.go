package auth

import "context"

// identityKey is the private context key for the authenticated Identity.
type identityKey struct{}

// WithIdentity returns a context carrying the authenticated identity.
// The UI middleware calls this after a successful auth so downstream
// Connect handlers (audit, manage) can attribute actions to an operator.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFromContext returns the authenticated identity, or a zero
// Identity (Subject "") when the context carries none.
func IdentityFromContext(ctx context.Context) Identity {
	if ctx == nil {
		return Identity{}
	}
	id, _ := ctx.Value(identityKey{}).(Identity)
	return id
}
