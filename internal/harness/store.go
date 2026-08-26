package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/esmarkowski/plasticity-modules/internal/plst"
)

// prefixes are the spellings of a github remote that all mean owner/repo.
var prefixes = []string{"https://github.com/", "http://github.com/", "git@github.com:", "github.com/"}

// Harness is one installed harness.
type Harness struct {
	Name     string
	Dir      string
	Manifest Manifest
	// Source is the repository it was cloned from, empty for one made locally.
	Source string
	// Ref is the branch or tag it tracks, and Commit what it is on now. Both
	// empty for a harness that is not a git repository, which is allowed: a
	// directory of files is a perfectly good harness.
	Ref    string
	Commit string
	Dirty  bool
}

// Root is where harnesses are kept.
func Root() (string, error) { return plst.Dir("harnesses") }

// List is every installed harness, by name.
func List() ([]Harness, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Harness
	for _, e := range entries {
		// A dotted entry is plst's own bookkeeping, not a harness.
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		h, err := Open(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Open reads one harness from disk.
func Open(dir string) (Harness, error) {
	m, err := LoadManifest(dir)
	if err != nil {
		return Harness{}, err
	}
	h := Harness{Name: filepath.Base(dir), Dir: dir, Manifest: m}
	h.Source, h.Ref, h.Commit, h.Dirty = gitState(dir)
	return h, nil
}

// Find one harness by name.
func Find(name string) (Harness, error) {
	root, err := Root()
	if err != nil {
		return Harness{}, err
	}
	dir := filepath.Join(root, name)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return Harness{}, fmt.Errorf("no harness named %q — `plst harness list` shows what is installed", name)
	}
	return Open(dir)
}

// Install clones a harness from a repository.
func Install(ref string, say func(string)) (Harness, error) {
	url, name, gitRef, err := parseSource(ref)
	if err != nil {
		return Harness{}, err
	}
	root, err := Root()
	if err != nil {
		return Harness{}, err
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(dir); err == nil {
		return Harness{}, fmt.Errorf("%q is already installed — `plst harness update %s` to pull it", name, name)
	}

	say("cloning " + url)
	// Cloned into place rather than staged and moved: a harness is a working
	// repository the user is expected to commit in, and moving a git directory
	// around behind their back is a good way to break a remote.
	// The detached-HEAD advice is git telling someone they might lose work, which
	// is not what pinning a tag is. Silenced so a normal sync does not read like a
	// warning.
	args := []string{"-c", "advice.detachedHead=false", "clone"}
	if gitRef != "" {
		args = append(args, "--branch", gitRef)
	}
	args = append(args, url, dir)
	cmd := exec.Command("git", args...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		os.RemoveAll(dir)
		return Harness{}, fmt.Errorf("clone %s: %w", url, err)
	}
	return Open(dir)
}

// Update pulls a harness. Refused when there is uncommitted work, because a
// harness is edited in place and a pull that stashes someone's changes is a pull
// that loses them.
func Update(name string, say func(string)) (Harness, error) {
	h, err := Find(name)
	if err != nil {
		return Harness{}, err
	}
	if h.Source == "" {
		return Harness{}, fmt.Errorf("%q is not a git repository — nothing to pull", name)
	}
	if h.Dirty {
		return Harness{}, fmt.Errorf("%q has uncommitted changes — commit or stash them first", name)
	}
	say("pulling " + h.Source)
	if err := git(h.Dir, "pull", "--ff-only"); err != nil {
		return Harness{}, fmt.Errorf("pull %s: %w", name, err)
	}
	return Open(h.Dir)
}

// New scaffolds an empty harness.
func New(name string, say func(string)) (Harness, error) {
	root, err := Root()
	if err != nil {
		return Harness{}, err
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(dir); err == nil {
		return Harness{}, fmt.Errorf("%q already exists", name)
	}
	for _, sub := range []string{"rules", "agents", "skills", "commands", "hooks"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return Harness{}, err
		}
		// A .gitkeep, because an empty directory is not a thing git can carry and
		// a harness cloned without its directories is a harness that links
		// nothing.
		if err := os.WriteFile(filepath.Join(dir, sub, ".gitkeep"), nil, 0o644); err != nil {
			return Harness{}, err
		}
	}
	files := map[string]string{
		ManifestFile: fmt.Sprintf(`{
  "name": %q,
  "description": "",
  "hooks": {}
}
`, name),
		"CLAUDE.md": "# " + name + "\n\nInstructions for this harness.\n",
		"README.md": harnessReadme(name),
	}
	for f, body := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			return Harness{}, err
		}
	}
	// A repository from the start, since the point of a harness is that it is
	// versioned and shareable.
	if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err == nil {
		say("initialised a git repository")
	}
	return Open(dir)
}

// Remove deletes an installed harness.
func Remove(name string) error {
	h, err := Find(name)
	if err != nil {
		return err
	}
	if h.Dirty {
		return fmt.Errorf("%q has uncommitted changes — commit them, or delete %s yourself", name, h.Dir)
	}
	return os.RemoveAll(h.Dir)
}

// parseSource reads a harness reference: owner/repo, a URL, or either with @ref.
func parseSource(ref string) (url, name, gitRef string, err error) {
	raw := ref
	// A trailing @ref, but only when the @ comes after the last path separator: an
	// ssh remote has an @ in its host, and that is not a version.
	if at := strings.LastIndex(ref, "@"); at > 0 && at > strings.LastIndex(ref, "/") {
		ref, gitRef = ref[:at], ref[at+1:]
	}
	short, github := ref, false
	for _, prefix := range prefixes {
		if strings.HasPrefix(short, prefix) {
			short, github = strings.TrimPrefix(short, prefix), true
			break
		}
	}
	short = strings.TrimSuffix(strings.Trim(short, "/"), ".git")

	// owner/repo, either spelled bare or with a github prefix already taken off.
	// The elsewhere check matters: git@gitlab.example.com:team/repo also splits
	// into two on the slash, and calling that a github repository would rewrite
	// someone's remote into one that does not exist.
	if parts := strings.Split(short, "/"); len(parts) == 2 && parts[0] != "" && parts[1] != "" &&
		(github || !elsewhere(short)) {
		return fmt.Sprintf("https://github.com/%s/%s.git", parts[0], parts[1]), harnessName(parts[1]), gitRef, nil
	}
	// Anything else git can clone is still a harness source: a self-hosted
	// remote, an ssh URL, a local path. Taken as written, because refusing them
	// would mean a harness — and so a repository's pin — could only ever name
	// github.
	if elsewhere(ref) {
		return ref, harnessName(path.Base(short)), gitRef, nil
	}
	return "", "", "", fmt.Errorf("cannot read %q as owner/repo, a URL, or a path", raw)
}

// elsewhere reports whether a reference names somewhere other than a github
// owner/repo: it has a scheme, a host, or is a path.
func elsewhere(ref string) bool {
	return strings.Contains(ref, "://") || strings.Contains(ref, ":") ||
		filepath.IsAbs(ref) || strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../")
}

// harnessName is a repository's name with the noise a harness repository tends to
// carry taken off — plasticity-harness-rails is the rails harness.
func harnessName(repo string) string {
	name := strings.TrimPrefix(repo, "plasticity-")
	name = strings.TrimPrefix(name, "harness-")
	name = strings.TrimSuffix(name, "-harness")
	if name == "" {
		return repo
	}
	return name
}

// git runs a git command in a directory, with its output on stderr where a
// person can see what happened.
func git(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

// gitLine asks git a question, and answers empty for anything that failed. The
// callers are all "what is true here", where a failure and a blank answer mean
// the same thing.
func gitLine(dir string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitState reports what git knows about a directory, and nothing if it is not a
// repository.
func gitState(dir string) (source, ref, commit string, dirty bool) {
	if gitLine(dir, "rev-parse", "--is-inside-work-tree") != "true" {
		return "", "", "", false
	}
	return gitLine(dir, "remote", "get-url", "origin"),
		gitLine(dir, "rev-parse", "--abbrev-ref", "HEAD"),
		gitLine(dir, "rev-parse", "--short", "HEAD"),
		gitLine(dir, "status", "--porcelain") != ""
}

func harnessReadme(name string) string {
	return "# " + name + `

A plst harness: an interchangeable set of agent configuration.

    plst harness use ` + name + `

## Layout

| path | scope |
|---|---|
| ` + "`CLAUDE.md`" + ` | user or project |
| ` + "`rules/`" + ` | project only — the agent never reads rules from user scope |
| ` + "`agents/`" + ` | user or project |
| ` + "`skills/`" + ` | user or project |
| ` + "`commands/`" + ` | user or project |
| ` + "`hooks/`" + ` | scripts, registered from ` + "`harness.json`" + ` |

Directory components are linked entry by entry, so a repository keeps its own
committed agents and skills and only a same-named entry is displaced. A harness
that should be the whole of what the agent reads says so:

    { "link": "replace" }

## Hooks

Hooks are declared in ` + "`harness.json`" + ` in the same shape the agent's own
settings file uses. Use ` + "`" + RootVar + "`" + ` instead of writing down a path:

    {
      "hooks": {
        "PreToolUse": [
          { "matcher": "Bash",
            "hooks": [{ "type": "command", "command": "` + RootVar + `/hooks/guard.sh" }] }
        ]
      }
    }
`
}
