// Package harness manages interchangeable sets of agent configuration.
//
// A harness is a directory — usually a git repository — holding the parts an
// agent reads: CLAUDE.md, rules, agents, skills, commands, and hooks. Using one
// links those parts into place and registers its hooks; using another swaps them.
// Nothing is copied, so a harness stays the single copy of its own files and
// edits made while it is active are edits to it.
package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/esmarkowski/plasticity-modules/internal/claude"
)

// ManifestFile is a harness's own account of itself.
const ManifestFile = "harness.json"

// RootVar is the placeholder a harness uses instead of writing down where it was
// installed.
//
// The same idea as the agent's own ${CLAUDE_PLUGIN_ROOT}: a harness is a git
// repository that may be cloned anywhere, by anyone, so a hook command that
// names a path is a hook command that works on one machine.
const RootVar = "${HARNESS_ROOT}"

// Manifest is what a harness declares.
type Manifest struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	// Hooks are declared in the same shape the agent's settings file uses, so a
	// harness author writes what they already know and plst does not invent a
	// second dialect for the same thing.
	Hooks map[string][]claude.HookGroup `json:"hooks,omitempty"`
}

// LoadManifest reads a harness's manifest. Optional: a harness that is only
// instructions and agents needs to declare nothing, and gets its name from its
// directory.
func LoadManifest(dir string) (Manifest, error) {
	m := Manifest{Name: filepath.Base(dir)}
	b, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	var read Manifest
	if err := json.Unmarshal(b, &read); err != nil {
		return m, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	if read.Name == "" {
		read.Name = m.Name
	}
	return read, nil
}

// Expand replaces the root placeholder throughout a manifest's hooks, giving the
// registrations that will actually be written.
//
// Done on a copy: the manifest is read from a file the harness owns, and a
// harness that has been used once must not come back with a machine's paths
// baked into it.
func (m Manifest) Expand(root string) map[string][]claude.HookGroup {
	if len(m.Hooks) == 0 {
		return nil
	}
	out := make(map[string][]claude.HookGroup, len(m.Hooks))
	for event, groups := range m.Hooks {
		expanded := make([]claude.HookGroup, 0, len(groups))
		for _, g := range groups {
			hooks := make([]json.RawMessage, 0, len(g.Hooks))
			for _, h := range g.Hooks {
				hooks = append(hooks, json.RawMessage(
					strings.ReplaceAll(string(h), RootVar, root)))
			}
			expanded = append(expanded, claude.HookGroup{Matcher: g.Matcher, Hooks: hooks})
		}
		out[event] = expanded
	}
	return out
}

// Components reports which parts a harness actually provides, in the canonical
// order. Read from the directory rather than declared, so adding a rules
// directory to a harness is enough to have it linked.
func Components(dir string) []claude.Component {
	var out []claude.Component
	for _, c := range claude.Components {
		if _, err := os.Stat(filepath.Join(dir, c.Name)); err == nil {
			out = append(out, c)
		}
	}
	return out
}
