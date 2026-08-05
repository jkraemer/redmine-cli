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
user whose key is used, and there is no way to limit them server-side. If you
want to have more control over the permissions granted to the CLI, read on and
use OAuth 2.0 instead, or create a dedicated "bot" user account in Redmine and
use its API key. To restrict the CLI to reads regardless of auth method, see
[Read-only mode](#read-only-mode) below.

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

### Read-only mode

To restrict the CLI to read-only actions, set `read_only` in the config file:

    read_only = true

or via the environment (which takes precedence, so `REDMINE_READ_ONLY=false`
overrides a config file that enables it):

    export REDMINE_READ_ONLY=true

In read-only mode, any write — `issues create`, `issues update`, `wiki put`, or
`time log` run with `--confirm` — is refused before any request is sent, and the
command exits with code **8**. Dry-run previews (the same commands *without*
`--confirm`) and all read commands continue to work. `--agent --help` reports
`read_only: true` and marks write commands as `blocked`.

This is the only way to constrain an API-key client, which otherwise inherits
the full permissions of its user (see above). With OAuth you can additionally
limit the granted scopes.

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
    ./redmine-cli issues watchers add 1459 7 --confirm
    ./redmine-cli attachments download 42
    ./redmine-cli wiki list --project myproj
    ./redmine-cli wiki get Architecture --project myproj
    ./redmine-cli wiki put MyPage --project myproj --text-file draft.textile --confirm
    ./redmine-cli wiki put MyPage --project myproj --text-file draft.textile --attach diagram.png --confirm
    ./redmine-cli categories list --project myproj
    ./redmine-cli custom-fields list
    ./redmine-cli version

See `./redmine-cli --agent --help` for machine-readable help.

## Testing

    make test          # hermetic unit/command tests, no network

`make test` is what runs on every push (see below) and needs nothing but Go.

### Live end-to-end suite

    make live-test REDMINE_VERSION=6.1

This boots a real `redmine:<version>` container (via `docker` if installed,
`podman` otherwise — override with `RUNTIME=podman`), waits for it to come up
(first boot runs DB migrations, which can take a couple of minutes), bootstraps
an admin API key and an `e2e` project inside it, then runs the tests in `e2e/`
(build tag `live`) against the real server through the real `redmine-cli`
binary — no mocks. The container is removed on exit.

Useful overrides:

    PORT=3001 make live-test REDMINE_VERSION=6.1   # if 3000 is taken
    KEEP=1 make live-test REDMINE_VERSION=6.1      # leave the container running for debugging

With `KEEP=1`, `scripts/live/run.sh` prints the container name and port
instead of tearing it down, so you can poke at the instance or re-run
`go test -tags live ./e2e -v` by hand against it.

CI (`.github/workflows/ci.yml`) runs the hermetic suite plus the live suite
against Redmine 5.1, 6.1, and 7.0 on every push and pull request.

## Releases

Tagging and pushing to the `gh` remote cuts a release:

    git tag v0.1.0 && git push gh v0.1.0

A GitHub Actions workflow (`.github/workflows/release.yml`) then runs
goreleaser, which builds binaries for 5 platforms (linux/amd64,
linux/arm64, darwin/amd64, darwin/arm64, windows/amd64) and publishes
them to GitHub Releases.

## License

Copyright (C) 2026 Jens Kraemer

This program is free software: you can redistribute it and/or modify it
under the terms of the GNU General Public License as published by the
Free Software Foundation, either version 3 of the License, or (at your
option) any later version. See [LICENSE](LICENSE) for the full text.
