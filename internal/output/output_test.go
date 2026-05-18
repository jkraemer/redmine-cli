package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestJSON_Object(t *testing.T) {
	var buf bytes.Buffer
	err := JSON(&buf, map[string]any{"a": 1, "b": "x"})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"a": 1`) || !strings.Contains(out, `"b": "x"`) {
		t.Errorf("unexpected output: %s", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("expected trailing newline")
	}
}

func TestMarkdownTable_Basic(t *testing.T) {
	var buf bytes.Buffer
	headers := []string{"ID", "Name"}
	rows := [][]string{{"1", "alpha"}, {"22", "beta"}}
	if err := MarkdownTable(&buf, headers, rows); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	want := "| ID | Name  |\n| -- | ----- |\n| 1  | alpha |\n| 22 | beta  |\n"
	if out != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
}

func TestMarkdownTable_EmptyRows(t *testing.T) {
	var buf bytes.Buffer
	if err := MarkdownTable(&buf, []string{"A"}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "(no results)") {
		t.Errorf("expected empty marker: %s", buf.String())
	}
}

// Server-controlled cell content may contain ANSI escape sequences or other
// terminal control bytes. MarkdownTable feeds a TTY, so it must strip those
// before writing — otherwise a malicious Redmine user can hijack the
// victim's terminal (OSC 52 clipboard write, screen clear, etc.).
func TestMarkdownTable_StripsControlBytesFromCells(t *testing.T) {
	var buf bytes.Buffer
	headers := []string{"ID", "Name"}
	rows := [][]string{
		{"1", "Innocent\x1b]52;c;PAYLOAD\x07name"},
		{"2", "with\x07bell"},
	}
	if err := MarkdownTable(&buf, headers, rows); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, bad := range []string{"\x1b", "\x07"} {
		if strings.Contains(out, bad) {
			t.Errorf("output contains raw control byte %q: %q", bad, out)
		}
	}
	// The visible parts should survive.
	for _, want := range []string{"Innocent", "PAYLOAD", "name", "withbell"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got %q", want, out)
		}
	}
}

func TestSanitizeForTerminal(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain ASCII", "plain ASCII"},
		{"tab\there\nnewline", "tab\there\nnewline"},                          // \t and \n preserved
		{"\x1b]52;c;PAYLOAD\x07", "]52;c;PAYLOAD"},                            // ESC and BEL stripped
		{"\x1b[2J\x1b[H", "[2J[H"},                                            // CSI sequences stripped
		{"bell\x07end", "bellend"},                                            // \x07
		{"null\x00mid", "nullmid"},                                            // \x00
		{"del\x7fmid", "delmid"},                                              // 0x7f
		{"c1\x9bcsi", "c1csi"},                                                // 0x9b (8-bit CSI)
		{"unicode é ✓", "unicode é ✓"},                                        // multibyte printable preserved
		{"carriage\rreturn", "carriagereturn"},                                // \r stripped (overwrite risk)
	}
	for _, tc := range cases {
		if got := SanitizeForTerminal(tc.in); got != tc.want {
			t.Errorf("SanitizeForTerminal(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
