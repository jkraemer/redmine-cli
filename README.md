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

### Option 2: OAuth 2.0 (headless / agent use)

OAuth 2.0 uses the authorization-code + PKCE flow with the out-of-band (OOB)
redirect URI. This is the correct choice when running `redmine-cli` on a
headless server or remote VM where a browser callback listener is not feasible.

#### 1. Register an OAuth application in Redmine

Go to **Administration → OAuth Applications** (or your personal settings if
using a non-admin account) and create a new application:

| Field | Value |
|---|---|
| **Name** | `redmine-cli` (or anything you like) |
| **Redirect URI** | `urn:ietf:wg:oauth:2.0:oob` |
| **Confidential** | No (public client — PKCE, no secret required) |
| **Scopes** | Leave blank or select the scopes your Redmine version supports |

Copy the **Client ID** that is shown after saving.

#### 2. Add the Client ID to your config

    url = "https://your.redmine.example"
    oauth_client_id = "your-client-id-here"
    default_format = "markdown"

The `oauth_client_id` key can also be set via the environment:

    export REDMINE_OAUTH_CLIENT_ID=your-client-id-here
    export REDMINE_OAUTH_CLIENT_SECRET=your-client-secret  # only for confidential clients (see below)

#### Confidential client (older Redmine without PKCE support)

If your Redmine instance requires a client secret, add it to your config:

    oauth_client_id = "your-client-id"
    oauth_client_secret = "your-client-secret"

The CLI detects the secret automatically and switches to the confidential
client flow (no PKCE). The `auth login` / `logout` / `status` commands work
identically in both modes.

#### 3. Authenticate

    redmine-cli auth login

This will print an authorization URL. Open it in your browser, approve the
application, and copy the authorization code shown by Redmine. Paste it at
the prompt. The token is stored in `~/.config/redmine-cli/token.json`
(mode 0600) and refreshed automatically.

    redmine-cli auth status   # check current auth method, expiry, granted scope
    redmine-cli auth logout   # revoke and remove stored token

#### Configuring requested scopes

When `oauth_scopes` is unset, Redmine grants only its minimal default scope
set, which usually limits the client to read-only operations like listing
projects. To request specific scopes, set `oauth_scopes` in your config:

    oauth_scopes = ["view_project", "view_issues", "edit_issues",
                    "view_wiki_pages", "edit_wiki_pages", "log_time"]

Or via the environment (space-separated, RFC 6749 §3.3):

    export REDMINE_OAUTH_SCOPES="view_project view_issues edit_issues"

`redmine-cli` will join them and send `scope=<space-separated>` on the
authorization request. The granted scope shows up in `auth status` and is
preserved across token refreshes. See "Reference: Planio OAuth scopes"
below for known values.

#### Granting full scopes (admin workaround)

If you don't want to enumerate scopes manually — or your Redmine version
doesn't honour the `scope` parameter — and you are a Redmine
administrator, you can have Redmine grant the application's full
permitted scope set instead:

1. In Redmine, create the OAuth application and note the **Client ID** and
   (for confidential clients) **Client Secret**. Put them in the
   `redmine-cli` config file as described above.
2. Run `redmine-cli auth login`, but **do not** open the URL it prints.
3. Instead, in the Redmine admin UI, scroll to the table of registered OAuth
   applications and click **Authorize** next to your application. This
   requests the maximum set of scopes the application is permitted to use,
   grants access, and shows an authorization code.
4. Paste that code at the `redmine-cli` prompt.

The resulting token will carry the full scope set, and refresh tokens will
preserve it.

#### Reference: Planio OAuth scopes

For convenience, the scopes a recent Planio installation supports — useful
as a starting point when you configure `oauth_scopes` (see #1469):

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

This list is for local reference only — different Redmine/Planio versions
may expose different scopes.

## Use

    ./redmine-cli projects list
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
