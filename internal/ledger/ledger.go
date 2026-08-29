// Package ledger implements the Context Ledger — per-session state
// tracking what evidence has already been delivered to an agent, so a
// second call in the same session doesn't re-spend tokens re-sending it.
// This is the single largest source of savings in a multi-call session
// (see the project plan's "Context Ledger" idea) and, unlike the rest of
// the pipeline, has no equivalent in Grafel's design at all.
//
// V0 persists to a plain JSON file rather than SQLite — the project has
// deliberately not introduced a SQLite dependency yet (docs/adr/0006-phase1-completion-and-search-scope.md),
// and a session's state is small (a handle map plus a per-entity level),
// well within what a JSON file handles fine at this scale. Moving this
// into SQLite is a natural, contained change whenever SQLite is
// introduced for its already-scoped purposes.
package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/deatherick/cartograph/internal/model"
)

// Level is the source-ladder rung an entity has been delivered at —
// mirrors internal/compile.Level without importing it, so ledger has no
// dependency on the compiler (the compiler depends on the ledger, not the
// reverse).
type Level int

const (
	LevelNone Level = iota
	LevelName
	LevelSignature
	LevelSkeleton
	LevelBody
	LevelFile
)

// State is one session's memory: for every entity ever sent, the handle
// it was assigned and the highest level it was delivered at. Handles are
// stable for the life of the session so an agent can refer back to "E7"
// across calls.
type State struct {
	NextHandle int                         `json:"next_handle"`
	Handles    map[model.EntityID]string   `json:"handles"`
	Delivered  map[model.EntityID]Level    `json:"delivered"`
}

// New creates an empty session state.
func New() *State {
	return &State{NextHandle: 1, Handles: map[model.EntityID]string{}, Delivered: map[model.EntityID]Level{}}
}

// HandleFor returns id's stable handle, assigning a new one (E1, E2, ...)
// on first use.
func (s *State) HandleFor(id model.EntityID) string {
	if h, ok := s.Handles[id]; ok {
		return h
	}
	h := "E" + itoa(s.NextHandle)
	s.NextHandle++
	s.Handles[id] = h
	return h
}

// DeliveredAt returns the highest level id has been sent at this session,
// or LevelNone if never sent.
func (s *State) DeliveredAt(id model.EntityID) Level {
	return s.Delivered[id]
}

// MarkDelivered records that id was just sent at level (only upgrades —
// recording a lower level than already delivered is a no-op, since a
// caller that already has the body doesn't need to be told it now only
// has the signature).
func (s *State) MarkDelivered(id model.EntityID, level Level) {
	if level > s.Delivered[id] {
		s.Delivered[id] = level
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// Path derives the on-disk location for a session's ledger file,
// namespaced under the same per-repo directory internal/store uses for
// snapshots (~/.cartograph/<repo>-<hash>/sessions/<id>.json) — see
// internal/store.SnapshotPath for the naming convention this mirrors.
func Path(repoDir, sessionID string) string {
	return filepath.Join(repoDir, "sessions", sessionID+".json")
}

// Load reads a session's ledger, returning a fresh empty State (not an
// error) when the file doesn't exist yet — a session's first call has no
// history, which is a normal state, not a failure.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Handles == nil {
		s.Handles = map[model.EntityID]string{}
	}
	if s.Delivered == nil {
		s.Delivered = map[model.EntityID]Level{}
	}
	return &s, nil
}

// Save persists the session state, creating the sessions/ directory if
// needed. Not atomic (unlike internal/store's snapshot writes) — a
// session ledger is advisory/best-effort state, not a correctness-critical
// artifact; losing a torn write just means a future call resends
// something it didn't strictly need to, not a wrong answer.
func Save(path string, s *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
