package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/esmarkowski/plasticity-modules/internal/claude"
	"github.com/esmarkowski/plasticity-modules/internal/plst"
)

// scratch points plst's state and the agent's configuration directory at
// temporary directories, so a test can apply harnesses without touching either
// real one.
func scratch(t *testing.T) (home, agent string) {
	t.Helper()
	home, agent = filepath.Join(t.TempDir(), "plst"), filepath.Join(t.TempDir(), "agent")
	t.Setenv(plst.EnvHome, home)
	t.Setenv("CLAUDE_CONFIG_DIR", agent)
	if err := os.MkdirAll(agent, 0o755); err != nil {
		t.Fatal(err)
	}
	return home, agent
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// build a harness on disk with the components named.
func makeHarness(t *testing.T, name string, hooks string, components ...string) Harness {
	t.Helper()
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, name)
	for _, c := range components {
		if c == "CLAUDE.md" {
			writeFile(t, filepath.Join(dir, c), "# "+name+"\n")
			continue
		}
		writeFile(t, filepath.Join(dir, c, "item.md"), "item\n")
	}
	manifest := `{"name":"` + name + `"`
	if hooks != "" {
		manifest += `,"hooks":` + hooks
	}
	manifest += "}"
	writeFile(t, filepath.Join(dir, ManifestFile), manifest)
	h, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// A harness is a repository that may be cloned anywhere, so a hook command that
// names a path is a hook command that works on one machine.
func TestExpandSubstitutesTheRoot(t *testing.T) {
	m := Manifest{Hooks: map[string][]claude.HookGroup{
		"PreToolUse": {{Matcher: "Bash", Hooks: []json.RawMessage{
			json.RawMessage(`{"type":"command","command":"` + RootVar + `/hooks/guard.sh","timeout":5}`),
		}}},
	}}
	got := m.Expand("/opt/h")
	cmds := claude.Commands(got["PreToolUse"])
	if len(cmds) != 1 || cmds[0] != "/opt/h/hooks/guard.sh" {
		t.Fatalf("commands = %v", cmds)
	}
	// Fields this program has never heard of survive the round trip.
	if !strings.Contains(string(got["PreToolUse"][0].Hooks[0]), `"timeout":5`) {
		t.Errorf("an unknown field was dropped: %s", got["PreToolUse"][0].Hooks[0])
	}
	// The manifest itself is untouched, so a harness used once does not come back
	// with a machine's paths baked into it.
	if !strings.Contains(string(m.Hooks["PreToolUse"][0].Hooks[0]), RootVar) {
		t.Error("Expand mutated the manifest it was given")
	}
}

// What was in the agent's directory before plst arrived is not plst's to throw
// away.
func TestUseParksWhatItDisplacesAndOffPutsItBack(t *testing.T) {
	_, agent := scratch(t)
	writeFile(t, filepath.Join(agent, "CLAUDE.md"), "ORIGINAL\n")
	writeFile(t, filepath.Join(agent, "agents", "mine.md"), "MY AGENT\n")
	makeHarness(t, "rails", "", "CLAUDE.md", "agents")

	rep, err := Use("rails", User, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Parked) != 2 {
		t.Errorf("parked %v, want both pre-existing components", rep.Parked)
	}
	// The link is live and resolves to the harness.
	if b, _ := os.ReadFile(filepath.Join(agent, "CLAUDE.md")); string(b) != "# rails\n" {
		t.Errorf("CLAUDE.md reads %q", b)
	}
	if fi, err := os.Lstat(filepath.Join(agent, "agents")); err != nil ||
		fi.Mode()&os.ModeSymlink == 0 {
		t.Error("agents is not a symlink")
	}

	if _, err := Off(User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "CLAUDE.md")); string(b) != "ORIGINAL\n" {
		t.Errorf("after off, CLAUDE.md reads %q, want the original back", b)
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "agents", "mine.md")); string(b) != "MY AGENT\n" {
		t.Errorf("after off, the original agent is %q", b)
	}
	if fi, _ := os.Lstat(filepath.Join(agent, "agents")); fi != nil && fi.Mode()&os.ModeSymlink != 0 {
		t.Error("a symlink was left behind")
	}
	if _, ok := LoadState().Active(User); ok {
		t.Error("state still reports a harness applied")
	}
}

