---
name: redmine-cli
description: |
  Interact with Redmine via the redmine-cli binary. Covers read,
  write, lookups, search, and time logging. Use for ANY Redmine question
  or action.
triggers:
  - redmine
  - /redmine
  - redmine issue
  - redmine project
  - redmine attachment
  - redmine search
  - redmine wiki
  - wiki page
  - my redmine issues
  - assigned to me in redmine
  - download redmine attachment
  - create redmine issue
  - update redmine issue
  - log time
  - rm.jkraemer.net
invocable: true
argument-hint: "[action] [args...]"
---

# /redmine - Redmine CLI Workflow

Read, write, search, lookups, time tracking, OAuth. Packaging and
distribution tracked in #1462.

## Binary location

The `redmine-cli` binary lives alongside this SKILL.md. Invoke it via
its full path — it is **not** assumed to be on `$PATH`. All example
commands below say `redmine-cli` for brevity — substitute the full
binary path when actually running them.

**If the binary is missing** (skill installed from git or a skills
manager, which ship no binaries): run `bash <skill-dir>/bootstrap.sh`
once. It downloads the latest release binary for this platform from
GitHub Releases, verifies it against the release checksums, and places
it next to this file. Pass a version argument (e.g. `0.1.0`) to pin.

## Agent Invariants

1. **Choose the right output mode:** `--format json` (`-j`, default) for
   parsing, `--format markdown` (`-m`) for human-readable output. Pass at most
   one of `--format`/`-m`/`-j`.
2. **Use `--agent --help` to discover commands** — works at every level and
   returns structured JSON.
3. **Write ops are dry-run by default.** `issues create`, `issues update`,
   `wiki put`, and `time log` print the request payload and exit 0 unless
   `--confirm` is passed. Always run dry-run first to inspect the body, then
   re-run with `--confirm` to actually send. **If read-only mode is active**,
   a `--confirm` write is refused with exit 8 (the dry-run preview still
   works); `--agent --help` then reports `read_only: true` and marks write
   commands `blocked`. Don't retry an exit-8 write.
4. **For attachments, follow the path** — `attachments download` writes
   the file to disk and prints the absolute path; read it from there with
   your file tool.
5. **Default page size is 25, max 100.** Use `--all` on list commands or
   search to auto-paginate (capped at 1000 results).
6. **Look up IDs first.** Use `trackers list`, `statuses list`,
   `priorities list`, `time-activities list`, `users me`/`users list`
   instead of guessing numeric IDs.

## CLI Introspection

```bash
redmine-cli --agent --help                     # top-level
redmine-cli issues --agent --help              # issue subcommand tree
redmine-cli issues list --agent --help         # full flag list
redmine-cli issues create --agent --help       # write-op flags
redmine-cli time log --agent --help            # time logging flags
redmine-cli search --agent --help              # search filters
```

Also add to the CLI introspection examples:
```bash
redmine-cli wiki --agent --help              # wiki subcommand tree
redmine-cli wiki list --agent --help         # list flags
redmine-cli wiki get --agent --help          # get flags
```

## Quick Reference — Wiki

Wiki pages are always scoped to a project (`--project` is required).

| Task | Command |
|------|---------|
| List wiki pages in a project | `redmine-cli wiki list --project myproj` |
| Get a page by title | `redmine-cli wiki get Architecture --project myproj` |
| Get a page as JSON | `redmine-cli wiki get Architecture --project myproj --format json` |

| Create or update a page | `redmine-cli wiki put MyPage --project myproj --text "h1. Hello"` |
| Create from file | `redmine-cli wiki put MyPage --project myproj --text-file page.textile --comments "Initial draft"` |
| Actually send (not dry-run) | append `--confirm` |

