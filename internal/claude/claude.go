// Package claude is everything that knows the shape of an agent harness's own
// configuration directory.
//
// Isolated here because it is the only part of a module that depends on how a
// particular agent lays its files out, and because more than one module needs it:
// harness swaps these paths, and a hooks or skills module would read the same
// ones.
package claude

import (
	"os"
	"path/filepath"
)

// Dir is the agent's configuration directory.
//
// CLAUDE_CONFIG_DIR is the agent's own variable and wins, so pointing a whole
// session somewhere else works without plst needing an opinion about it.
func Dir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// SettingsPath is the user-scope settings file, where hooks are registered.
func SettingsPath() string { return filepath.Join(Dir(), "settings.json") }

// ProjectDir is a repository's own configuration directory.
func ProjectDir(root string) string { return filepath.Join(root, ".claude") }

// ProjectSettingsPath is a repository's settings file.
func ProjectSettingsPath(root string) string {
	return filepath.Join(ProjectDir(root), "settings.json")
}

// Component is one swappable part of a harness.
type Component struct {
	// Name is what the component is called in a harness, and what it is called
	// in the agent's directory. They are the same on purpose: a harness is
	// readable as the thing it becomes.
	Name string
	// Dir distinguishes a directory from a single file, because moving an
	// existing one aside has to know which it is.
	Dir bool
	// ProjectOnly marks a component the agent only ever reads from a project.
	//
	// Rules are the case, and it is not obvious: ~/.claude/rules is never read.
	// Measured on this machine, thirteen rule loads, every one of them from a
	// project's own .claude/rules and none from user scope. Linking one at user
	// scope would appear to work and do nothing at all.
	ProjectOnly bool
}

// Components are the parts of a harness, in the order they are reported.
//
// A closed list on purpose. The agent's configuration directory also holds live
// state — a daemon lock, job directories, a history file, plan files, caches —
// and swapping a harness must never touch any of it.
var Components = []Component{
	{Name: "CLAUDE.md"},
	{Name: "rules", Dir: true, ProjectOnly: true},
	{Name: "agents", Dir: true},
	{Name: "skills", Dir: true},
	{Name: "commands", Dir: true},
	{Name: "hooks", Dir: true},
}

// Component finds one by name.
func Find(name string) (Component, bool) {
	for _, c := range Components {
		if c.Name == name {
			return c, true
		}
	}
	return Component{}, false
}
