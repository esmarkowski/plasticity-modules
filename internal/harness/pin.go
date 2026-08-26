package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PinFile is where a repository records the harness it expects.
//
// The same file plst already reads when a repository *provides* modules, under a
// different key: one file for a repository's relationship to plst, whether it
// provides or consumes. A second filename for the second meaning is how this gets
// confusing, and the core reads plst.json with a plain unmarshal, so an unknown
// key costs it nothing.
const PinFile = "plst.json"

// Pin is the harness a repository expects every checkout of it to be using.
//
// It exists because the alternative — machine-local state keyed by absolute path
// — cannot answer the question a team actually has. A fresh clone has no binding,
// two developers cannot be shown to be running the same configuration, and every
// git worktree is a separate absolute path and therefore a separate scope. A
// committed pin is inherited by all of them.
type Pin struct {
	// Source is owner/repo, or a URL. Required: a pin that only names a harness
	// works on the machine that already installed it and nowhere else.
	Source string `json:"source"`
	// Ref is the tag, branch, or commit to be on. Optional, and recommended: "the
	// same harness" without a version is not the same harness, because one
	// developer running `update` moves ahead of everyone else.
	Ref string `json:"ref,omitempty"`
	// Name overrides the local name derived from Source, for the case where two
	// repositories pin different harnesses whose repository names collide.
	Name string `json:"name,omitempty"`
}

// pinned is plst.json as this program reads it: one known key, everything else
// left alone.
type pinned struct {
	Harness *Pin `json:"harness,omitempty"`
}

// HarnessName is what the pinned harness is called on disk.
func (p Pin) HarnessName() (string, error) {
	if p.Name != "" {
		return p.Name, nil
	}
	_, name, _, err := parseSource(p.Source)
	return name, err
}

// LoadPin reads a repository's pin. Absent is not an error: most repositories do
// not pin one.
func LoadPin(root string) (Pin, bool, error) {
	b, err := os.ReadFile(filepath.Join(root, PinFile))
	if os.IsNotExist(err) {
		return Pin{}, false, nil
	}
	if err != nil {
		return Pin{}, false, err
	}
	var read pinned
	if err := json.Unmarshal(b, &read); err != nil {
		return Pin{}, false, fmt.Errorf("%s: %w", PinFile, err)
	}
	if read.Harness == nil {
		return Pin{}, false, nil
	}
	if read.Harness.Source == "" {
		return Pin{}, false, fmt.Errorf("%s: harness.source is required — a pin that only names a harness works on the machine that installed it and nowhere else", PinFile)
	}
	return *read.Harness, true, nil
}

