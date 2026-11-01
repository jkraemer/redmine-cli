# Changelog

All notable changes to redmine-cli are documented in this file. The format
is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
the project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `issues get --include allowed_statuses` is documented and rendered: the
  status transitions the current user's role may make on the issue
- `issues update --confirm` prints the resulting values of the fields it
  sent, e.g. `Updated issue #42 (status: Resolved (3), done: 100%).`; the
  JSON output carries the re-read `issue`

### Fixed

- `issues update --confirm` no longer reports success for fields Redmine
  silently dropped (a status transition the role may not make, a user who
  is not assignable, ...). With `--status` the issue is preflighted via
  `include=allowed_statuses` and a disallowed transition is refused with
  exit 1 before anything is sent; after the update the issue is re-read and
  every sent field is verified, with a stderr warning per dropped field,
  `not_applied` in the JSON output, and exit 1 (notes and the other fields
  are still saved)

## [0.3.0] - 2026-10-22

### Added

- Locked mode (`REDMINE_LOCKED`): reads only, config from the default path
  only, API-key auth only; writes, `auth`, and `--config` are refused with
  exit 8
- `attachments download -o -` streams the raw file to stdout
- `contrib/redmine-safe`: example sudo wrapper and agent skill that keep
  the API key out of the agent's reach

## [0.2.0] - 2026-10-22

### Added

- `issues get --include checklists` returns checklist items with their
  done state (requires the redmine_checklists plugin)

## [0.1.0] - 2026-08-05

Initial release.

### Added

- Issues: list/get/create/update with dry-run-by-default writes
  (`--confirm` to send), pointer-semantics updates (pass `""` to clear a
  field), category assignment (`--category`), watchers (`--watcher` on
  create, `issues watchers add/remove`, `--include watchers`), custom
  fields (`--cf`), attachments (`--attach`), and journal notes from a
  file (`--notes-file`).
- Lookups: projects, trackers, statuses, priorities, time-entry
  activities, users, issue categories, custom field definitions, and
  saved queries.
- Wiki: list/get/put with attachment support.
- Time logging against issues or projects.
- Full-text search; `--all` auto-pagination on list commands and search
  (capped at 1000 results).
- Output formats: JSON (agent-first, default) and markdown; structured
  `--agent --help` on every command; documented exit-code contract.
- Auth: API key or OAuth 2.0 (authorization code + PKCE, confidential
  clients supported) with automatic token refresh and
  `auth login/logout/status`.
- Read-only mode (`read_only` config / `REDMINE_READ_ONLY`) refusing
  confirmed writes with exit code 8.
- `version` subcommand and `--version`.
- goreleaser release pipeline (5 platforms) and CI running a hermetic
  suite plus live end-to-end tests against Redmine 5.1, 6.1, and 7.0.

### Fixed

- `issues get` now returns `category` and `custom_fields`; both were
  silently dropped from every issue read before.
- Empty list results render as JSON `[]` instead of `null`.
- Terminal escape sequences in server-controlled strings are stripped
  from all markdown output and error messages.
