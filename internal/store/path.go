package store

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// SnapshotPath derives a stable snapshot location for a repo at root.
// PROVISIONAL: real multi-project management (`ctx project add`, per the
// master plan) doesn't exist yet, so this is a single-machine, per-path
// convention: ~/.cartograph/<repo>-<hash8ofAbsPath>/graph.bin. The path
// hash exists so two different repos that happen to share a directory
// name (both called "app", say) don't silently collide — a cheap
// robustness win worth having even before real project identity exists.
func SnapshotPath(root, repo string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(abs))
	suffix := hex.EncodeToString(h[:])[:8]

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cartograph", repo+"-"+suffix, "graph.bin"), nil
}
