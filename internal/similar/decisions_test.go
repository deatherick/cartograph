package similar

import (
	"path/filepath"
	"testing"

	"github.com/deatherick/cartograph/internal/model"
)

func TestParseDecision_ValidAndInvalid(t *testing.T) {
	for _, d := range []Decision{DecisionIgnore, DecisionIntentional, DecisionSamePattern, DecisionShouldShareAbstraction, DecisionFalsePositive} {
		if got, ok := ParseDecision(string(d)); !ok || got != d {
			t.Errorf("ParseDecision(%q) = (%q, %v), want (%q, true)", d, got, ok, d)
		}
	}
	if _, ok := ParseDecision("not-a-real-decision"); ok {
		t.Error("expected an unrecognized decision string to be rejected")
	}
}

func TestDecisions_LoadMissingFile_ReturnsEmptyNotError(t *testing.T) {
	d, err := LoadDecisions(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("expected a missing decisions file to be a normal empty state, got error: %v", err)
	}
	if len(d.ByPair) != 0 {
		t.Errorf("expected an empty ByPair, got %+v", d.ByPair)
	}
}

func TestDecisions_SaveThenLoad_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.json")
	d := &Decisions{ByPair: map[string]Decision{}}
	d.Decide("aaa", "bbb", DecisionIntentional)
	if err := d.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadDecisions(path)
	if err != nil {
		t.Fatalf("LoadDecisions: %v", err)
	}
	if dec, ok := got.Get("aaa", "bbb"); !ok || dec != DecisionIntentional {
		t.Errorf("Get(aaa,bbb) = (%q, %v), want (%q, true)", dec, ok, DecisionIntentional)
	}
	// Order-independence must survive the round trip too.
	if dec, ok := got.Get("bbb", "aaa"); !ok || dec != DecisionIntentional {
		t.Errorf("Get(bbb,aaa) = (%v, %v), want (%q, true) — Key() must be order-independent", dec, ok, DecisionIntentional)
	}
}

func TestDecisions_Decide_OverwritesPriorDecision(t *testing.T) {
	d := &Decisions{ByPair: map[string]Decision{}}
	d.Decide("aaa", "bbb", DecisionFalsePositive)
	d.Decide("bbb", "aaa", DecisionSamePattern) // same pair, opposite argument order
	dec, ok := d.Get("aaa", "bbb")
	if !ok || dec != DecisionSamePattern {
		t.Errorf("expected the second Decide call to overwrite the first, got (%v, %v)", dec, ok)
	}
	if len(d.ByPair) != 1 {
		t.Errorf("expected exactly one stored decision (order-independent key), got %d", len(d.ByPair))
	}
}

func TestDecisions_Filter_RemovesDecidedPairs(t *testing.T) {
	d := &Decisions{ByPair: map[string]Decision{}}
	decided := Pair{A: model.EntityID("aaa"), B: model.EntityID("bbb"), Overall: 0.9}
	undecided := Pair{A: model.EntityID("ccc"), B: model.EntityID("ddd"), Overall: 0.8}
	d.Decide(decided.A, decided.B, DecisionIgnore)

	got := d.Filter([]Pair{decided, undecided})
	if len(got) != 1 || got[0].Key() != undecided.Key() {
		t.Fatalf("expected only the undecided pair to remain, got %+v", got)
	}
}
