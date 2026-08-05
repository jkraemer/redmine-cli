# Changelog

All notable changes to redmine-cli are documented in this file. The format
is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
the project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

Initial feature set, pending the first tagged release.

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
