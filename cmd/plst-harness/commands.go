package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/esmarkowski/plasticity-modules/internal/claude"
	"github.com/esmarkowski/plasticity-modules/internal/harness"
	"github.com/esmarkowski/plasticity-modules/internal/ui"
)

// say reports progress on stderr, so a command whose output is piped stays clean.
func say(s string) { ui.Say(os.Stderr, s) }

// scopeOf reads --project, which is the only scope choice there is.
//
// User scope is the default because a harness is normally the whole way you work.
// A project scope exists because rules only work there, and because a repository
// sometimes needs its own.
func scopeOf(args []string) (harness.Scope, error) {
	if !hasFlag(args, "--project") {
		return harness.User, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root := repoRoot(wd)
	return harness.ProjectScope(root), nil
}

// repoRoot walks up to the repository, falling back to the working directory.
// A harness applied to "the project" should land at its root and not in whatever
// subdirectory the command was typed in.
func repoRoot(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

func list(args []string) int {
	hs, err := harness.List()
	if err != nil {
		ui.Fail(os.Stderr, err)
		return 1
	}
	state := harness.LoadState()

	// What is applied leads, because it is the question being asked.
	if scopes := state.Order(); len(scopes) > 0 {
		fmt.Println(ui.Note.Render("APPLIED"))
		for _, sc := range scopes {
			a, _ := state.Active(sc)
			fmt.Printf("  %s%s\n", ui.Pad(ui.Name.Render(a.Harness), 22),
				ui.Desc.Render(sc.Label()))
		}
		fmt.Println()
	}

	if len(hs) == 0 {
		fmt.Println(ui.Desc.Render("no harnesses installed"))
		fmt.Println(ui.Desc.Render("  ") + ui.Name.Render("plst harness install <owner>/<repo>") +
			ui.Desc.Render(" or ") + ui.Name.Render("plst harness new <name>"))
		return 0
	}
	fmt.Println(ui.Note.Render("INSTALLED"))
	for _, h := range hs {
		line := "  " + ui.Pad(ui.Name.Render(h.Name), 22)
		if d := h.Manifest.Description; d != "" {
			line += ui.Desc.Render(d)
		} else {
			line += ui.Desc.Render(strings.Join(componentNames(h), ", "))
		}
		fmt.Println(line)
		if h.Commit != "" {
			state := h.Ref + " " + h.Commit
			if h.Dirty {
				state += ui.Warn.Render(" uncommitted")
			}
			fmt.Println("    " + ui.Note.Render(state))
		}
	}
	return 0
}

func componentNames(h harness.Harness) []string {
	var out []string
	for _, c := range harness.Components(h.Dir) {
		out = append(out, c.Name)
	}
	if len(out) == 0 {
		return []string{"empty"}
	}
	return out
}

func install(args []string) int {
	ref := positional(args)
	if ref == "" {
		ui.Fail(os.Stderr, fmt.Errorf("usage: plst harness install <owner>/<repo>[@ref]"))
		return 2
	}
	h, err := harness.Install(ref, say)
	if err != nil {
		ui.Fail(os.Stderr, err)
		return 1
	}
	ui.Done(os.Stdout, "installed "+h.Name)
	printContents(h)
	fmt.Println(ui.Desc.Render("  apply it with ") + ui.Name.Render("plst harness use "+h.Name))
	return 0
}

func newHarness(args []string) int {
	name := positional(args)
	if name == "" {
		ui.Fail(os.Stderr, fmt.Errorf("usage: plst harness new <name>"))
		return 2
	}
	h, err := harness.New(name, say)
	if err != nil {
		ui.Fail(os.Stderr, err)
		return 1
	}
	ui.Done(os.Stdout, "created "+h.Name)
	fmt.Println("  " + ui.Note.Render(h.Dir))
	fmt.Println(ui.Desc.Render("  put instructions in CLAUDE.md, agents in agents/, hooks in hooks/,"))
	fmt.Println(ui.Desc.Render("  and declare the hooks in ") + ui.Name.Render(harness.ManifestFile))
	return 0
}

func use(args []string) int {
	name := positional(args)
	if name == "" {
		ui.Fail(os.Stderr, fmt.Errorf("usage: plst harness use <name> [--project]"))
		return 2
	}
	scope, err := scopeOf(args)
	if err != nil {
		ui.Fail(os.Stderr, err)
		return 1
	}
	rep, err := harness.Use(name, scope, say)
	if err != nil {
		ui.Fail(os.Stderr, err)
		return 1
	}
	if rep.Replaced != "" {
		say("replaced " + rep.Replaced)
	}
	ui.Done(os.Stdout, fmt.Sprintf("%s applied at %s", rep.Harness, scope.Label()))
	if len(rep.Linked) > 0 {
		fmt.Println("  " + ui.Desc.Render("linked   ") + strings.Join(rep.Linked, ", "))
	}
	if len(rep.Hooks) > 0 {
		fmt.Println("  " + ui.Desc.Render("hooks    ") +
			fmt.Sprintf("%d registered in %s", len(rep.Hooks), shortPath(settingsFor(scope))))
	}
	if len(rep.Parked) > 0 {
		fmt.Println("  " + ui.Warn.Render("moved    ") + strings.Join(rep.Parked, ", ") +
			ui.Desc.Render(" — what was there is kept, not deleted"))
	}
	for _, s := range rep.Skipped {
		fmt.Println("  " + ui.Warn.Render("skipped  ") + s +
			ui.Desc.Render(" — the agent only reads this from a project; use --project in a repo"))
	}
	fmt.Println(ui.Note.Render("  takes effect in sessions started from here on"))
	return 0
}

func off(args []string) int {
	scope, err := scopeOf(args)
	if err != nil {
		ui.Fail(os.Stderr, err)
		return 1
	}
	rep, err := harness.Off(scope, say)
	if err != nil {
		ui.Fail(os.Stderr, err)
		return 1
	}
	ui.Done(os.Stdout, fmt.Sprintf("%s removed from %s", rep.Harness, scope.Label()))
	if len(rep.Parked) > 0 {
		fmt.Println("  " + ui.Desc.Render("restored ") + strings.Join(rep.Parked, ", "))
	}
	return 0
}

func show(args []string) int {
	name := positional(args)
	if name == "" {
		ui.Fail(os.Stderr, fmt.Errorf("usage: plst harness show <name>"))
		return 2
	}
	h, err := harness.Find(name)
	if err != nil {
		ui.Fail(os.Stderr, err)
		return 1
	}
	fmt.Println(ui.Title.Render(h.Name))
	if d := h.Manifest.Description; d != "" {
		fmt.Println(ui.Desc.Render("  " + d))
	}
	fmt.Println("  " + ui.Note.Render(h.Dir))
	if h.Source != "" {
		fmt.Println("  " + ui.Note.Render(h.Source+" "+h.Ref+" "+h.Commit))
	}
	printContents(h)

	if len(h.Manifest.Hooks) > 0 {
		fmt.Println()
		fmt.Println(ui.Note.Render("  HOOKS"))
		for event, groups := range h.Manifest.Hooks {
			for _, cmd := range claude.Commands(groups) {
				fmt.Printf("    %s%s\n", ui.Pad(ui.Desc.Render(event), 24), ui.Note.Render(cmd))
			}
		}
	}
	return 0
}

func printContents(h harness.Harness) {
	comps := harness.Components(h.Dir)
	if len(comps) == 0 {
		fmt.Println("  " + ui.Warn.Render("no components — nothing to link"))
		return
	}
	fmt.Println()
	fmt.Println(ui.Note.Render("  PROVIDES"))
	for _, c := range comps {
		note := ""
		if c.ProjectOnly {
			note = ui.Warn.Render("project scope only")
		}
		fmt.Printf("    %s%s\n", ui.Pad(c.Name, 14), note)
	}
}

func update(args []string) int {
	name := positional(args)
	if name == "" {
		ui.Fail(os.Stderr, fmt.Errorf("usage: plst harness update <name>"))
		return 2
	}
	h, err := harness.Update(name, say)
	if err != nil {
		ui.Fail(os.Stderr, err)
		return 1
	}
	ui.Done(os.Stdout, fmt.Sprintf("%s now on %s", h.Name, h.Commit))
	// The links point at the directory, so a pull is picked up with nothing to
	// re-apply. Hooks are the exception: they were expanded into the settings
	// file, so a changed manifest needs writing again.
	state := harness.LoadState()
	for _, sc := range state.Order() {
		if a, ok := state.Active(sc); ok && a.Harness == h.Name {
			fmt.Println(ui.Note.Render("  applied at " + sc.Label() +
				" — re-run `use` if its hooks changed"))
		}
	}
	return 0
}

func remove(args []string) int {
	name := positional(args)
	if name == "" {
		ui.Fail(os.Stderr, fmt.Errorf("usage: plst harness remove <name>"))
		return 2
	}
	// Taken out of every scope first, so removing a harness never leaves a dead
	// symlink or a hook pointing at a directory that is gone.
	state := harness.LoadState()
	for _, sc := range state.Order() {
		if a, ok := state.Active(sc); ok && a.Harness == name {
			if _, err := harness.Off(sc, say); err != nil {
				ui.Fail(os.Stderr, err)
				return 1
			}
		}
	}
	if err := harness.Remove(name); err != nil {
		ui.Fail(os.Stderr, err)
		return 1
	}
	ui.Done(os.Stdout, "removed "+name)
	return 0
}

func settingsFor(scope harness.Scope) string {
	if root, ok := scope.IsProject(); ok {
		return claude.ProjectSettingsPath(root)
	}
	return claude.SettingsPath()
}

// shortPath abbreviates a home-relative path, which is most of them.
func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}
