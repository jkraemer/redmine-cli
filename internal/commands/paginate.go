package commands

import "fmt"

// paginateAllPageSize is the internal page size used when --all is set on
// list commands. paginateAllCap is the maximum allowed total_count; if the
// server reports more than this many results, --all aborts with an error
// asking the caller to narrow their filters.
const (
	paginateAllPageSize = 100
	paginateAllCap      = 1000
)

// collectPages runs the shared list-command pagination protocol: it
// validates the --limit flag, fetches one page in normal mode, and in
// --all mode fetches every page with a fixed internal page size (ignoring
// --limit/--offset), refusing result sets larger than paginateAllCap.
// fetch returns one page of items plus the server-reported total_count.
func collectPages[T any](limit, offset int, all bool, fetch func(limit, offset int) ([]T, int, error)) ([]T, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("--limit must be between 1 and 100")
	}
	if all {
		limit = paginateAllPageSize
		offset = 0
	}
	// Non-nil so an empty result marshals as JSON [] rather than null —
	// agent consumers (e.g. jq's .issues[]) hard-error on null.
	collected := []T{}
	for {
		items, total, err := fetch(limit, offset)
		if err != nil {
			return nil, err
		}
		if all && total > paginateAllCap {
			return nil, fmt.Errorf("more than %d results (%d); narrow your filters or omit --all", paginateAllCap, total)
		}
		collected = append(collected, items...)
		if !all || len(collected) >= total || len(items) == 0 {
			break
		}
		offset += len(items)
	}
	return collected, nil
}
