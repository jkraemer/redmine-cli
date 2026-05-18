// Package output formats data for stdout: pretty JSON or Markdown tables.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// JSON writes a 2-space-indented JSON document to w with a trailing newline.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// SanitizeForTerminal removes control bytes that a terminal emulator would
// otherwise interpret (ANSI/CSI/OSC escape sequences, BEL, DEL, NUL, CR, 8-bit
// C1 controls). \t and \n are preserved because callers depend on them for
// layout. Use this on any server-derived string that lands on stdout/stderr
// in markdown mode — JSON output is already safe via encoding/json escaping.
//
// Removing rather than escaping keeps the markdown structure stable for
// downstream consumers; the visible characters around the stripped bytes
// remain intact.
func SanitizeForTerminal(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t', r == '\n':
			return r
		case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0, r == utf8.RuneError:
			return -1
		}
		return r
	}, s)
}

// MarkdownTable writes a GitHub-flavored Markdown table to w. Column widths
// are sized to the widest cell. If rows is empty, writes "(no results)".
// Cells are sanitized via SanitizeForTerminal before width calculation and
// emission, so server-controlled values can't inject terminal escapes.
func MarkdownTable(w io.Writer, headers []string, rows [][]string) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "(no results)")
		return err
	}
	cleanHeaders := make([]string, len(headers))
	for i, h := range headers {
		cleanHeaders[i] = SanitizeForTerminal(h)
	}
	cleanRows := make([][]string, len(rows))
	for i, row := range rows {
		cleanRow := make([]string, len(row))
		for j, c := range row {
			cleanRow[j] = SanitizeForTerminal(c)
		}
		cleanRows[i] = cleanRow
	}

	widths := make([]int, len(cleanHeaders))
	for i, h := range cleanHeaders {
		widths[i] = len(h)
	}
	for _, row := range cleanRows {
		for i, c := range row {
			if i < len(widths) && len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}

	writeRow := func(cells []string) error {
		parts := make([]string, len(cells))
		for i, c := range cells {
			parts[i] = padRight(c, widths[i])
		}
		_, err := fmt.Fprintf(w, "| %s |\n", strings.Join(parts, " | "))
		return err
	}

	if err := writeRow(cleanHeaders); err != nil {
		return err
	}
	sep := make([]string, len(cleanHeaders))
	for i := range sep {
		sep[i] = strings.Repeat("-", widths[i])
	}
	if err := writeRow(sep); err != nil {
		return err
	}
	for _, r := range cleanRows {
		if err := writeRow(r); err != nil {
			return err
		}
	}
	return nil
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
