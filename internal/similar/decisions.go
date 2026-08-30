package similar

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/deatherick/cartograph/internal/model"
)

// Decision is a human's disposition on one candidate pair — persisted so
// a decided pair never resurfaces as "new" in a later Find/`ctx
// duplicates` run. Never inferred or auto-applied by this package; every
// value here requires an explicit Decide call. Matches the master plan's
// own named taxonomy exactly.
type Decision string

const (
	DecisionIgnore                 Decision = "ignore"
	DecisionIntentional            Decision = "intentional"
	DecisionSamePattern            Decision = "same-pattern"
	DecisionShouldShareAbstraction Decision = "should-share-abstraction"
	DecisionFalsePositive          Decision = "false-positive"
)

// ParseDecision validates a user-supplied string against the known
// Decision values — CLI/MCP input never trusts a bare cast.
func ParseDecision(s string) (Decision, bool) {
	switch Decision(s) {
	case DecisionIgnore, DecisionIntentional, DecisionSamePattern, DecisionShouldShareAbstraction, DecisionFalsePositive:
		return Decision(s), true
	}
	return "", false
}

// Decisions is the on-disk shape: pair key (Pair.Key()) -> Decision.
type Decisions struct {
	ByPair map[string]Decision `json:"by_pair"`
}

// DecisionsPath derives the on-disk location for a repo's duplicate
// decisions — namespaced under the same per-repo directory
// internal/store (snapshots) and internal/ledger (sessions) already use.
func DecisionsPath(repoDir string) string {
	return filepath.Join(repoDir, "duplicate-decisions.json")
}

// LoadDecisions reads a repo's decisions, returning an empty (not an
// error) Decisions when the file doesn't exist yet — no decisions made
// yet is a normal state, not a failure.
func LoadDecisions(path string) (*Decisions, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Decisions{ByPair: map[string]Decision{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var d Decisions
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	if d.ByPair == nil {
		d.ByPair = map[string]Decision{}
	}
	return &d, nil
}

// Save persists d, creating its directory if needed.
func (d *Decisions) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// Decide records decision for the (order-independent) pair (a, b),
// overwriting any prior decision for that same pair.
func (d *Decisions) Decide(a, b model.EntityID, decision Decision) {
	d.ByPair[pairKey(a, b)] = decision
}

// Get returns the recorded decision for (a, b), if any.
func (d *Decisions) Get(a, b model.EntityID) (Decision, bool) {
	dec, ok := d.ByPair[pairKey(a, b)]
	return dec, ok
}

// Filter removes every pair that already has a recorded decision —
// what `ctx duplicates`/`ctx similar` show is always undecided pairs
// only, so a decided pair never resurfaces as "new".
func (d *Decisions) Filter(pairs []Pair) []Pair {
	out := make([]Pair, 0, len(pairs))
	for _, p := range pairs {
		if _, decided := d.ByPair[p.Key()]; !decided {
			out = append(out, p)
		}
	}
	return out
}

// String renders a Decision list for CLI usage/error text.
func ValidDecisions() string {
	return fmt.Sprintf("%s, %s, %s, %s, %s",
		DecisionIgnore, DecisionIntentional, DecisionSamePattern, DecisionShouldShareAbstraction, DecisionFalsePositive)
}
