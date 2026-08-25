package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/esmarkowski/plasticity-modules/internal/claude"
)

// Report is what applying a harness did, for the caller to print.
type Report struct {
	Harness  string
	Scope    Scope
	Linked   []string
	Parked   []string
	Skipped  []string
	Hooks    []string
	Replaced string
}

// Use puts a harness in place.
//
// Symlinks, not copies. It was worth checking rather than assuming: a scratch
// project with a symlinked CLAUDE.md, a symlinked agents directory, and an
// agents directory holding a symlinked entry were each probed for what the agent
// reported loading, and all three loaded — directory links and entry links alike.
// So a harness stays the one copy of its own files, an edit made while it is
// active is an edit to the harness, and swapping is instant.
func Use(name string, scope Scope, say func(string)) (Report, error) {
	h, err := Find(name)
	if err != nil {
		return Report{}, err
	}
	target, settings, err := scopePaths(scope)
	if err != nil {
		return Report{}, err
	}

	state := LoadState()
	rep := Report{Harness: h.Name, Scope: scope}

	// Whatever is already here comes out first. Applying over the top would leave
	// the previous harness's hooks registered and its parked files unclaimed.
	if prev, ok := state.Active(scope); ok {
		if prev.Harness == h.Name && prev.Dir == h.Dir {
			say("re-linking " + h.Name)
		} else {
			rep.Replaced = prev.Harness
		}
		if err := revert(&state, scope, prev); err != nil {
			return Report{}, err
		}
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	applied := Applied{Harness: h.Name, Dir: h.Dir, At: time.Now(), Settings: settings}
	_, isProject := scope.IsProject()

	for _, c := range Components(h.Dir) {
		if c.ProjectOnly && !isProject {
			// Saying so rather than linking it: the link would be created, look
			// correct, and never be read.
			rep.Skipped = append(rep.Skipped, c.Name)
			continue
		}
		path := filepath.Join(target, c.Name)
		src := filepath.Join(h.Dir, c.Name)

		link := Link{Path: path, Target: src}
		parked, err := clear(path, stamp, scope)
		if err != nil {
			return Report{}, err
		}
		if parked != "" {
			link.Parked = parked
			rep.Parked = append(rep.Parked, c.Name)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Report{}, err
		}
		if err := os.Symlink(src, path); err != nil {
			return Report{}, fmt.Errorf("link %s: %w", c.Name, err)
		}
		applied.Links = append(applied.Links, link)
		rep.Linked = append(rep.Linked, c.Name)
	}

	cmds, err := registerHooks(settings, h)
	if err != nil {
		return Report{}, err
	}
	applied.Commands = cmds
	rep.Hooks = cmds

	state.Scopes[scope] = applied
	if err := state.Save(); err != nil {
		return Report{}, err
	}
	return rep, nil
}

// Off takes a harness out of a scope and puts back whatever it displaced.
func Off(scope Scope, say func(string)) (Report, error) {
	state := LoadState()
	prev, ok := state.Active(scope)
	if !ok {
		return Report{}, fmt.Errorf("no harness is applied at %s", scope.Label())
	}
	say("removing " + prev.Harness)
	if err := revert(&state, scope, prev); err != nil {
		return Report{}, err
	}
	if err := state.Save(); err != nil {
		return Report{}, err
	}
	rep := Report{Harness: prev.Harness, Scope: scope}
	for _, l := range prev.Links {
		rep.Linked = append(rep.Linked, filepath.Base(l.Path))
		if l.Parked != "" {
			rep.Parked = append(rep.Parked, filepath.Base(l.Path))
		}
	}
	return rep, nil
}

// revert undoes one Applied: the links it made, the files it moved, the hooks it
// registered. Best effort throughout — a half-reverted scope is worse than a
// reverted one with a complaint, so it keeps going and reports at the end.
func revert(state *State, scope Scope, a Applied) error {
	var failed []string
	for _, l := range a.Links {
		// Only a link this program made is removed. If the path is now a real
		// directory, someone replaced it deliberately and it is not plst's to
		// delete.
		if fi, err := os.Lstat(l.Path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(l.Path); err != nil {
				failed = append(failed, l.Path)
				continue
			}
		}
		if l.Parked == "" {
			continue
		}
		if _, err := os.Lstat(l.Path); err == nil {
			// Something is in the way; leaving the parked copy where it is beats
			// overwriting whatever that is.
			failed = append(failed, l.Parked)
			continue
		}
		if err := os.Rename(l.Parked, l.Path); err != nil {
			failed = append(failed, l.Parked)
		}
	}
	if err := unregisterHooks(a.Settings, a.Commands); err != nil {
		return err
	}
	delete(state.Scopes, scope)
	if len(failed) > 0 {
		return fmt.Errorf("could not restore: %s", strings.Join(failed, ", "))
	}
	return nil
}

// clear makes a path available, moving anything real out of the way.
//
// A symlink is removed outright: the only symlinks at these paths are ones this
// program made, and a broken one from a harness that was deleted by hand should
// not stop a new one going in. Anything else is moved, never deleted.
func clear(path, stamp string, scope Scope) (parked string, err error) {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", os.Remove(path)
	}
	dir, err := parkDir(stamp + "-" + scopeSlug(scope))
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, filepath.Base(path))
	if err := os.Rename(path, dest); err != nil {
		return "", fmt.Errorf("move %s aside: %w", path, err)
	}
	return dest, nil
}

// scopeSlug makes a scope usable as a directory name.
func scopeSlug(scope Scope) string {
	if root, ok := scope.IsProject(); ok {
		return "project" + strings.ReplaceAll(root, string(os.PathSeparator), "-")
	}
	return "user"
}

// scopePaths is where a scope's components and settings live.
func scopePaths(scope Scope) (target, settings string, err error) {
	if root, ok := scope.IsProject(); ok {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", "", err
		}
		return claude.ProjectDir(abs), claude.ProjectSettingsPath(abs), nil
	}
	return claude.Dir(), claude.SettingsPath(), nil
}
