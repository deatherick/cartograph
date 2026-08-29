package index

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/deatherick/cartograph/internal/resolve"
)

// tsconfigFile mirrors just the subset of tsconfig.json's shape this
// project uses. Real tsconfig files can extend another config
// (`"extends": "./base.json"`) and can be JSONC (comments, trailing
// commas) — neither is handled here; both are documented gaps, not
// silently-wrong behavior (a config that fails to parse is skipped, not
// guessed at).
type tsconfigFile struct {
	CompilerOptions struct {
		BaseURL string              `json:"baseUrl"`
		Paths   map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
}

// loadTSConfig reads <root>/tsconfig.json, if present, and returns the
// resolver's TSConfig view of it. ok=false when the file doesn't exist or
// fails to parse — the caller proceeds without path-alias resolution
// rather than failing the whole index run over a malformed config.
func loadTSConfig(root string) (resolve.TSConfig, bool) {
	data, err := os.ReadFile(filepath.Join(root, "tsconfig.json"))
	if err != nil {
		return resolve.TSConfig{}, false
	}
	var tc tsconfigFile
	if err := json.Unmarshal(data, &tc); err != nil {
		return resolve.TSConfig{}, false
	}
	if tc.CompilerOptions.BaseURL == "" && len(tc.CompilerOptions.Paths) == 0 {
		return resolve.TSConfig{}, false
	}
	return resolve.TSConfig{BaseURL: tc.CompilerOptions.BaseURL, Paths: tc.CompilerOptions.Paths}, true
}
