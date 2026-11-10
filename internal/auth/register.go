package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/jkraemer/redmine-cli/internal/output"
)

// ErrRegistrationUnsupported is returned by RegisterClient when the server
// publishes no OAuth authorization server metadata (RFC 8414) or the
// metadata names no registration endpoint (RFC 7591). Stock Redmine is in
// this case; an OAuth application then has to be registered by hand.
var ErrRegistrationUnsupported = errors.New("server does not support OAuth dynamic client registration")

// serverMetadata is the subset of RFC 8414 authorization server metadata
// used for client registration.
type serverMetadata struct {
	Issuer               string `json:"issuer"`
	RegistrationEndpoint string `json:"registration_endpoint"`
}

// registrationResponse is the subset of an RFC 7591 registration response
// (or error response) the CLI uses. Any client_secret in the response is
// deliberately ignored: the CLI registers as a public client and uses PKCE.
type registrationResponse struct {
	ClientID  string `json:"client_id"`
	Error     string `json:"error"`
	ErrorDesc string `json:"error_description"`
}

// RegisterClient discovers the authorization server metadata for the
// Redmine at baseURL (RFC 8414) and registers a public OAuth client named
// clientName at its registration endpoint (RFC 7591), using the
// out-of-band redirect URI. It returns the issued client_id.
func RegisterClient(baseURL, clientName string) (string, error) {
	md, err := discoverMetadata(baseURL)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(map[string]any{
		"client_name":                clientName,
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Post(md.RegistrationEndpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("client registration: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var rr registrationResponse
	if err := json.Unmarshal(respBody, &rr); err != nil {
		return "", fmt.Errorf("decode registration response (status %d): %w", resp.StatusCode, err)
	}
	// Server-controlled strings headed for stderr; see Exchange.
	if rr.Error != "" {
		return "", fmt.Errorf("client registration error %s: %s", output.SanitizeForTerminal(rr.Error), output.SanitizeForTerminal(rr.ErrorDesc))
	}
	if rr.ClientID == "" {
		return "", fmt.Errorf("no client_id in registration response (status %d): %s", resp.StatusCode, output.SanitizeForTerminal(string(respBody)))
	}
	return rr.ClientID, nil
}

// discoverMetadata fetches the RFC 8414 metadata for the issuer baseURL and
// checks that it is usable for registration.
func discoverMetadata(baseURL string) (*serverMetadata, error) {
	issuer := strings.TrimRight(baseURL, "/")
	metadataURL, err := metadataURL(issuer)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Get(metadataURL)
	if err != nil {
		return nil, fmt.Errorf("fetching OAuth server metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w (%s returned status %d)", ErrRegistrationUnsupported, metadataURL, resp.StatusCode)
	}
	var md serverMetadata
	if err := json.NewDecoder(resp.Body).Decode(&md); err != nil {
		return nil, fmt.Errorf("%w (%s is not OAuth server metadata)", ErrRegistrationUnsupported, metadataURL)
	}
	// RFC 8414 §3.3: metadata whose issuer differs from the one it was
	// requested for must not be used.
	if md.Issuer != issuer {
		return nil, fmt.Errorf("OAuth server metadata issuer %q does not match %q", output.SanitizeForTerminal(md.Issuer), issuer)
	}
	if md.RegistrationEndpoint == "" {
		return nil, fmt.Errorf("%w (metadata has no registration_endpoint)", ErrRegistrationUnsupported)
	}
	return &md, nil
}

// metadataURL builds the RFC 8414 §3.1 metadata URL for issuer: the
// well-known segment goes between the host and any path component.
func metadataURL(issuer string) (string, error) {
	u, err := url.Parse(issuer)
	if err != nil {
		return "", err
	}
	u.Path = "/.well-known/oauth-authorization-server" + u.Path
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
