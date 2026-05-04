# redmine-cli Phase 2+ — QoL gaps and search

> **For Claude:** Use superpowers:executing-plans / subagent-driven-development to implement.

**Goal:** Plug agent-friendliness gaps in the existing CLI and add the `search` command. Stay on master, ship small, mocked-tested commits.

**Parent ticket:** #1459 (umbrella)
**Out of scope (separate tickets):** #1461 (OAuth), #1462 (Planio extras + packaging)

---

## Architecture notes

- Same patterns as Phase 1+2: `internal/api/` typed wrapper, `internal/commands/` cobra trees, JSON/markdown output via `internal/output/`, mocked tests via `httptest`.
- New endpoints used:
  - `GET /users/current.json` (users me)
  - `GET /users.json` (users list — admin only on most installs; degrade to 403 → exit 4)
  - `GET /trackers.json`
  - `GET /issue_statuses.json`
  - `GET /enumerations/issue_priorities.json`
  - `GET /enumerations/time_entry_activities.json`
  - `GET /search.json?q=...`
- Custom fields use Redmine's `custom_fields: [{id, value}]` array; we pass them through CLI as `--cf <id>=<value>` (repeatable).
- Pagination: every list endpoint already returns `total_count`, `offset`, `limit`. `--all` = loop with offset += limit until offset+limit >= total_count, capped at e.g. 1000 results to prevent runaway.

---

## Task 1: Lookup commands (users, trackers, statuses, priorities, time-activities)

**Files:**
- Modify: `internal/api/types.go` — add `User`, `Tracker`, `Status`, `Priority`, `Activity` types and list-result wrappers
- Modify: `internal/api/client.go` — add `GetCurrentUser`, `ListUsers`, `ListTrackers`, `ListStatuses`, `ListPriorities`, `ListActivities`
- Create: `internal/commands/lookups.go` — cobra commands: `users me`, `users list`, `trackers list`, `statuses list`, `priorities list`, `time-activities list`
- Create: `internal/commands/lookups_test.go` — mocked tests for each
- Modify: `internal/commands/root.go` — register `users`, `trackers`, `statuses`, `priorities`, `time-activities`

**Notes:**
- Output: same JSON / markdown table treatment as projects.
- For markdown, just print id+name (+ identifier where applicable, + is_default for priorities/activities).
- `users me` returns a single user; render as JSON or as a small markdown block (id, login, name, mail).
- `users list` may 403; let the standard error handler emit exit 4.

**Steps (TDD):**
1. Write types in `types.go`.
2. Write a failing test for one lookup endpoint (e.g. `TestListTrackers_OK`); implement; pass.
3. Repeat for each lookup.
4. Write a failing cobra test (e.g. `TestTrackersList_JSON`); implement command; pass.
5. Repeat for each cobra command.
6. Register in `root.go`.
7. `go test ./... && go vet ./... && gofmt -l .`
8. Commit per command: `feat: add <thing> lookup command`.

---

## Task 2: --all auto-pagination

