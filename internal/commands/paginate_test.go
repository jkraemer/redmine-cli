package commands

import (
	"errors"
	"strings"
	"testing"
)

// fakePages returns a fetch func serving items in pages, recording each
// (limit, offset) call.
func fakePages(items []int, total int, calls *[][2]int) func(limit, offset int) ([]int, int, error) {
	return func(limit, offset int) ([]int, int, error) {
		*calls = append(*calls, [2]int{limit, offset})
		if offset >= len(items) {
			return nil, total, nil
		}
		end := offset + limit
		if end > len(items) {
			end = len(items)
		}
		return items[offset:end], total, nil
	}
}

func intRange(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func TestCollectPages_SinglePageWithoutAll(t *testing.T) {
	var calls [][2]int
	got, err := collectPages(25, 5, false, fakePages(intRange(200), 200, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 25 {
		t.Errorf("got %d items, want 25", len(got))
	}
	if got[0] != 5 {
		t.Errorf("first item %d, want 5 (offset honored)", got[0])
	}
	if len(calls) != 1 {
		t.Errorf("fetch called %d times, want 1", len(calls))
	}
	if calls[0] != [2]int{25, 5} {
		t.Errorf("fetch called with %v, want limit=25 offset=5", calls[0])
	}
}

func TestCollectPages_RejectsLimitOutOfRange(t *testing.T) {
	for _, limit := range []int{0, -1, 101} {
		var calls [][2]int
		_, err := collectPages(limit, 0, false, fakePages(intRange(10), 10, &calls))
		if err == nil {
			t.Errorf("limit %d: expected error, got nil", limit)
			continue
		}
		if !strings.Contains(err.Error(), "between 1 and 100") {
			t.Errorf("limit %d: error should explain the range: %q", limit, err.Error())
		}
		if len(calls) != 0 {
			t.Errorf("limit %d: fetch must not be called on invalid input", limit)
		}
	}
}

func TestCollectPages_AllFetchesEveryPage(t *testing.T) {
	var calls [][2]int
	got, err := collectPages(25, 7, true, fakePages(intRange(250), 250, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 250 {
		t.Errorf("got %d items, want 250", len(got))
	}
	if len(calls) != 3 {
		t.Errorf("fetch called %d times, want 3 pages of %d", len(calls), paginateAllPageSize)
	}
	// --all ignores --limit/--offset and uses the internal page size.
	if calls[0] != [2]int{paginateAllPageSize, 0} {
		t.Errorf("first call %v, want limit=%d offset=0", calls[0], paginateAllPageSize)
	}
}

func TestCollectPages_AllRefusesOverCap(t *testing.T) {
	var calls [][2]int
	_, err := collectPages(25, 0, true, fakePages(intRange(100), paginateAllCap+1, &calls))
	if err == nil {
		t.Fatal("expected error when total exceeds cap, got nil")
	}
	if !strings.Contains(err.Error(), "narrow your filters") {
		t.Errorf("cap error should suggest narrowing filters: %q", err.Error())
	}
}

func TestCollectPages_AllStopsOnEmptyPage(t *testing.T) {
	// A server misreporting total_count higher than what it serves must not
	// loop forever.
	var calls [][2]int
	got, err := collectPages(25, 0, true, fakePages(intRange(50), 500, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 50 {
		t.Errorf("got %d items, want the 50 actually served", len(got))
	}
	if len(calls) != 2 {
		t.Errorf("fetch called %d times, want 2 (full page then empty page)", len(calls))
	}
}

func TestCollectPages_PropagatesFetchError(t *testing.T) {
	boom := errors.New("boom")
	_, err := collectPages(25, 0, false, func(_, _ int) ([]int, int, error) {
		return nil, 0, boom
	})
	if !errors.Is(err, boom) {
		t.Errorf("fetch error not propagated: %v", err)
	}
}
