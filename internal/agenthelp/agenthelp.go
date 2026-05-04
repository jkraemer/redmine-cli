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
	Name  string `json:"name"`
	Short string `json:"short"`
	Path  string `json:"path"`
}

// Help is the rendered output structure.
type Help struct {
	Command        string       `json:"command"`
	Path           string       `json:"path"`
	Short          string       `json:"short"`
	Long           string       `json:"long,omitempty"`
	Usage          string       `json:"usage"`
	Flags          []Flag       `json:"flags"`
	InheritedFlags []Flag       `json:"inherited_flags,omitempty"`
	Subcommands    []Subcommand `json:"subcommands,omitempty"`
	Notes          []string     `json:"notes,omitempty"`
}

// Render writes a JSON Help document for cmd to w.
func Render(w io.Writer, cmd *cobra.Command) error {
	h := Help{
		Command: cmd.Name(),
		Path:    cmd.CommandPath(),
		Short:   cmd.Short,
		Long:    cmd.Long,
		Usage:   cmd.UseLine(),
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
			Name:  c.Name(),
			Short: c.Short,
			Path:  c.CommandPath(),
		})
	}
	if notes, ok := cmd.Annotations["agent-notes"]; ok && notes != "" {
		h.Notes = []string{notes}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(h)
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
