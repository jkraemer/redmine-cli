// Package agenthelp emits a structured JSON description of any cobra command.
package agenthelp

import (
	"encoding/json"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Flag describes a single cobra flag in JSON-friendly form.
type Flag struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type"`
	Default   string `json:"default,omitempty"`
	Usage     string `json:"usage"`
}

// Subcommand is a name + short description.
type Subcommand struct {
	Name    string `json:"name"`
	Short   string `json:"short"`
	Path    string `json:"path"`
	Blocked bool   `json:"blocked,omitempty"`
}

// Help is the rendered output structure.
type Help struct {
	Command        string       `json:"command"`
	Path           string       `json:"path"`
	Short          string       `json:"short"`
	Long           string       `json:"long,omitempty"`
	Usage          string       `json:"usage"`
	ReadOnly       bool         `json:"read_only,omitempty"`
	Blocked        bool         `json:"blocked,omitempty"`
	Flags          []Flag       `json:"flags"`
	InheritedFlags []Flag       `json:"inherited_flags,omitempty"`
	Subcommands    []Subcommand `json:"subcommands,omitempty"`
	Notes          []string     `json:"notes,omitempty"`
}

// Render writes a JSON Help document for cmd to w. When readOnly is true the
// document advertises that read-only mode is active: every command carries
// read_only, and write commands (those annotated "write") are marked blocked
// with an explanatory note, so an agent discovers the restriction without
// running the command.
func Render(w io.Writer, cmd *cobra.Command, readOnly bool) error {
	h := Help{
		Command:  cmd.Name(),
		Path:     cmd.CommandPath(),
		Short:    cmd.Short,
		Long:     cmd.Long,
		Usage:    cmd.UseLine(),
		ReadOnly: readOnly,
	}
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		h.Flags = append(h.Flags, flagToJSON(f))
	})
	cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) {
		h.InheritedFlags = append(h.InheritedFlags, flagToJSON(f))
	})
	for _, c := range cmd.Commands() {
		if c.Hidden {
			continue
		}
		h.Subcommands = append(h.Subcommands, Subcommand{
			Name:    c.Name(),
			Short:   c.Short,
			Path:    c.CommandPath(),
			Blocked: readOnly && isWrite(c),
		})
	}
	if notes, ok := cmd.Annotations["agent-notes"]; ok && notes != "" {
		h.Notes = append(h.Notes, notes)
	}
	if readOnly && isWrite(cmd) {
		h.Blocked = true
		h.Notes = append(h.Notes, "read-only mode is active: this write command is blocked; running it with --confirm exits 8")
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(h)
}

// isWrite reports whether cmd is a server-mutating command, as declared by the
// "write" annotation the command layer sets on every --confirm command.
func isWrite(cmd *cobra.Command) bool {
	return cmd.Annotations["write"] == "true"
}

func flagToJSON(f *pflag.Flag) Flag {
	return Flag{
		Name:      f.Name,
		Shorthand: f.Shorthand,
		Type:      f.Value.Type(),
		Default:   f.DefValue,
		Usage:     f.Usage,
	}
}
