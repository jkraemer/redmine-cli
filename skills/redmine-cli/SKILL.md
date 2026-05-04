---
name: redmine-cli
description: |
  Interact with Redmine/Planio via the redmine-cli binary. Phases 1+2 cover
  read and write operations: list/get/create/update issues, list projects,
  download attachments, log time. Use for ANY Redmine question or action.
triggers:
  - redmine
  - /redmine
  - planio
  - redmine issue
  - redmine project
  - redmine attachment
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

Phase 1 (read) and Phase 2 (write) coverage: issues, projects, attachments, time.

## Agent Invariants

1. **Choose the right output mode:** `--format json` (default) for parsing,
   `--format markdown` (`-m`) for human-readable output.
2. **Use `--agent --help` to discover commands** — works at every level and
   returns structured JSON.
3. **Write ops are dry-run by default.** `issues create`, `issues update`, and
   `time log` print the request payload and exit 0 unless `--confirm` is
   passed. Always run dry-run first to inspect the body, then re-run with
   `--confirm` to actually send.
4. **For attachments, follow the path** — `attachments download` writes the
   file to disk and prints the absolute path; read it from there with your
   file tool.
5. **Default page size is 25, max 100.** Use `--all` to auto-paginate.

## CLI Introspection

```bash
redmine-cli --agent --help                     # top-level
redmine-cli issues --agent --help              # issue subcommand tree
redmine-cli issues list --agent --help         # full flag list
redmine-cli issues create --agent --help       # write-op flags
redmine-cli time log --agent --help            # time logging flags
```

Returns JSON `{command, path, short, long, usage, flags[], inherited_flags[], subcommands[], notes[]}`.

## Quick Reference — Read

| Task | Command |
|------|---------|
| List projects | `redmine-cli projects list` |
| List my open issues | `redmine-cli issues list --assignee me --status open` |
| List recent issues in a project | `redmine-cli issues list --project myproj --updated-since 2026-04-01` |
| List with extras | `redmine-cli issues list --project myproj --include attachments,relations` |
| Get full issue with journals | `redmine-cli issues get 1459 --include journals,attachments` |
| Download attachment | `redmine-cli attachments download 42` |
| Save attachment to a path | `redmine-cli attachments download 42 -o /tmp/foo.png` |

## Quick Reference — Write (dry-run unless `--confirm`)

| Task | Command |
|------|---------|
| Preview new issue | `redmine-cli issues create --project myproj --tracker 1 --subject "Title" --description "..."` |
| Create issue | append `--confirm` to the above |
| Preview update | `redmine-cli issues update 42 --status 5 --notes "Closing"` |
| Update issue | append `--confirm` |
| Add comment only | `redmine-cli issues update 42 --notes "FYI" --confirm` |
| Reassign | `redmine-cli issues update 42 --assignee me --confirm` |
| Preview time log | `redmine-cli time log --hours 1.5 --activity 12 --issue 42 --comments "Investigated"` |
| Log time | append `--confirm` |

### Update semantics

`issues update` uses pointer semantics: only flags you pass are sent. Pass
`--description ""` explicitly to clear the description; omit it to leave it
unchanged.

### Time log scope

Pass exactly one of `--issue <id>` or `--project <id-or-identifier>`, never
both. Server-side validation (422) will reject invalid combinations or
disabled activities — the CLI surfaces these with exit code 1 and the raw
error message.

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

## Out of Scope (Phase 3+)

OAuth, Planio Help Desk, search, full pagination across all list commands,
custom fields, watchers, file uploads. Use the existing Redmine MCP tools
for those until Phase 3 ships.
