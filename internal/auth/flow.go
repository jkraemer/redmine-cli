package auth

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jkraemer/redmine-cli/internal/output"
)

const redirectURI = "urn:ietf:wg:oauth:2.0:oob"

// httpClient is used for the token endpoint calls. Unlike the API client's
// per-phase timeouts, a single overall timeout is fine here: token responses
// are small, and a refresh runs implicitly before most commands, so an
// unresponsive server must never hang the CLI indefinitely.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// AuthorizeURL builds the authorization URL the user must open in their browser.
// When pkce is false (confidential client), the code_challenge params are omitted.
// When scopes is non-empty, they are joined with spaces and sent as the "scope"
// query parameter (RFC 6749 §3.3); otherwise the parameter is omitted and the
// server falls back to its default scope set.
func AuthorizeURL(baseURL, clientID, verifier string, pkce bool, scopes []string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	u.Path = "/oauth/authorize"
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	if pkce {
		q.Set("code_challenge", Challenge(verifier))
		q.Set("code_challenge_method", "S256")
	}
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// tokenResponse is the raw JSON from /oauth/token.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// Exchange trades the authorization code for a token. When clientSecret is
// non-empty the request uses the confidential client flow (secret in body, no
// code_verifier); otherwise the public/PKCE flow is used.
func Exchange(baseURL, clientID, clientSecret, code, verifier string) (*Token, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/oauth/token"
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"client_id":    {clientID},
		"code":         {code},
		"redirect_uri": {redirectURI},
	}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	} else {
		form.Set("code_verifier", verifier)
	}
	resp, err := httpClient.PostForm(endpoint, form)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	// error/error_description (and the raw body excerpt below) are
	// server-controlled and end up on stderr; strip terminal control bytes,
	// matching the policy applied to api.Error bodies.
	if tr.Error != "" {
		return nil, fmt.Errorf("token error %s: %s", output.SanitizeForTerminal(tr.Error), output.SanitizeForTerminal(tr.ErrorDesc))
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("no access_token in response (status %d): %s", resp.StatusCode, output.SanitizeForTerminal(string(body)))
	}
	t := &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
		Scope:        tr.Scope,
	}
	if tr.ExpiresIn > 0 {
		t.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return t, nil
}

// Refresh uses the refresh_token to obtain a new access token. When
// clientSecret is non-empty it is included in the POST body (confidential
// client).
func Refresh(baseURL, clientID, clientSecret, refreshToken string) (*Token, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/oauth/token"
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	resp, err := httpClient.PostForm(endpoint, form)
	if err != nil {
		return nil, fmt.Errorf("token refresh: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}
	// Server-controlled strings headed for stderr; see Exchange.
	if tr.Error != "" {
		return nil, fmt.Errorf("refresh error %s: %s", output.SanitizeForTerminal(tr.Error), output.SanitizeForTerminal(tr.ErrorDesc))
	}
	t := &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: refreshToken,
		TokenType:    tr.TokenType,
		Scope:        tr.Scope,
	}
	if tr.RefreshToken != "" {
		t.RefreshToken = tr.RefreshToken
	}
	if tr.ExpiresIn > 0 {
		t.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return t, nil
}

// RefreshWithScope is like Refresh but preserves priorScope on the returned
// Token when the server response omits the "scope" field. Most refresh
// responses don't echo scope, so callers should pass the previously stored
// scope here to keep `auth status` informative across refreshes.
func RefreshWithScope(baseURL, clientID, clientSecret, refreshToken, priorScope string) (*Token, error) {
	t, err := Refresh(baseURL, clientID, clientSecret, refreshToken)
	if err != nil {
		return nil, err
	}
	if t.Scope == "" {
		t.Scope = priorScope
	}
	return t, nil
}

// PromptCode prints the authorization URL and reads the code from r.
func PromptCode(out io.Writer, in io.Reader, authURL string) (string, error) {
	fmt.Fprintf(out, "\nOpen this URL in your browser:\n\n  %s\n\n", authURL)
	fmt.Fprintf(out, "Paste the authorization code shown by Redmine: ")
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return "", fmt.Errorf("no code entered")
	}
	code := strings.TrimSpace(scanner.Text())
	if code == "" {
		return "", fmt.Errorf("empty authorization code")
	}
	return code, nil
}
