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

    redmine-cli auth status   # check current auth method and expiry
    redmine-cli auth logout   # revoke and remove stored token

> **Note:** OAuth support is tracked in issue #1461 and is not yet
> implemented. The `oauth_client_id` config key is accepted by the parser
> today so you can pre-populate your config file.

## Use

    ./redmine-cli projects list
    ./redmine-cli issues list --project myproj --status open --limit 10
    ./redmine-cli issues get 1459
    ./redmine-cli attachments download 42
    ./redmine-cli wiki list --project myproj
    ./redmine-cli wiki get Architecture --project myproj
    ./redmine-cli wiki put MyPage --project myproj --text-file draft.textile --confirm

See `./redmine-cli --agent --help` for machine-readable help.
