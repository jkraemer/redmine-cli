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
