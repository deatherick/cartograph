// Package project is a small, global (not per-repo) registry mapping a
// short name to a repo path — `ctx project add/list/remove`, named
// explicitly in the master plan's CLI/UX scope (docs/MVP.md's "no real
// multi-project management" known issue) but never built until now.
//
// Deliberately minimal: this is NOT the daemon-side multi-project registry
// a future `ctxd project add` would need (one process watching/serving
// several projects at once — ADR-0012's documented gap). This is just a
// name → path lookup every CLI command can resolve through, so a user
// types `ctx index myapp` instead of `ctx index ~/code/some/long/path`
// once. `.cartograph.json` (ADR-0011) stays per-project language config;
// this file is the one thing that's genuinely global across all of them.
package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Project is one registered name → path mapping.
type Project struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// registry is the on-disk shape of ~/.cartograph/projects.json.
type registry struct {
	Projects []Project `json:"projects"`
}

// registryPath is always under the user's home directory, alongside every
// other Cartograph-managed state (store.RepoDir's per-repo snapshots) —
// never inside a project directory itself (ADR-0011's "only
// .cartograph.json lives in a project directory" invariant).
func registryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("project: resolving home directory: %w", err)
	}
	return filepath.Join(home, ".cartograph", "projects.json"), nil
}

func load() (registry, error) {
	path, err := registryPath()
	if err != nil {
		return registry{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return registry{}, nil // no registry yet — an empty one, not an error
		}
		return registry{}, fmt.Errorf("project: reading %s: %w", path, err)
	}
	var reg registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return registry{}, fmt.Errorf("project: parsing %s: %w", path, err)
	}
	return reg, nil
}

func save(reg registry) error {
	path, err := registryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("project: creating %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("project: marshaling registry: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("project: writing %s: %w", path, err)
	}
	return nil
}

// Add registers name → path, overwriting any existing entry with the same
// name (re-adding a project points it at a new location rather than
// erroring — the common case when a repo moves). path is resolved to an
// absolute path and validated as an existing directory before it's saved,
// so a typo is caught at `add` time, not the next time the name is used.
func Add(name, path string) error {
	if name == "" {
		return fmt.Errorf("project: name must not be empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("project: resolving %q: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("project: %s: %w", abs, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project: %s is not a directory", abs)
	}

	reg, err := load()
	if err != nil {
		return err
	}
	replaced := false
	for i, p := range reg.Projects {
		if p.Name == name {
			reg.Projects[i].Path = abs
			replaced = true
			break
		}
	}
	if !replaced {
		reg.Projects = append(reg.Projects, Project{Name: name, Path: abs})
	}
	return save(reg)
}

// Remove unregisters name. A no-op (not an error) if name was never
// registered — removing something that isn't there achieves the caller's
// goal either way.
func Remove(name string) error {
	reg, err := load()
	if err != nil {
		return err
	}
	out := reg.Projects[:0]
	for _, p := range reg.Projects {
		if p.Name != name {
			out = append(out, p)
		}
	}
	reg.Projects = out
	return save(reg)
}

// List returns every registered project, sorted by name for stable,
// predictable output.
func List() ([]Project, error) {
	reg, err := load()
	if err != nil {
		return nil, err
	}
	sort.Slice(reg.Projects, func(i, j int) bool { return reg.Projects[i].Name < reg.Projects[j].Name })
	return reg.Projects, nil
}

// Resolve maps nameOrPath through the registry: if it matches a registered
// project's name exactly, its path is returned; otherwise nameOrPath is
// returned UNCHANGED, treated as a literal filesystem path — every
// existing command that took a raw path keeps working exactly as before
// for anyone who never registers a project at all. Errors reading the
// registry are swallowed (falling back to the literal input) rather than
// failing the caller's real command over an optional convenience feature.
func Resolve(nameOrPath string) string {
	reg, err := load()
	if err != nil {
		return nameOrPath
	}
	for _, p := range reg.Projects {
		if p.Name == nameOrPath {
			return p.Path
		}
	}
	return nameOrPath
}
