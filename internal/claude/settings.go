package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Settings is the agent's settings file, held as raw JSON.
//
// Ordered by nothing and understood barely: every key but hooks is passed through
// untouched. The file holds the model, the theme, the status line, enabled
// plugins, permission preferences — none of which a harness has any business
// rewriting, and all of which would be lost by round-tripping through a struct
// that only knows about hooks.
type Settings struct {
	raw  map[string]json.RawMessage
	path string
}

// LoadSettings reads a settings file. A missing file is an empty one, since
// registering the first hook on a fresh machine has to work.
func LoadSettings(path string) (*Settings, error) {
	s := &Settings{raw: map[string]json.RawMessage{}, path: path}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// HookGroup is one registration: a matcher, and the commands it runs.
//
// Kept as raw JSON below the group level so a field this program has never heard
// of — a timeout, a status message, whatever is added next — survives being read
// and written back.
type HookGroup struct {
	Matcher string            `json:"matcher,omitempty"`
	Hooks   []json.RawMessage `json:"hooks"`
}

// Hooks reads the hook registrations, keyed by event.
func (s *Settings) Hooks() (map[string][]HookGroup, error) {
	out := map[string][]HookGroup{}
	raw, ok := s.raw["hooks"]
	if !ok {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: hooks: %w", s.path, err)
	}
	return out, nil
}

// SetHooks replaces the hook registrations, leaving every other key alone.
func (s *Settings) SetHooks(h map[string][]HookGroup) error {
	// An empty map is written as no hooks key at all rather than as {}, so
	// turning a harness off leaves the file as it would have been.
	if len(h) == 0 {
		delete(s.raw, "hooks")
		return nil
	}
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	s.raw["hooks"] = b
	return nil
}

// Commands lists every command string registered under an event, which is how a
// registration is recognised again later without depending on its position.
func Commands(groups []HookGroup) []string {
	var out []string
	for _, g := range groups {
		for _, h := range g.Hooks {
			var cmd struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(h, &cmd) == nil && cmd.Command != "" {
				out = append(out, cmd.Command)
			}
		}
	}
	return out
}

// Save writes the settings back, after copying the previous file aside.
//
// Written to a temporary file in the same directory and renamed, so a settings
// file is never half-written — the agent may read it at any moment, and a
// truncated one is a session that will not start.
func (s *Settings) Save() error {
	if err := s.backup(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".plst-new"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// backup keeps the previous settings in the agent's own backups directory, which
// it already maintains, so a bad edit is one copy from undone.
func (s *Settings) backup() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		// Nothing to back up on a first write.
		return nil
	}
	dir := filepath.Join(Dir(), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("settings-%s.json", time.Now().UTC().Format("20060102-150405"))
	return os.WriteFile(filepath.Join(dir, name), b, 0o644)
}

// Path is where these settings came from.
func (s *Settings) Path() string { return s.path }
