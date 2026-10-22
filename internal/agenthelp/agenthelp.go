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
	Locked         bool         `json:"locked,omitempty"`
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
// running the command. When locked is true the document also carries locked,
// and write and auth commands are marked blocked.
func Render(w io.Writer, cmd *cobra.Command, readOnly, locked bool) error {
	h := Help{
		Command:  cmd.Name(),
		Path:     cmd.CommandPath(),
		Short:    cmd.Short,
		Long:     cmd.Long,
		Usage:    cmd.UseLine(),
		ReadOnly: readOnly,
		Locked:   locked,
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
			Blocked: (readOnly && IsWrite(c)) || (locked && isLockedOut(c)),
		})
	}
	if notes, ok := cmd.Annotations["agent-notes"]; ok && notes != "" {
		h.Notes = append(h.Notes, notes)
	}
	if readOnly && IsWrite(cmd) {
		h.Blocked = true
		h.Notes = append(h.Notes, "read-only mode is active: this write command is blocked; running it with --confirm exits 8")
	}
	if locked {
		h.Notes = append(h.Notes, "locked mode (redmine-safe) is active: write and auth commands are unavailable and exit 8; --config is refused; attachments download writes to stdout only")
		if isLockedOut(cmd) {
			h.Blocked = true
			h.Notes = append(h.Notes, "locked mode is active: this command is unavailable and exits 8")
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(h)
}

// IsWrite reports whether cmd is a server-mutating command, as declared by the
// "write" annotation the command layer sets on every --confirm command.
func IsWrite(cmd *cobra.Command) bool {
	return cmd.Annotations["write"] == "true"
}

// isLockedOut reports whether locked mode refuses cmd outright.
func isLockedOut(cmd *cobra.Command) bool {
	return IsWrite(cmd) || IsAuth(cmd)
}

// IsAuth reports whether cmd is the top-level auth command or any of its
// descendants. These commands read and write OAuth credentials.
func IsAuth(cmd *cobra.Command) bool {
	for c := cmd; c.HasParent(); c = c.Parent() {
		if !c.Parent().HasParent() {
			return c.Name() == "auth"
		}
	}
	return false
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