Notes:
- Page titles are case-sensitive and match the Redmine URL slug (e.g. `Database-Model`).
- `wiki get` includes attachments in the response automatically.
- A 404 means either the page doesn't exist or the Wiki module is not enabled for that project.
- `wiki put` uses idempotent PUT — it **creates** the page if it doesn't exist, **updates** it if it does.
- **Write ops are dry-run by default.** Always inspect the preview before adding `--confirm`.
- `--text` and `--text-file` are mutually exclusive; exactly one is required.
- Page content uses Redmine's configured markup format - by default this is **CommonMark** (GitHub-flavored Markdown).
- **Redmine wiki internal links** use the `[[Page Title]]` syntax:
  - `[[Gitolite-Integration]]` — linked page title shown as display text
  - `[[Gitolite-Integration|Integration Docs]]` — custom display text
  - NOT standard markdown `[text](url)` or Textile links — Redmine's renderers handle `[[ ]]` natively
- Wiki content is sent as raw text via `--text` — Redmine parses it according to the project's markup setting. No need to pre-render or wrap in HTML.

## Quick Reference — Read

| Task | Command |
|------|---------|
| List projects | `redmine-cli projects list` |
| List all projects (paginated) | `redmine-cli projects list --all` |
| List my open issues | `redmine-cli issues list --assignee me --status open` |
| List recent issues in a project | `redmine-cli issues list --project myproj --updated-since 2026-04-01` |
| List ALL issues in a project | `redmine-cli issues list --project myproj --status '*' --all` |
| List with extras | `redmine-cli issues list --project myproj --include attachments,relations` |
| List saved queries | `redmine-cli queries list` |
| Run a saved query | `redmine-cli issues list --query-id 42 --all` |
| Get full issue with journals | `redmine-cli issues get 1459 --include journals,attachments` |
| Get issue with watchers | `redmine-cli issues get 1459 --include watchers` |
| Download attachment | `redmine-cli attachments download 42` |
| Save attachment to a path | `redmine-cli attachments download 42 -o /tmp/foo.png` |

## Quick Reference — Lookups

| Task | Command |
|------|---------|
| Who am I | `redmine-cli users me` |
| List users (admin only) | `redmine-cli users list` |
| Trackers (Bug, Feature, ...) | `redmine-cli trackers list` |
| Issue statuses | `redmine-cli statuses list` |
| Priorities | `redmine-cli priorities list` |
| Time-entry activities | `redmine-cli time-activities list` |
| Issue categories (per project) | `redmine-cli categories list --project myproj` |
| Custom fields (admin only) | `redmine-cli custom-fields list` |

## Quick Reference — Search

| Task | Command |
|------|---------|
| Free-text search | `redmine-cli search "phase 2 plan"` |
| Issues only | `redmine-cli search "deploy" --issues` |
| Wiki only | `redmine-cli search "architecture" --wiki` |
| Within a project | `redmine-cli search "bug" --project myproj` |
| Titles only | `redmine-cli search "release" --titles-only` |
| Paginate all | `redmine-cli search "redmine" --all` |

Scopes: `--scope all|my_projects|subprojects` (default = `all`).

## Quick Reference — Write (dry-run unless `--confirm`)

| Task | Command |
|------|---------|
| Preview new issue | `redmine-cli issues create --project myproj --tracker 1 --subject "Title" --description "..."` |
| Create issue | append `--confirm` to the above |
| Issue with custom fields | append `--cf 5=hello --cf 7=world` |
| Preview update | `redmine-cli issues update 42 --status 5 --notes "Closing"` |
| Update issue | append `--confirm` |
| Add comment only | `redmine-cli issues update 42 --notes "FYI" --confirm` |
| Long comment from file | `redmine-cli issues update 42 --notes-file /tmp/comment.md --confirm` |
| Reassign | `redmine-cli issues update 42 --assignee me --confirm` |
| Set custom field on update | `redmine-cli issues update 42 --cf 7=value --confirm` |
| Preview time log | `redmine-cli time log --hours 1.5 --activity 12 --issue 42 --comments "Investigated"` |
| Log time | append `--confirm` |
| Issue with category | append `--category 7` |
| Issue with watchers | append `--watcher 3 --watcher 8` |
| Set category on update | `redmine-cli issues update 42 --category 7 --confirm` |
| Clear category | `redmine-cli issues update 42 --category "" --confirm` |
| Preview add watcher | `redmine-cli issues watchers add 42 7` |
| Add watcher | append `--confirm` |
| Preview remove watcher | `redmine-cli issues watchers remove 42 7` |
| Remove watcher | append `--confirm` |