**Files:**
- Modify: `internal/api/client.go` — generalise `ListIssues` and `ListProjects` to support paginate-all, OR add helper `paginateAll(ctx, fetchPage)` that callers loop with.
- Modify: `internal/commands/issues.go`, `projects.go`, plus the new `lookups.go` (where the underlying endpoint paginates — users does, the enumeration ones don't).
- Modify: `internal/commands/issues_test.go`, `projects_test.go` — add multi-page tests.

**Notes:**
- Add `--all` flag (bool). When true, ignore `--limit`/`--offset` and fetch every page. Cap at 1000 results; if exceeded, error out with a clear message ("more than 1000 results; narrow your filters or use --limit").
- Implementation: simplest is per-command loop using existing list params. Pass fixed page size (100) internally.

**Steps (TDD):**
1. Failing test: `TestIssuesList_All_PaginatesAcrossPages` — mock server returns 3 pages.
2. Implement `--all` for issues.
3. Repeat for projects, users.
4. Add cap test: `TestIssuesList_All_RespectsCap`.
5. `go test ./... && go vet ./... && gofmt -l .`
6. Commit: `feat: add --all auto-pagination to list commands`.

---

## Task 3: --notes-file and custom fields

**Files:**
- Modify: `internal/commands/issues.go` (update + create)
- Modify: `internal/commands/issues_test.go`
- Modify: `internal/api/types.go` — add `CustomFieldValue {ID int; Value any}` and slot into `IssueCreate` and `IssueUpdate`

**Notes:**
- `--notes-file <path>` on `issues update`: mutually exclusive with `--notes`. Read entire file, treat trailing whitespace as significant (don't trim). Use `os.ReadFile`.
- `--cf <id>=<value>` repeatable, on both `issues create` and `issues update`. Parse as `strings.SplitN(s, "=", 2)`. Numeric ID, value is opaque string. Multiple values for the same field via comma-separated string is a follow-up; for now one value per occurrence.
- JSON wrapping: Redmine expects `{"issue":{..., "custom_fields":[{"id":1,"value":"x"},{"id":2,"value":"y"}]}}`.

**Steps (TDD):**
1. Failing test: `TestIssuesUpdate_NotesFile`.
2. Implement.
3. Failing test: `TestIssuesUpdate_NotesFile_ConflictsWithNotes`.
4. Implement validation.
5. Failing test: `TestIssuesCreate_CustomFields_InBody` and `TestIssuesUpdate_CustomFields_InBody`.
6. Implement.
7. `go test ./... && go vet ./... && gofmt -l .`
8. Commit: `feat: add --notes-file and --cf custom fields`.

---

## Task 4: Search command

**Files:**
- Modify: `internal/api/types.go` — add `SearchResult` and `SearchResultsParams`
- Modify: `internal/api/client.go` — `Search(ctx, params) (*SearchResults, error)`
- Create: `internal/commands/search.go` — `search <query...>` command
- Create: `internal/commands/search_test.go`
- Modify: `internal/commands/root.go` — register `search`

**Notes:**
- Endpoint: `GET /search.json?q=<query>&offset=<n>&limit=<n>&scope=<>`
- Useful filters per Redmine docs: `issues=1`, `news=1`, `documents=1`, `changesets=1`, `wiki_pages=1`, `messages=1`, `projects=1`, `attachments=1` (subset based on what's enabled). For our CLI start simple: `--issues`, `--wiki`, `--projects`, `--all-types` (default = whatever Redmine returns by default = all enabled).
- Response shape: `{"results":[{"id":..,"title":..,"type":..,"url":..,"description":..,"datetime":..}], "total_count":.., "offset":.., "limit":..}`
- Markdown output: short table of `type | id | title | datetime`. JSON output: pass through.
- Support `--all` and `--limit`/`--offset` (consistent with other lists).

**Steps (TDD):**
1. Failing test: `TestSearch_QueryAndFilters`.
2. Implement client method.
3. Failing test: `TestSearch_JSON_Output`.
4. Implement command.
5. Failing test: `TestSearch_Markdown_Output`.
6. Implement.
7. `go test ./... && go vet ./... && gofmt -l .`
8. Commit: `feat: add search command`.

---

## Final task: smoke test + docs

1. Build, run live against rm.jkraemer.net:
   - `redmine-cli users me` (must succeed)
   - `redmine-cli trackers list`
   - `redmine-cli statuses list`
   - `redmine-cli priorities list`
   - `redmine-cli time-activities list`
   - `redmine-cli issues list --project fera --all` (verify pagination)
   - `redmine-cli search "phase 2"` (verify hits)
   - Spot-check `--cf` on a project that has a custom field, otherwise just verify dry-run body shape
2. Update `skills/redmine-cli/SKILL.md`: add the new commands to Quick Reference, mention `--all`, `--notes-file`, `--cf`, the new search command. Remove any "out of scope" lines that no longer apply.
3. Comment on #1459 with the delta.

---

## Success criteria

- All four task groups implemented and committed.
- `go test ./...` passes (target: ~45 tests total).
- `go vet ./...` clean, `gofmt -l .` clean.
- Live smoke test shows new commands working against rm.jkraemer.net.
- SKILL.md and #1459 reflect current state.
