package auth

import "time"

// Token holds the stored OAuth token.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	// Scope is the space-separated list of scopes the server reports as
	// granted on this token. May be empty if the server does not echo it.
	Scope string `json:"scope,omitempty"`
}

// Expired reports whether the access token has expired (with a 30s buffer).
func (t *Token) Expired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(30 * time.Second).After(t.ExpiresAt)
}