// WritePin records a pin, leaving every other key in the file untouched.
//
// Read as raw keys rather than into a struct: this program knows about one key
// and a repository's own `modules` declaration must survive being written
// alongside it.
func WritePin(root string, p Pin) error {
	path := filepath.Join(root, PinFile)
	doc := map[string]json.RawMessage{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("%s: %w", PinFile, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	entry, err := json.Marshal(p)
	if err != nil {
		return err
	}
	doc["harness"] = entry
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// SetPin records a harness reference in a repository, parsing owner/repo@ref the
// same way `install` does so the two accept the same thing.
func SetPin(root, ref string) (Pin, error) {
	url, _, gitRef, err := parseSource(ref)
	if err != nil {
		return Pin{}, err
	}
	p := Pin{Source: url, Ref: gitRef}
	if err := WritePin(root, p); err != nil {
		return Pin{}, err
	}
	return p, nil
}

// SyncReport is what Sync did, on top of what applying the harness did.
type SyncReport struct {
	Report
	Pin Pin
	// Action is how the harness got to the pinned ref: installed, moved, or was
	// already there.
	Action string
	Commit string
}

// Sync brings a repository's pinned harness into place: installs it if it is
// missing, moves it to the pinned ref, and applies it at project scope.
//
// Idempotent, so it is safe in bin/setup, a postinstall hook, and CI. That is the
// point of it: reproducing a configuration should be one command that can be run
// twice.
func Sync(root string, say func(string)) (SyncReport, error) {
	p, ok, err := LoadPin(root)
	if err != nil {
		return SyncReport{}, err
	}
	if !ok {
		return SyncReport{}, fmt.Errorf("no harness pinned in %s — `plst harness pin <owner>/<repo>[@ref]` records one",
			filepath.Join(root, PinFile))
	}
	name, err := p.HarnessName()
	if err != nil {
		return SyncReport{}, err
	}
	rep := SyncReport{Pin: p}

	h, err := Find(name)
	if err != nil {
		source := p.Source
		if p.Ref != "" {
			source += "@" + p.Ref
		}
		if h, err = Install(source, say); err != nil {
			return SyncReport{}, err
		}
		rep.Action = "installed"
	} else {
		// A same-named harness from somewhere else is the failure this checks for:
		// it would apply cleanly and be the wrong configuration, which is exactly
		// what a pin exists to prevent. Said out loud rather than silently used.
		if err := sameSource(h, p); err != nil {
			return SyncReport{}, err
		}
		before := h.Commit
		if h, err = checkout(h, p.Ref, say); err != nil {
			return SyncReport{}, err
		}
		if h.Commit == before {
			rep.Action = "already current"
		} else {
			rep.Action = "moved to " + h.Commit
		}
	}
	rep.Commit = h.Commit

	applied, err := Use(name, ProjectScope(root), say)
	if err != nil {
		return SyncReport{}, err
	}
	rep.Report = applied
	return rep, nil
}

// sameSource reports whether an installed harness came from where the pin says.
func sameSource(h Harness, p Pin) error {
	if h.Source == "" {
		return fmt.Errorf("%q is installed but is not a git repository, so it cannot be checked against the pin — remove it, or give the pin a different `name`", h.Name)
	}
	if remote(h.Source) == remote(p.Source) {
		return nil
	}
	return fmt.Errorf("%q is installed from %s, but this repository pins %s — remove it, or give the pin a different `name`",
		h.Name, h.Source, p.Source)
}

// remote reduces a repository reference to something two spellings of the same
// remote agree on.
//
// owner/repo goes through the same normalisation `install` uses. Anything else —
// a self-hosted remote, an ssh URL, a local path — is compared as written, minus
// the .git and the trailing slash that are the two ways it differs from itself.
// Refusing what this program cannot normalise would refuse every remote that is
// not github, and the pin is exactly where a company's own host shows up.
func remote(ref string) string {
	if url, _, _, err := parseSource(ref); err == nil {
		return url
	}
	return strings.TrimSuffix(strings.TrimRight(ref, "/"), ".git")
}

// checkout moves a harness onto a ref, or fast-forwards the branch it is on when
// the pin names none.
//
// Refused when there is uncommitted work, for the same reason Update refuses it:
// a harness is edited in place, and a checkout that discards someone's changes is
// a checkout that loses them.
func checkout(h Harness, ref string, say func(string)) (Harness, error) {
	if h.Dirty {
		return h, fmt.Errorf("%q has uncommitted changes — commit or stash them before syncing", h.Name)
	}
	if err := git(h.Dir, "fetch", "--tags", "--prune", "origin"); err != nil {
		return h, fmt.Errorf("fetch %s: %w", h.Name, err)
	}
	if ref != "" && ref != h.Ref {
		say("checking out " + ref)
		if err := git(h.Dir, "-c", "advice.detachedHead=false", "checkout", "--quiet", ref); err != nil {
			return h, fmt.Errorf("checkout %s in %s: %w", ref, h.Name, err)
		}
	}
	// A branch is followed; a tag or a commit has no upstream and stays put.
	if gitLine(h.Dir, "symbolic-ref", "--quiet", "HEAD") != "" {
		if gitLine(h.Dir, "rev-parse", "--quiet", "--verify", "@{u}") != "" {
			if err := git(h.Dir, "merge", "--ff-only", "@{u}"); err != nil {
				return h, fmt.Errorf("fast-forward %s: %w", h.Name, err)
			}
		}
	}
	return Open(h.Dir)
}