// The settings file holds the model, the theme, the status line and hooks
// registered by hand. A harness owns what it declared and nothing else.
func TestHooksAreMergedAndRemovedExactly(t *testing.T) {
	_, agent := scratch(t)
	writeFile(t, filepath.Join(agent, "settings.json"), `{
  "model": "opus",
  "theme": "dark",
  "hooks": { "UserPromptSubmit": [ { "hooks": [ { "type": "command", "command": "/mine/emit" } ] } ] }
}`)
	makeHarness(t, "rails", `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"`+RootVar+`/hooks/g.sh"}]}]}`, "hooks")

	if _, err := Use("rails", User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	s, err := claude.LoadSettings(claude.SettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	hooks, _ := s.Hooks()
	if len(claude.Commands(hooks["PreToolUse"])) != 1 {
		t.Errorf("the harness hook was not registered: %v", hooks)
	}
	if cmds := claude.Commands(hooks["UserPromptSubmit"]); len(cmds) != 1 || cmds[0] != "/mine/emit" {
		t.Errorf("a hand-registered hook was disturbed: %v", cmds)
	}

	if _, err := Off(User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	s, _ = claude.LoadSettings(claude.SettingsPath())
	hooks, _ = s.Hooks()
	if _, ok := hooks["PreToolUse"]; ok {
		t.Error("the event was left behind with nothing under it")
	}
	if cmds := claude.Commands(hooks["UserPromptSubmit"]); len(cmds) != 1 {
		t.Errorf("the hand-registered hook did not survive removal: %v", cmds)
	}
	// Everything that is not hooks is passed through untouched.
	raw, err := os.ReadFile(claude.SettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	if all["model"] != "opus" || all["theme"] != "dark" {
		t.Errorf("other settings were lost: %v", all)
	}
}

// Swapping has to take the previous harness out, or its hooks stay registered.
func TestUseReplacesThePreviousHarness(t *testing.T) {
	_, agent := scratch(t)
	makeHarness(t, "one", `{"Stop":[{"hooks":[{"type":"command","command":"`+RootVar+`/hooks/a.sh"}]}]}`, "CLAUDE.md", "hooks")
	makeHarness(t, "two", `{"Stop":[{"hooks":[{"type":"command","command":"`+RootVar+`/hooks/b.sh"}]}]}`, "CLAUDE.md", "hooks")

	if _, err := Use("one", User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	rep, err := Use("two", User, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Replaced != "one" {
		t.Errorf("replaced %q, want one", rep.Replaced)
	}
	s, _ := claude.LoadSettings(claude.SettingsPath())
	hooks, _ := s.Hooks()
	cmds := claude.Commands(hooks["Stop"])
	if len(cmds) != 1 || !strings.Contains(cmds[0], "/two/hooks/b.sh") {
		t.Errorf("Stop hooks = %v, want only the new harness's", cmds)
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "CLAUDE.md")); string(b) != "# two\n" {
		t.Errorf("CLAUDE.md reads %q", b)
	}
}

// Applying the same harness twice must not double its hooks.
func TestUseIsIdempotent(t *testing.T) {
	scratch(t)
	makeHarness(t, "rails", `{"Stop":[{"hooks":[{"type":"command","command":"`+RootVar+`/hooks/a.sh"}]}]}`, "hooks")
	for range 3 {
		if _, err := Use("rails", User, func(string) {}); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := claude.LoadSettings(claude.SettingsPath())
	hooks, _ := s.Hooks()
	if got := claude.Commands(hooks["Stop"]); len(got) != 1 {
		t.Errorf("registered %d copies of one hook: %v", len(got), got)
	}
}

// Rules are only ever read from a project. Linking one at user scope would look
// correct and never be read, so it is reported instead.
func TestRulesAreSkippedAtUserScope(t *testing.T) {
	_, agent := scratch(t)
	makeHarness(t, "rails", "", "CLAUDE.md", "rules")

	rep, err := Use("rails", User, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0] != "rules" {
		t.Errorf("skipped = %v, want rules", rep.Skipped)
	}
	if _, err := os.Lstat(filepath.Join(agent, "rules")); err == nil {
		t.Error("rules were linked at user scope")
	}

	// At project scope they are linked, because that is where they work.
	repo := t.TempDir()
	rep, err = Use("rails", ProjectScope(repo), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Skipped) != 0 {
		t.Errorf("skipped %v at project scope", rep.Skipped)
	}
	if fi, err := os.Lstat(filepath.Join(repo, ".claude", "rules")); err != nil ||
		fi.Mode()&os.ModeSymlink == 0 {
		t.Error("rules were not linked at project scope")
	}
}

// The two scopes are independent: a project runs one harness while everything
// else runs another.
func TestScopesAreTrackedSeparately(t *testing.T) {
	scratch(t)
	makeHarness(t, "one", "", "CLAUDE.md")
	makeHarness(t, "two", "", "CLAUDE.md")
	repo := t.TempDir()

	if _, err := Use("one", User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := Use("two", ProjectScope(repo), func(string) {}); err != nil {
		t.Fatal(err)
	}
	state := LoadState()
	if a, _ := state.Active(User); a.Harness != "one" {
		t.Errorf("user scope has %q", a.Harness)
	}
	if a, _ := state.Active(ProjectScope(repo)); a.Harness != "two" {
		t.Errorf("project scope has %q", a.Harness)
	}
	// User scope is reported first: it is the one that applies everywhere.
	if order := state.Order(); len(order) != 2 || order[0] != User {
		t.Errorf("order = %v", order)
	}
}

func TestParseSource(t *testing.T) {
	cases := map[string][3]string{
		"esmarkowski/rails-harness":                 {"https://github.com/esmarkowski/rails-harness.git", "rails", ""},
		"https://github.com/o/plasticity-harness-x": {"https://github.com/o/plasticity-harness-x.git", "x", ""},
		"git@github.com:o/r.git":                    {"https://github.com/o/r.git", "r", ""},
		"o/r@v2":                                    {"https://github.com/o/r.git", "r", "v2"},
	}
	for in, want := range cases {
		url, name, ref, err := parseSource(in)
		if err != nil {
			t.Errorf("parseSource(%q): %v", in, err)
			continue
		}
		if url != want[0] || name != want[1] || ref != want[2] {
			t.Errorf("parseSource(%q) = %q %q %q, want %q", in, url, name, ref, want)
		}
	}
	for _, bad := range []string{"", "r", "a/b/c"} {
		if _, _, _, err := parseSource(bad); err == nil {
			t.Errorf("parseSource(%q) was accepted", bad)
		}
	}
}
