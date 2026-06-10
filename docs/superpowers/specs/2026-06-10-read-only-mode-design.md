# Read-only mode — design

## Goal

Let an operator restrict `redmine-cli` to read-only actions so it can never
mutate the Redmine server, configured two ways:

- Environment variable `REDMINE_READ_ONLY`
- Config file key `read_only` (TOML)

The CLI is agent-driven, so read-only state must be **discoverable** on the
agent-facing surfaces, not just enforced at runtime.

## Scope of "write"

Server mutations come from exactly four commands, all already dry-run-by-default
and gated by `--confirm`:

- `issues create`, `issues update`
- `wiki put`
- `time log`

These call the five mutating API methods (`CreateIssue`, `UpdateIssue`,
`LogTime`, `PutWikiPage`, and `UploadFile` via `--attach`). There is no
standalone delete command. `auth login` / token operations write only the local
config file and already skip `PersistentPreRunE`; they are unaffected.

## Setting resolution (`internal/config`)

- Add `ReadOnly bool` to `Config` and `read_only bool` (`toml:"read_only,omitempty"`)
  to `fileConfig`.
- Add `resolveReadOnly(env string, fileVal bool) (bool, error)`:
  env-over-file precedence, matching every other setting. A non-empty env value
  must parse as a bool (`strconv.ParseBool`); otherwise it returns an error
  (`invalid REDMINE_READ_ONLY value %q: must be true or false`). Empty env →
  file value. So `REDMINE_READ_ONLY=false` explicitly overrides a file
  `read_only = true`.
- `Load` calls `resolveReadOnly` strictly (an invalid env value fails `Load`).
- Add a best-effort `func ReadOnly(path string) bool` for the help surface: it
  reads `REDMINE_READ_ONLY` + the file's `read_only` **without** requiring URL or
  API key, swallowing any error (returns `false`). This keeps
  `--agent --help` working uncredentialed, as it does today.
- `config` only *resolves* the setting. It does not enforce policy.
- `read_only` survives the `SaveToken`/`DeleteToken` round-trip because they
  re-encode the whole `fileConfig`.

## Enforcement (`internal/commands/root.go`)

One gate, in the existing single seam `PersistentPreRunE`. After `config.Load`,
set `rc.readOnly = cfg.ReadOnly` (unconditionally, so the preview path sees it
too). Then, before the API client is constructed:

```go
if rc.readOnly {
    if confirm, _ := cmd.Flags().GetBool("confirm"); confirm {
        return fmt.Errorf("%w: %s", ErrReadOnly, cmd.CommandPath())
    }
}
```

- Keyed on the effective `--confirm` *value*, so `--confirm=false` is correctly
  a preview, and a read command (no `confirm` flag → `GetBool` zero value) is
  never affected.
- Placed before client construction: a blocked write fails fast, never reaching
  token refresh, attachment upload, or the API call. Because it short-circuits
  the whole `RunE`, the pre-send `applyAttachments` upload in `issues
  create`/`update` cannot fire either.
- Centralized on purpose: any future `--confirm` write command is covered with
  nothing to remember.

### Error and exit code

- New sentinel `ErrReadOnly` in `root.go`:
  `read-only mode is enabled; refusing to send a write`.
- The returned error wraps it with the command path
  (`fmt.Errorf("%w: %s", ErrReadOnly, cmd.CommandPath())`) so `errors.Is` still
  matches.
- `exitCodeFor` maps `ErrReadOnly` to **8**, a new dedicated code so an agent
  distinguishes a local read-only refusal from a server `403` (exit 4).

## Discoverability

### Mark write commands (one source of truth)

Add `addConfirmFlag(cmd *cobra.Command, confirm *bool)` to the commands package.
It adds the (currently duplicated, byte-identical) `--confirm` flag *and* sets
`cmd.Annotations["write"] = "true"`. The four write commands use it. "This is a
write" now lives in one place, derived structurally.

### `--agent --help` JSON (`internal/agenthelp`)

`Render` gains a `readOnly bool` parameter. New JSON fields:

- `Help.ReadOnly bool` (`json:"read_only,omitempty"`) — set on every command when
  read-only is active; omitted otherwise (only surfaced when it matters).
- `Help.Blocked bool` (`json:"blocked,omitempty"`) — set when the command itself
  is a write (annotation `write=true`) and read-only is active.
- `Subcommand.Blocked bool` (`json:"blocked,omitempty"`) — set for write leaves
  in a group listing (e.g. `issues --agent --help` tags `create`/`update`).
- When read-only and the command is a write, append a `note`:
  `read-only mode is active: this write command is blocked; running it with --confirm exits 8`.

`agenthelp` stays generic: it reads annotations + the bool. It does **not** learn
the `--confirm` convention. The help func resolves the bool via
`config.ReadOnly(rc.configPath)` (best-effort; `rc.configPath` is already parsed
by the time the help func runs, same as `rc.agentHelp`).

### Dry-run preview (`internal/commands/dryrun.go`)

`renderDryRun` reads `rc.readOnly`:

- Markdown footer, when read-only, replaces `(re-run with --confirm to send)`
  with `(read-only mode is active — --confirm is disabled; unset REDMINE_READ_ONLY / read_only to enable writes)`.
- JSON payload gains `"read_only": true` when active.

This removes the otherwise-misleading "re-run with --confirm" guidance.

## Documentation

- Project `CLAUDE.md`: cross-cutting conventions (read-only mode) + exit-code list
  (add 8).
- README config table: `REDMINE_READ_ONLY` / `read_only`.
- `skills/redmine-cli/SKILL.md`: exit code 8, read-only behavior, and the
  `read_only` / `blocked` `--agent --help` fields.

## Out of scope (YAGNI)

- No per-command opt-out, no command-specific read-only.
- No hiding of write commands from the tree (they stay visible, marked + refused).
- Human (`--help`) output is unchanged; the focus is the agent surface.

## Tests (TDD, via the `Build` seam + httptest)

- **config**: env true / false, file true, env-false-overrides-file-true,
  unset→false, invalid env → error; `config.ReadOnly` best-effort (env-only,
  file-only, broken file → false).
- **enforcement**: read-only + `--confirm` → `ErrReadOnly`, exit 8, and **zero
  HTTP requests** reach the test server; read-only without `--confirm` → preview
  prints (exit 0) with the adjusted footer / `read_only` JSON; not-read-only +
  `--confirm` → unchanged behavior.
- **agenthelp**: read-only marks `read_only` on a command, `blocked` + note on a
  write command, `blocked` on write subcommands in a group listing; not-read-only
  omits all three.
