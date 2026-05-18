package api

import (
	"fmt"

	"github.com/jkraemer/redmine-cli/internal/output"
)

// Error is returned for non-2xx responses.
type Error struct {
	Status int
	Body   string // first ~512 bytes of response body
	URL    string
}

// Error renders the wrapped status/URL/body for human display. The Body
// excerpt is server-controlled, so it is run through output.SanitizeForTerminal
// to strip ANSI escape sequences before it reaches stderr.
func (e *Error) Error() string {
	return fmt.Sprintf("redmine API %d at %s: %s", e.Status, e.URL, output.SanitizeForTerminal(e.Body))
}