### Update semantics

`issues update` uses pointer semantics: only flags you pass are sent. Pass
`--description ""` explicitly to clear the description; omit it to leave
it unchanged.

### Notes from a file

`--notes-file <path>` reads the entire file (whitespace preserved) and
sends it as the `notes` field. Mutually exclusive with `--notes`.

### Custom fields

`--cf <id>=<value>` is repeatable on both `issues create` and
`issues update`. Look up custom-field IDs with `redmine-cli
custom-fields list` (admin only on most Redmine installs) or from the
project settings in the web UI.

### Time log scope

Pass exactly one of `--issue <id>` or `--project <id-or-identifier>`,
never both. Server-side validation (422) will reject invalid
combinations or disabled activities — the CLI surfaces these with
exit code 1 and the raw error message.

## --all auto-pagination

Available on `issues list`, `projects list`, `users list`, `queries list`, and `search`.
When set:

- Internally fetches in pages of 100 until exhausted.
- Ignores `--limit`/`--offset`.
- Capped at 1000 results — beyond that the CLI errors with
  `more than 1000 results (N); narrow your filters or omit --all`.

## Configuration

Env (highest priority):

    export REDMINE_URL=https://your.redmine.example
    export REDMINE_API_KEY=...

Or `~/.config/redmine-cli/config.toml`:

    url = "https://your.redmine.example"
    api_key = "..."
    default_format = "markdown"

### Read-only mode

`read_only = true` in the config (or `REDMINE_READ_ONLY=true` in the env,
which takes precedence) restricts the CLI to reads: any `--confirm` write is
refused with exit 8. Previews and reads still work.

### OAuth (alternative to API key)

When using OAuth instead of an API key, set `oauth_client_id` (and
`oauth_client_secret` for confidential clients) plus the scopes the
client needs — without them Redmine grants only its minimal default set:

    oauth_client_id = "..."
    oauth_scopes = ["view_project", "view_issues", "edit_issues",
                    "view_wiki_pages", "edit_wiki_pages", "log_time"]

Or via env: `REDMINE_OAUTH_CLIENT_ID`, `REDMINE_OAUTH_CLIENT_SECRET`,
`REDMINE_OAUTH_SCOPES` (space-separated).

Then run `redmine-cli auth login`. `auth status` shows the granted scope
and is preserved across token refreshes. The OAuth token is stored in
the same TOML file under a `[token]` section. See README for a list
of common scopes and the admin workaround for older Redmines.

### Multiple instances

Pass `--config <path>` (`-c`) on any command to select a different
config file. Each file is self-contained (URL, credentials, token) so
tokens never leak between instances:

    redmine-cli --config ~/.config/redmine-cli/projA.toml issues list --project foo

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | OK |
| 1 | Usage error / API validation error (4xx other than below) |
| 2 | Not found (404) |
| 3 | Auth error / missing config |
| 4 | Forbidden (403) |
| 5 | Rate limited (429) |
| 6 | Network error |
| 7 | API error (5xx) |
| 8 | Blocked by read-only mode (a `--confirm` write was refused locally) |

Exit 8 is a local policy refusal, not a server error — do not retry; the
write will keep failing until read-only mode is turned off.

## Out of Scope (in flight)

- Packaging, distribution — #1462: Help Desk endpoints, an installer
  script, and publishing the first actual release remain open. The
  `version` subcommand and the release workflow (tag `vX.Y.Z`, push to
  the `gh` remote, goreleaser publishes binaries for 5 platforms to
  GitHub Releases) already exist.
