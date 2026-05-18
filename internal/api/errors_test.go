package api

import (
	"strings"
	"testing"
)

// The Body field is a verbatim excerpt of the server response. When err.Error()
// is printed to a terminal (the CLI does this on every failed command), any
// ANSI escape sequences the server injected would be interpreted by the
// terminal — clipboard hijack via OSC 52, screen clear, etc. They must be
// stripped on render. URL and Status are CLI-controlled and pass through.
func TestError_Error_StripsTerminalEscapesFromBody(t *testing.T) {
	e := &Error{
		Status: 422,
		URL:    "https://example.test/x",
		Body:   "validation\x1b]52;c;PAYLOAD\x07failed\x07end",
	}
	got := e.Error()
	for _, bad := range []string{"\x1b", "\x07"} {
		if strings.Contains(got, bad) {
			t.Errorf("Error() contains raw control byte %q: %q", bad, got)
		}
	}
	for _, want := range []string{"validation", "PAYLOAD", "failed", "end", "422", "https://example.test/x"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() missing %q: %q", want, got)
		}
	}
}
