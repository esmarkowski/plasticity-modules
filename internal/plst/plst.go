// Package plst reads the environment plst hands a module.
//
// A module is told where things are rather than working it out again: plst owns
// that decision, and a module that derives its own paths is a second
// implementation of config that will disagree with the first the moment either
// changes. The fallbacks exist only so a module still runs when invoked directly,
// which is how it gets developed.
package plst

import (
	"os"
	"path/filepath"
)

const (
	EnvHome      = "PLST_HOME"
	EnvModuleDir = "PLST_MODULE_DIR"
	EnvConfig    = "PLST_CONFIG"
	EnvTools     = "PLST_TOOLS"
	EnvBin       = "PLST_BIN"
)

// Home is the root of plst's state, resolved the same way plst resolves it.
func Home() string {
	if h := os.Getenv(EnvHome); h != "" {
		return h
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "plasticity")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".plasticity"
	}
	return filepath.Join(home, ".plasticity")
}

// Dir is a subdirectory of plst's state, created on demand.
func Dir(name string) (string, error) {
	d := filepath.Join(Home(), name)
	return d, os.MkdirAll(d, 0o755)
}
