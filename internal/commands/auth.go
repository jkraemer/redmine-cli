package commands

import (
	"fmt"
	"os"

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
		Short: "Authenticate via OAuth 2.0 (prints URL, prompts for code)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.OAuthClientID == "" {
				return fmt.Errorf("oauth_client_id not configured — set it in config.toml or REDMINE_OAUTH_CLIENT_ID")
			}
			verifier, err := auth.GenerateVerifier()
			if err != nil {
				return fmt.Errorf("generating PKCE verifier: %w", err)
			}
			pkce := cfg.OAuthClientSecret == ""
			authURL, err := auth.AuthorizeURL(cfg.URL, cfg.OAuthClientID, verifier, pkce)
			if err != nil {
				return fmt.Errorf("building auth URL: %w", err)
			}
			code, err := auth.PromptCode(rc.out, os.Stdin, authURL)
			if err != nil {
				return err
			}
			tok, err := auth.Exchange(cfg.URL, cfg.OAuthClientID, cfg.OAuthClientSecret, code, verifier)
			if err != nil {
				return fmt.Errorf("token exchange: %w", err)
			}
			if err := auth.SaveToken(tok); err != nil {
				return err
			}
			fmt.Fprintln(rc.out, "Authenticated successfully. Token saved.")
			return nil
		},
	}
}

func newAuthLogoutCmd(rc *runCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored OAuth token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := auth.DeleteToken(); err != nil {
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
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.AuthMethod() == "oauth" {
				tok, err := auth.LoadToken()
				if err != nil {
					return err
				}
				if tok == nil {
					fmt.Fprintln(rc.out, "Auth method: oauth\nStatus:      not logged in (run: redmine-cli auth login)")
					return nil
				}
				expiry := "none (non-expiring)"
				if !tok.ExpiresAt.IsZero() {
					expiry = tok.ExpiresAt.Format("2006-01-02 15:04:05 UTC")
					if tok.Expired() {
						expiry += " (EXPIRED)"
					}
				}
				fmt.Fprintf(rc.out, "Auth method: oauth\nStatus:      logged in\nExpires:     %s\n", expiry)
			} else {
				fmt.Fprintln(rc.out, "Auth method: api_key")
			}
			return nil
		},
	}
}
