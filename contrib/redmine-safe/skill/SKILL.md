---
name: redmine-safe
description: |
  Read-only access to the locked Redmine instance via the redmine-safe
  command. Use for any question about that instance's issues, projects,
  wiki, or attachments. Writes are impossible.
invocable: true
argument-hint: "[action] [args...]"
---

# redmine-safe - read-only Redmine

`redmine-safe` is a locked `redmine-cli` for one Redmine instance. It runs
as a separate system user that holds the API key; you never see the key and
cannot write to the instance.

## Invariants

1. **Writes and `auth` are unavailable.** `issues create`, `issues update`,
   `issues watchers add/remove`, `time log`, `wiki put`, and every `auth`
   subcommand exit 8, even without `--confirm`. Don't retry, and don't look
   for workarounds (other tools, direct API calls, other config files). If
   a write is needed, tell the user.
2. **`--config`/`-c` and environment overrides do not work.** The config is
   fixed; `REDMINE_URL`, `REDMINE_API_KEY` etc. are ignored.
3. **Attachments stream to stdout.** `attachments download <id>` prints the
   raw bytes and nothing else. Any `-o` other than `-` exits 8. Save the
   file with a shell redirect, then read it with your file tool:

       redmine-safe issues get 42 --include attachments    # filename, content type, attachment id
       redmine-safe attachments download 7 > /tmp/<filename>

   Take `<filename>` and the content type from the `attachments` list of
   `issues get`. Screenshots and PDFs are the intended use.
4. **Use `redmine-safe <cmd> --agent --help`** to discover commands and
   flags. The JSON reports `"locked": true` and marks unavailable commands
   `blocked`.
5. **Default page size is 25, max 100.** Use `--all` on list commands or
   search (capped at 1000 results).

## Command reference

Read commands behave exactly as in the redmine-cli skill: see its Read,
Lookups, Search, and Wiki sections (the `wiki list` and `wiki get` parts)
and substitute `redmine-safe` for `redmine-cli`. Ignore its Write,
Configuration, and OAuth sections, and `wiki put`.

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
| 8 | Blocked by policy (locked mode refused a write, `auth`, `--config`, or a non-stdout download target) |

Exit 8 is a local policy refusal, not a server error. It will never succeed
on retry.

If `redmine-safe` itself fails with a sudo message (for example "a password
is required"), the wrapper is not set up for this shell; report it to the
user. Sandboxes that set `no_new_privs` cause this.
