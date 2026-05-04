---
name: redmine-cli
description: |
  Interact with Redmine/Planio via the redmine-cli binary. Covers read,
  write, lookups, search, and time logging. Use for ANY Redmine question
  or action.
triggers:
  - redmine
  - /redmine
  - planio
  - redmine issue
  - redmine project
  - redmine attachment
  - redmine search
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

Read, write, search, lookups, time tracking. Phase 3 (OAuth + Planio extras
+ packaging) is tracked in #1461 and #1462.

## Agent Invariants

1. **Choose the right output mode:** `--format json` (default) for parsing,
   `--format markdown` (`-m`) for human-readable output.
2. **Use `--agent --help` to discover commands** — works at every level and
   returns structured JSON.
3. **Write ops are dry-run by default.** `issues create`, `issues update`,
   and `time log` print the request payload and exit 0 unless `--confirm`
   is passed. Always run dry-run first to inspect the body, then re-run
   with `--confirm` to actually send.
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

Returns JSON `{command, path, short, long, usage, flags[], inherited_flags[], subcommands[], notes[]}`.

## Quick Reference — Read

| Task | Command |
|------|---------|
| List projects | `redmine-cli projects list` |
| List all projects (paginated) | `redmine-cli projects list --all` |
| List my open issues | `redmine-cli issues list --assignee me --status open` |
| List recent issues in a project | `redmine-cli issues list --project myproj --updated-since 2026-04-01` |
| List ALL issues in a project | `redmine-cli issues list --project myproj --status '*' --all` |
| List with extras | `redmine-cli issues list --project myproj --include attachments,relations` |
| Get full issue with journals | `redmine-cli issues get 1459 --include journals,attachments` |
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

### Update semantics

`issues update` uses pointer semantics: only flags you pass are sent. Pass
`--description ""` explicitly to clear the description; omit it to leave
it unchanged.

### Notes from a file

`--notes-file <path>` reads the entire file (whitespace preserved) and
sends it as the `notes` field. Mutually exclusive with `--notes`.

### Custom fields

`--cf <id>=<value>` is repeatable on both `issues create` and
`issues update`. Look up custom-field IDs from the project settings
in the web UI; the CLI does not yet have a `custom-fields list`
command.

### Time log scope

Pass exactly one of `--issue <id>` or `--project <id-or-identifier>`,
never both. Server-side validation (422) will reject invalid
combinations or disabled activities — the CLI surfaces these with
exit code 1 and the raw error message.

## --all auto-pagination

Available on `issues list`, `projects list`, `users list`, and `search`.
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

## Out of Scope (in flight)

- OAuth 2.0 auth flow — #1461
- Planio Help Desk endpoints, packaging, distribution — #1462

Watchers and file uploads on create/update are not yet implemented.
