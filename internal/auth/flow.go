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
)

const redirectURI = "urn:ietf:wg:oauth:2.0:oob"

// AuthorizeURL builds the authorization URL the user must open in their browser.
func AuthorizeURL(baseURL, clientID, verifier string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	u.Path = "/oauth/authorize"
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", Challenge(verifier))
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// tokenResponse is the raw JSON from /oauth/token.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// Exchange trades the authorization code for a token.
func Exchange(baseURL, clientID, code, verifier string) (*Token, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/oauth/token"
	resp, err := http.PostForm(endpoint, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("token error %s: %s", tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("no access_token in response (status %d): %s", resp.StatusCode, body)
	}
	t := &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
	}
	if tr.ExpiresIn > 0 {
		t.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return t, nil
}

// Refresh uses the refresh_token to obtain a new access token.
func Refresh(baseURL, clientID, refreshToken string) (*Token, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/oauth/token"
	resp, err := http.PostForm(endpoint, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		return nil, fmt.Errorf("token refresh: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("refresh error %s: %s", tr.Error, tr.ErrorDesc)
	}
	t := &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: refreshToken,
		TokenType:    tr.TokenType,
	}
	if tr.RefreshToken != "" {
		t.RefreshToken = tr.RefreshToken
	}
	if tr.ExpiresIn > 0 {
		t.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
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
