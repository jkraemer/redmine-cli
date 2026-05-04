# OpenAPI Spec Source

Vendored from https://github.com/d-yoshi/redmine-openapi
Pinned commit: b38f1e1b97d231106c055f77eede85a4b636b2ad
License: MIT (per upstream)

To update:

    COMMIT=<new-sha>
    curl -fsSL "https://raw.githubusercontent.com/d-yoshi/redmine-openapi/${COMMIT}/openapi.yaml" -o api/openapi.yaml
    # see "Code generation status" below before running `make generate`
    go test ./...

## Code generation status

Code generation via `oapi-codegen` is currently **disabled**.

Reason: the upstream spec defines several inline `oneOf` schemas of primitive
types (e.g. `project_id: oneOf: [integer, string]`, `repository_id`,
`CustomFields.value`). oapi-codegen v2.4.1 emits accessor methods that
reference anonymous union member types named `N0`, `N1`, ... but does not
emit the corresponding type definitions, producing
`undefined: N0` / `undefined: N1` build errors. This was reproduced both
with the full spec and with `include-tags` narrowed to
`Issues`/`Projects`/`Attachments`.

The Phase 1 implementation does not need the generated client at runtime --
the typed wrapper in `internal/redmine/` (Task 5) hand-rolls the four
endpoints we use (issues list/get, projects list, attachments download).

`api/openapi.yaml` and `api/oapi-codegen.yaml` remain checked in so the
spec is still available as authoritative API documentation and so that
codegen can be re-enabled in the future (e.g. after upgrading
oapi-codegen, patching the spec, or pre-extracting the inline `oneOf`
schemas into named components). To re-enable, restore the
`generate:` recipe in the Makefile to:

    oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
