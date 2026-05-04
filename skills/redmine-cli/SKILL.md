---
name: redmine-cli
description: |
  Interact with Redmine/Planio via the redmine-cli binary. Phase 1 covers
  read operations: list/get issues, list projects, download attachments.
  Use for ANY Redmine question or read action.
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
  - rm.jkraemer.net
invocable: true
argument-hint: "[action] [args...]"
---

# /redmine - Redmine CLI Workflow

Phase 1 (read-only) coverage: issues, projects, attachments.

## Agent Invariants

1. **Choose the right output mode:** `--format json` (default) for parsing,
   `--format markdown` (`-m`) for human-readable output.
2. **Use `--agent --help` to discover commands** — works at every level and
   returns structured JSON.
3. **For attachments, follow the path** — `attachments download` writes the
   file to disk and prints the absolute path; read it from there with your
   file tool.
4. **Default page size is 25, max 100.** Use `--all` to auto-paginate.

## CLI Introspection

```bash
redmine-cli --agent --help                     # top-level
redmine-cli issues --agent --help              # issue subcommand tree
redmine-cli issues list --agent --help         # full flag list
```

Returns JSON `{command, path, short, long, usage, flags[], inherited_flags[], subcommands[], notes[]}`.

## Quick Reference

| Task | Command |
|------|---------|
| List projects | `redmine-cli projects list` |
| List my open issues | `redmine-cli issues list --assignee me --status open` |
| List recent issues in a project | `redmine-cli issues list --project myproj --updated-since 2026-04-01` |
| Get full issue with journals | `redmine-cli issues get 1459 --include journals,attachments` |
| Download attachment | `redmine-cli attachments download 42` |
| Save attachment to a path | `redmine-cli attachments download 42 -o /tmp/foo.png` |

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
| 1 | Usage error |
| 2 | Not found (404) |
| 3 | Auth error / missing config |
| 4 | Forbidden (403) |
| 5 | Rate limited (429) |
| 6 | Network error |
| 7 | API error (5xx) |

## Out of Scope (Phase 1)

Write operations (`create`/`update`), time logging, OAuth, Planio Help Desk,
search. Use the existing Redmine MCP tools for those until Phase 2 ships.
