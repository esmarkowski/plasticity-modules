package harness

import (
	"encoding/json"

	"github.com/esmarkowski/plasticity-modules/internal/claude"
)

// registerHooks writes a harness's hook declarations into a settings file and
// returns the commands it wrote.
//
// Merged, not replaced. The settings file also holds the model, the theme, the
// status line, enabled plugins and permission preferences, and hooks registered
// by hand or by another tool — a harness owns what it declared and nothing else.
// The commands are returned so removing them later is a match on what was
// written rather than a guess at what must have been.
func registerHooks(path string, h Harness) ([]string, error) {
	want := h.Manifest.Expand(h.Dir)
	if len(want) == 0 {
		return nil, nil
	}
	s, err := claude.LoadSettings(path)
	if err != nil {
		return nil, err
	}
	have, err := s.Hooks()
	if err != nil {
		return nil, err
	}

	var wrote []string
	for event, groups := range want {
		for _, g := range groups {
			// An identical registration already present is left alone, so using
			// the same harness twice does not double its hooks.
			cmds := claude.Commands([]claude.HookGroup{g})
			if containsAll(claude.Commands(have[event]), cmds) {
				wrote = append(wrote, cmds...)
				continue
			}
			have[event] = append(have[event], g)
			wrote = append(wrote, cmds...)
		}
	}
	if err := s.SetHooks(have); err != nil {
		return nil, err
	}
	if err := s.Save(); err != nil {
		return nil, err
	}
	return wrote, nil
}

// unregisterHooks removes exactly the commands a harness registered.
//
// A group is dropped when nothing is left in it, and an event when nothing is
// left under it, so turning a harness off returns the file to the shape it had
// rather than leaving a scattering of empty arrays behind.
func unregisterHooks(path string, commands []string) error {
	if path == "" || len(commands) == 0 {
		return nil
	}
	s, err := claude.LoadSettings(path)
	if err != nil {
		return err
	}
	have, err := s.Hooks()
	if err != nil {
		return err
	}
	drop := map[string]bool{}
	for _, c := range commands {
		drop[c] = true
	}

	changed := false
	for event, groups := range have {
		kept := make([]claude.HookGroup, 0, len(groups))
		for _, g := range groups {
			hooks := make([]json.RawMessage, 0, len(g.Hooks))
			for _, h := range g.Hooks {
				var cmd struct {
					Command string `json:"command"`
				}
				if json.Unmarshal(h, &cmd) == nil && drop[cmd.Command] {
					changed = true
					continue
				}
				hooks = append(hooks, h)
			}
			if len(hooks) == 0 {
				continue
			}
			g.Hooks = hooks
			kept = append(kept, g)
		}
		if len(kept) == 0 {
			delete(have, event)
			continue
		}
		have[event] = kept
	}
	if !changed {
		return nil
	}
	if err := s.SetHooks(have); err != nil {
		return err
	}
	return s.Save()
}

func containsAll(haystack, needles []string) bool {
	if len(needles) == 0 {
		return false
	}
	have := map[string]bool{}
	for _, h := range haystack {
		have[h] = true
	}
	for _, n := range needles {
		if !have[n] {
			return false
		}
	}
	return true
}
