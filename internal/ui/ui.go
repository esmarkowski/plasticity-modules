// Package ui is how a module talks to a terminal.
//
// Separate from the packages that do the work, so those can be tested by what
// they return rather than by what they printed.
package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	fg    = lipgloss.AdaptiveColor{Light: "#1F2430", Dark: "#E4E6EB"}
	dim   = lipgloss.AdaptiveColor{Light: "#7A8194", Dark: "#767C8C"}
	faint = lipgloss.AdaptiveColor{Light: "#B4BAC8", Dark: "#454A57"}
	acc   = lipgloss.AdaptiveColor{Light: "#4A6FE0", Dark: "#7E9CFF"}

	Title = lipgloss.NewStyle().Foreground(fg).Bold(true)
	Name  = lipgloss.NewStyle().Foreground(acc).Bold(true)
	Desc  = lipgloss.NewStyle().Foreground(dim)
	Note  = lipgloss.NewStyle().Foreground(faint)
	Good  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#2F7D4F", Dark: "#63C68A"})
	Warn  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B4681A", Dark: "#E8A94A"})
	Bad   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B23A34", Dark: "#F0776E"})
)

// Say is progress: what a command is doing right now. On stderr, so a command
// whose output is read by something else stays readable.
func Say(w io.Writer, s string) { fmt.Fprintln(w, Note.Render("  "+s)) }

// Done reports a success worth confirming.
func Done(w io.Writer, s string) { fmt.Fprintln(w, Good.Render("✓ ")+s) }

// Fail reports a failure.
func Fail(w io.Writer, err error) { fmt.Fprintln(w, Bad.Render("✗ ")+err.Error()) }

// Pad right-pads to a display width, measured with lipgloss so styled content
// lines up.
func Pad(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// Wrap folds text to a width on word boundaries, with a margin.
func Wrap(text string, margin, width int) string {
	lead := strings.Repeat(" ", margin)
	room := width - margin
	if room < 20 {
		room = 20
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= room:
			line += " " + word
		default:
			out = append(out, lead+line)
			line = word
		}
	}
	if line != "" {
		out = append(out, lead+line)
	}
	return strings.Join(out, "\n")
}
