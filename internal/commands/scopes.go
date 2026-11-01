package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
)

// requiredScopes maps a command path to the Redmine OAuth scopes the
// command's API endpoint is authorized against. Redmine's OAuth scopes are
// its permission names: config/initializers/30-redmine.rb registers
// Redmine::AccessControl.permissions as Doorkeeper's optional scopes, and
// User#allowed_to? checks the token's scope against the permission that
// covers the requested controller action (lib/redmine/preparation.rb).
//
// Entries list the permission that grants the endpoint for a regular
// member. Endpoints that need no permission (public enumerations, users
// me), admin-only endpoints (users list, custom-fields list), and endpoints
// whose permission depends on the container (attachments download) are
// left out; a 403 on those just reports the granted scope.
var requiredScopes = map[string][]string{
	"redmine-cli issues list":            {"view_issues"},
	"redmine-cli issues get":             {"view_issues"},
	"redmine-cli issues create":          {"add_issues"},
	"redmine-cli issues update":          {"edit_issues"},
	"redmine-cli issues watchers add":    {"add_issue_watchers"},
	"redmine-cli issues watchers remove": {"delete_issue_watchers"},
	"redmine-cli categories list":        {"view_issues"},
	"redmine-cli queries list":           {"view_issues"},
	"redmine-cli wiki list":              {"view_wiki_pages"},
	"redmine-cli wiki get":               {"view_wiki_pages"},
	"redmine-cli wiki put":               {"edit_wiki_pages"},
	"redmine-cli time log":               {"log_time"},
	"redmine-cli search":                 {"search_project"},
}

// decorateForbidden appends the OAuth token's granted scope, and the scope
// the command needs when known, to a 403 error. A bare 403 cannot be told
// apart from a missing role permission otherwise. The original error is
// wrapped, so exitCodeFor still maps it to 4 and errors.As still finds the
// *api.Error. Errors from other statuses, and 403s under API-key auth
// (where no scope exists), are returned unchanged.
func decorateForbidden(rc *runCtx, cmd *cobra.Command, err error) error {
	if err == nil || rc.authMethod != "oauth" {
		return err
	}
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 403 {
		return err
	}
	scope := rc.oauthScope
	if scope == "" {
		scope = "(none reported)"
	} else {
		scope = fmt.Sprintf("%q", scope)
	}
	needs := ""
	if s, ok := requiredScopes[cmd.CommandPath()]; ok {
		needs = fmt.Sprintf(" (this command needs: %s)", strings.Join(s, ", "))
	}
	return fmt.Errorf("%w; OAuth token scope: %s%s — re-run auth login with the missing scope in oauth_scopes", err, scope, needs)
}

// wrapRunE installs decorateForbidden around the RunE of cmd and every
// descendant, so a 403 is annotated at one place regardless of the command.
func wrapRunE(rc *runCtx, cmd *cobra.Command) {
	if run := cmd.RunE; run != nil {
		cmd.RunE = func(c *cobra.Command, args []string) error {
			return decorateForbidden(rc, c, run(c, args))
		}
	}
	for _, sub := range cmd.Commands() {
		wrapRunE(rc, sub)
	}
}
