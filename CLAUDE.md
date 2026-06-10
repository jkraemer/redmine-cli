# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build, test, run

    make build         # builds ./redmine-cli (entry: cmd/redmine-cli)
    make test          # go test ./...
    make fmt vet
    go test ./internal/commands -run TestIssuesUpdate   # single test
    go test ./internal/commands -run TestIssuesUpdate/sub_name

`make generate` is intentionally a no-op — see `api/SOURCE.md`. The
OpenAPI spec under `api/` is vendored documentation; oapi-codegen is
disabled because upstream uses inline `oneOf` schemas that produce
broken output. The runtime client is hand-rolled in `internal/api/`.

## Architecture

Entry point `cmd/redmine-cli/main.go` calls `commands.Execute()`. Everything
else lives under `internal/`:

- `internal/commands/` — cobra command tree. `root.go::Build(ctx, out, errOut)`
  wires every subcommand and is the seam tests use. The exported
  `Execute()` adds signal handling and `os.Exit`.
  - `runCtx` (shared across subcommands) carries `out`, `errOut`, the
    initialized `*api.Client`, the resolved `format` ("json" or
    "markdown"), and the cancellable `parentCtx` propagated to every
    HTTP call.
  - `PersistentPreRunE` loads config, picks API-key vs OAuth, refreshes
    expired tokens (preserving prior scope), and constructs the client.
    Auth subcommands and bare `--help` skip this so they work without
    valid creds.
- `internal/api/` — typed HTTP client. `New()` for API-key auth,
  `NewWithToken()` for OAuth Bearer. `DefaultHTTPClient()` deliberately
  sets per-phase timeouts but **no overall body timeout** so streaming
  large attachments to disk isn't capped. Cancellation comes from the
  context.
- `internal/config/` — loads env vars (highest priority) then a TOML
  config file. The path is `--config <path>` (`-c`) if given, otherwise
  `$XDG_CONFIG_HOME/redmine-cli/config.toml`. `Load` populates
  `Config.Path` (resolved path) and `Config.Token` (OAuth token from the
  file's `[token]` section, if any). `AuthMethod()` returns `"oauth"` if
  `oauth_client_id` is set, else `"apikey"`. Loose perms on the config
  file surface as a non-fatal warning in `cfg.Warnings`. Token I/O lives
  here too: `cfg.SaveToken(*auth.Token)` rewrites the file atomically
  (mode 0600, comments not preserved) and `cfg.DeleteToken()` strips the
  `[token]` section. On the default config path only, a legacy
  `~/.config/redmine-cli/token.json` is read transparently on `Load` and
  removed on the next successful `SaveToken`.
- `internal/auth/` — OAuth 2.0 authorization-code + PKCE with the OOB
  redirect URI (`urn:ietf:wg:oauth:2.0:oob`). Confidential clients (with
  `oauth_client_secret`) skip PKCE. Refresh preserves the previously
  granted scope. The `auth.Token` struct and its `Expired()` helper live
  in `token.go`; token persistence is in `internal/config` (see above).
- `internal/output/` — JSON (2-space, no HTML escaping) and Markdown
  table rendering. Format chosen by `--format` flag, then config
  `default_format`, then `"json"`.
- `internal/agenthelp/` — emits a structured JSON description of any
  cobra command. Triggered by `--agent --help` at any nesting level via
  a `HelpFunc` installed on the root command.

### Cross-cutting conventions

- **Dry-run by default for writes.** `issues create`, `issues update`,
  `time log`, and `wiki put` print a preview (JSON body + summary of
  files that would be uploaded) and exit 0 unless `--confirm` is passed.
  The single emitter is `commands/dryrun.go::renderDryRun`; don't add
  parallel dry-run paths in new commands.
- **Read-only mode.** `read_only` config / `REDMINE_READ_ONLY` env
  (resolved in `internal/config`) refuses any `--confirm` write. A single
  gate in `root.go::PersistentPreRunE` keys on the effective `--confirm`
  value and returns `ErrReadOnly` (exit 8) before the client is built, so
  no upload/refresh/call runs — don't add per-command read-only checks.
  Write commands declare themselves via `addConfirmFlag` (sets the
  `write` annotation); `--agent --help` and the dry-run footer read that
  + the resolved flag to advertise the restriction.
- **Pointer semantics on updates.** `issues update` only sends fields
  that were explicitly set. Pass `--description ""` to clear; omit to
  leave unchanged.
- **Attachments share helpers.** `--attach` is a repeatable flag on
  `issues create`, `issues update`, and `wiki put`. All three use the
  helpers in `commands/attachments.go` (`parseAttachSpecs`,
  `preflightAttachSpecs`, `uploadAndAttach*`) — keep new attach-capable
  commands on that path so behavior stays consistent.
- **`--all` auto-pagination.** Available on `issues list`,
  `projects list`, `users list`, and `search`. Fetches in pages of 100,
  ignores `--limit`/`--offset`, caps at 1000.
- **Exit codes are part of the contract** (see `exitCodeFor` in
  `root.go`). 0 OK, 1 generic/4xx-other, 2 not-found, 3 auth/config,
  4 forbidden, 5 rate-limited, 6 network, 7 5xx, 8 blocked by read-only
  mode. Tests and the agent skill depend on these — preserve them.
- **HTML escaping is off** for JSON output. Same for markdown dry-run
  bodies — see commit 13c2b83 if you're tempted to re-enable.

## Agent-facing surface

The CLI is designed to be driven by an agent (see
`skills/redmine-cli/SKILL.md`). The two things that surface change
through is the `--agent --help` JSON and the dry-run preview. If you
add a new command or flag, run `./redmine-cli <cmd> --agent --help`
locally and make sure the JSON looks right — that's what agents will
read.

## Planning docs

`docs/plans/` is gitignored and holds design notes from prior phases.
They are useful background but not authoritative — the code is.
