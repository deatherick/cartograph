package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ConfigFileName is the project-level settings file — committed to the
// repo like tsconfig.json or .eslintrc, unlike ~/.cartograph/ (which holds
// the derived, disposable snapshot; deleting it just means the next index
// rebuilds it). This is where "which languages are active" lives after
// `ctx init`, editable by hand or by re-running init.
const ConfigFileName = ".cartograph.json"

// Config is the persisted, editable set of project-level settings — today
// just which languages are active. Grows in place as new project-level
// settings are added; unknown fields in an existing file are preserved by
// nothing today (a real limitation worth revisiting once a second field
// exists — see docs/MVP.md's known issues).
type Config struct {
	// Languages lists which of registry()'s languages are enabled by
	// name. Empty or absent (the zero value, and what a fresh repo with no
	// config file gets) means "every language this build detects in the
	// repo" — Run's zero-config default, so a brand new user never needs
	// to run `ctx init` first for indexing to work at all; init exists to
	// make that choice explicit and persisted, not to gate basic use.
	Languages []string `json:"languages,omitempty"`
}

// LoadConfig reads <root>/.cartograph.json. ok=false when the file is
// absent or fails to parse — callers proceed with the zero-config default
// (every detected language) rather than failing the whole run over a
// missing or malformed settings file, the same fail-soft convention
// loadTSConfig/loadGoModule already follow for their own config files.
func LoadConfig(root string) (Config, bool) {
	data, err := os.ReadFile(filepath.Join(root, ConfigFileName))
	if err != nil {
		return Config{}, false
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, false
	}
	return cfg, true
}

// SaveConfig writes cfg to <root>/.cartograph.json as indented JSON —
// human-editable by design, since a user or team may want to hand-edit
// the language list after `ctx init` without re-running the wizard.
func SaveConfig(root string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("index: marshaling config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(root, ConfigFileName), data, 0o644); err != nil {
		return fmt.Errorf("index: writing %s: %w", ConfigFileName, err)
	}
	return nil
}

// enabledLanguages resolves the effective language list for a Run against
// root: cfg.Languages if the config file specifies any (an explicit,
// possibly narrower choice), otherwise every language registry(root)
// detects (the zero-config default — see Config.Languages's doc). A name
// in cfg.Languages that registry() doesn't recognize (a stale entry from
// an older build, or a typo) is silently ignored rather than erroring the
// whole run — the same fail-soft convention as a malformed config file.
func enabledLanguages(root string) []Language {
	all := registry(root)
	cfg, ok := LoadConfig(root)
	if !ok || len(cfg.Languages) == 0 {
		var detected []Language
		for _, l := range all {
			if l.Detect(root) {
				detected = append(detected, l)
			}
		}
		return detected
	}
	want := map[string]bool{}
	for _, name := range cfg.Languages {
		want[name] = true
	}
	var enabled []Language
	for _, l := range all {
		if want[l.Name] {
			enabled = append(enabled, l)
		}
	}
	return enabled
}
