# redmine-cli

Agent-friendly CLI for the Redmine/Planio API.

## Build

    make build

## Configure

### Option 1: API Key (simplest)

Set env vars:

    export REDMINE_URL=https://your.redmine.example
    export REDMINE_API_KEY=...

Or create `~/.config/redmine-cli/config.toml`:

    url = "https://your.redmine.example"
    api_key = "..."
    default_format = "markdown"   # optional: json (default) or markdown

Note that with an API key, the CLI will have the exact same permissions as the
user whose key is used, and there is no way to limit them. If you want to have
more control over the permissions granted to the CLI, read on and use OAuth 2.0
instead, or create a dedicated "bot" user account in Redmine and use its API key.

### Option 2: OAuth 2.0 (headless / agent use)

#### 1. Register an OAuth application in Redmine

Go to **Administration → Applications** and create a new application:

| Field | Value |
|---|---|
| **Name** | `redmine-cli` (or anything you like) |
| **Redirect URI** | `urn:ietf:wg:oauth:2.0:oob` |
| **Scopes** | Select the maximum set of permissions you want the CLI to have |

Copy the **Client ID** and **Client Secret** that are shown after saving.

#### 2. Add the client credentials to your config

    oauth_client_id = "your-client-id-here"
    oauth_client_secret = "your-client-id-here"

Client ID and secret can also be set via the environment:

    export REDMINE_OAUTH_CLIENT_ID=your-client-id-here
    export REDMINE_OAUTH_CLIENT_SECRET=your-client-secret

#### 3. Authenticate

    redmine-cli auth login

This will print an authorization URL. Open it in your browser, approve the
application, and copy the authorization code shown by Redmine. Paste it at
the prompt. The token is stored inside the config file itself, under a
`[token]` section (mode 0600), and refreshed automatically.

    redmine-cli auth status   # check current auth method, expiry, granted scope
    redmine-cli auth logout   # revoke and remove stored token

#### Configuring requested scopes

By default, Redmine grants only its minimal default scope set, which limits the
client to few read-only operations like listing projects. To request specific
scopes, set `oauth_scopes` in your config:

    oauth_scopes = ["view_project", "view_issues", "edit_issues",
                    "view_wiki_pages", "edit_wiki_pages", "log_time"]

Or via the environment (space-separated, RFC 6749 §3.3):

    export REDMINE_OAUTH_SCOPES="view_project view_issues edit_issues"

`redmine-cli` will join them and send `scope=<space-separated>` on the
authorization request. The granted scope shows up in `auth status` and is
preserved across token refreshes. See "Reference: Common OAuth scopes"
below for known values.

#### Granting all scopes (admin workaround)

If you don't want to enumerate scopes manually — or your Redmine version
doesn't honour the `scope` parameter — and you are a Redmine
administrator, you can have Redmine grant the application's full
permitted scope set instead:

1. In Redmine, create the OAuth application and note the **Client ID** and
   **Client Secret**. Put them in the `redmine-cli` config file or environment
as described above.
2. Run `redmine-cli auth login`, but **do not** open the URL it prints.
3. Instead, in the Redmine Administration / Applications table, click the name
   of the application you just registered, and then click **Authorize** in the
   table at the bottom. This requests the maximum set of scopes the application is
   permitted to use (that is, the set of permissions that were selected during the
   initial creation of the application record), and after you grant access, an
   authorization code will be shown.
4. Paste that code at the `redmine-cli` prompt.

The resulting token will carry the full scope set, and refresh tokens will
preserve it.

#### Reference: OAuth scopes / permissions

The following Redmine permissions are currently used by the CLI. You can grant
the application only a subset of these, functionality will always be limited to
the union of application permissions and the permissions, the authorizing user
actually has in a given project.

- `view_project`
- `search_project`
- `view_issues`
- `add_issues`
- `edit_issues`
- `edit_own_issues`
- `add_issue_notes`
- `edit_issue_notes`
- `edit_own_issue_notes`
- `view_wiki_pages`
- `edit_wiki_pages`
- `view_time_entries`
- `log_time`

### Multiple instances / config files

To run `redmine-cli` against more than one Redmine instance, you can use a tool
like `direnv` for per-working directory environment variables, or pass
`--config <path>` (`-c`) on any command:

    redmine-cli --config ~/.config/redmine-cli/projA.toml issues list --project foo
    redmine-cli -c ~/.config/redmine-cli/projB.toml issues list --project bar

Each config file holds its own URL, credentials, and — for OAuth — its
own `[token]` section. Tokens are isolated per file: a custom config
never reads or writes the default instance's token, and vice versa.

Note that token refreshes rewrite the TOML file, and the encoder does
**not** preserve comments. Don't put hand-written annotations you care
about into a config file the CLI will write to.

When `--config` is omitted, the default
`$XDG_CONFIG_HOME/redmine-cli/config.toml` (falling back to
`~/.config/redmine-cli/config.toml`) is used.

## Use

    ./redmine-cli projects list
    ./redmine-cli --config ~/.config/redmine-cli/projA.toml projects list
    ./redmine-cli issues list --project myproj --status open --limit 10
    ./redmine-cli issues get 1459
    ./redmine-cli issues create --project myproj --tracker 1 --subject "Bug" --attach screenshot.png --confirm
    ./redmine-cli issues update 1459 --attach patch.diff --notes "see attached" --confirm
    ./redmine-cli attachments download 42
    ./redmine-cli wiki list --project myproj
    ./redmine-cli wiki get Architecture --project myproj
    ./redmine-cli wiki put MyPage --project myproj --text-file draft.textile --confirm
    ./redmine-cli wiki put MyPage --project myproj --text-file draft.textile --attach diagram.png --confirm

See `./redmine-cli --agent --help` for machine-readable help.

## License

Copyright (C) 2026 Jens Kraemer

This program is free software: you can redistribute it and/or modify it
under the terms of the GNU General Public License as published by the
Free Software Foundation, either version 3 of the License, or (at your
option) any later version. See [LICENSE](LICENSE) for the full text.
