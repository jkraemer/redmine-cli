package api

import "fmt"

// Error is returned for non-2xx responses.
type Error struct {
	Status int
	Body   string // first ~512 bytes of response body
	URL    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("redmine API %d at %s: %s", e.Status, e.URL, e.Body)
}
