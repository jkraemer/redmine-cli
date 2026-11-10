package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/auth"
	"github.com/jkraemer/redmine-cli/internal/config"
)

func newAuthCmd(rc *runCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication (OAuth 2.0)",
	}
	cmd.AddCommand(newAuthLoginCmd(rc))
	cmd.AddCommand(newAuthLogoutCmd(rc))
	cmd.AddCommand(newAuthStatusCmd(rc))
	return cmd
}

func newAuthLoginCmd(rc *runCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate via OAuth 2.0 (registers a client if none is configured, prints URL, prompts for code)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.LoadForLogin(rc.configPath)
			if err != nil {
				return err
			}
			if cfg.OAuthClientID == "" {
				if err := registerClient(rc, cfg); err != nil {
					return err
				}
			}
			verifier, err := auth.GenerateVerifier()
			if err != nil {
				return fmt.Errorf("generating PKCE verifier: %w", err)
			}
			pkce := cfg.OAuthClientSecret == ""
			authURL, err := auth.AuthorizeURL(cfg.URL, cfg.OAuthClientID, verifier, pkce, cfg.OAuthScopes)
			if err != nil {
				return fmt.Errorf("building auth URL: %w", err)
			}
			code, err := auth.PromptCode(rc.out, cmd.InOrStdin(), authURL)
			if err != nil {
				return err
			}
			tok, err := auth.Exchange(cfg.URL, cfg.OAuthClientID, cfg.OAuthClientSecret, code, verifier)
			if err != nil {
				return fmt.Errorf("token exchange: %w", err)
			}
			if err := cfg.SaveToken(tok); err != nil {
				return err
			}
			fmt.Fprintln(rc.out, "Authenticated successfully. Token saved.")
			return nil
		},
	}
}

// registerClient obtains an OAuth client_id for cfg by dynamic client
// registration and saves it to the config file, which switches the config
// to OAuth even if it also holds an api_key. Unless oauth_scopes is already
// configured, the scopes the client was registered with are saved as
// oauth_scopes too, so login requests them instead of Redmine's minimal
// default scope.
func registerClient(rc *runCtx, cfg *config.Config) error {
	reg, err := auth.RegisterClient(cfg.URL, "redmine-cli")
	if err != nil {
		return fmt.Errorf("oauth_client_id not configured and registering a client failed: %w — register an OAuth application in Redmine and set oauth_client_id (see README)", err)
	}
	var scopes []string
	if len(cfg.OAuthScopes) == 0 {
		scopes = reg.Scopes
	}
	if err := cfg.SaveRegistration(reg.ClientID, scopes); err != nil {
		return err
	}
	fmt.Fprintf(rc.out, "Registered OAuth client %s; saved as oauth_client_id in %s.\n", reg.ClientID, cfg.Path)
	if len(scopes) > 0 {
		fmt.Fprintf(rc.out, "Saved its scopes as oauth_scopes: %s\n", strings.Join(scopes, " "))
	}
	if cfg.APIKey != "" {
		fmt.Fprintln(rc.out, "This config now uses OAuth; its api_key is no longer used.")
	}
	return nil
}

func newAuthLogoutCmd(rc *runCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored OAuth token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// TODO: if Load fails on a half-set-up install (missing URL),
			// fall back to a path-only resolver so logout still clears
			// the [token] section without a fully-valid config.
			cfg, err := config.Load(rc.configPath)
			if err != nil {
				return err
			}
			if err := cfg.DeleteToken(); err != nil {
				return err
			}
			fmt.Fprintln(rc.out, "Logged out.")
			return nil
		},
	}
}

func newAuthStatusCmd(rc *runCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(rc.configPath)
			if err != nil {
				return err
			}
			if cfg.AuthMethod() == "oauth" {
				tok := cfg.Token
				if tok == nil {
					fmt.Fprintln(rc.out, "Auth method: oauth\nStatus:      not logged in (run: redmine-cli auth login)")
					return nil
				}
				expiry := "none (non-expiring)"
				if !tok.ExpiresAt.IsZero() {
					expiry = tok.ExpiresAt.UTC().Format("2006-01-02 15:04:05 UTC")
					if tok.Expired() {
						expiry += " (EXPIRED)"
					}
				}
				scope := tok.Scope
				if scope == "" {
					scope = "(none reported)"
				}
				fmt.Fprintf(rc.out, "Auth method: oauth\nStatus:      logged in\nExpires:     %s\nScope:       %s\n", expiry, scope)
			} else {
				fmt.Fprintln(rc.out, "Auth method: api_key")
			}
			return nil
		},
	}
}
