// Package similar implements the Duplicate/Similarity Engine (Phase 5),
// designed as a FUNNEL, never all-pairs comparison — O(N^2) is infeasible
// past a few thousand entities, per the master plan's own explicit
// framing:
//
//	all entities (N)
//	  -> L1  exact fingerprint (Anchor.ContentHash)                O(N)
//	  -> MinHash+LSH candidate generation over token shingles       O(N)
//	  -> scored candidates: structural (MinHash-estimated Jaccard)
//	     + behavioral (Jaccard of CALLS/USES edge targets)
//
// Every score is reported fully decomposed (Pair's Exact/Structural/
// Behavioral/Overall fields) — never a single opaque number — and nothing
// here is ever prescriptive: this reports evidence; a human decides
// (Decision/Decisions, decisions.go). A decided pair never resurfaces.
//
// V0 SCOPE, stated plainly (see docs/adr/0021-similarity-duplicate-engine.md):
//   - Scoped to Function/Method entities only — "duplicate code" is
//     primarily a function-body question; comparing two Interfaces or
//     Classes structurally as "duplicates" is a different, unattempted
//     problem.
//   - Structural similarity DOES normalize renamed identifiers now
//     (tokenize.go's normalizeIdentifiers, ADR-0025's follow-up to
//     ADR-0021's originally-scoped-out gap) — every identifier-looking
//     token that isn't a shared keyword (structuralKeywords) is replaced
//     with a placeholder numbered by first appearance within one
//     entity's own token stream, the standard "blind renaming" clone-
//     detection technique. Still not real declaration/reference-aware
//     (a heuristic tokenizer, not a parser, can't tell a declared name
//     from a referenced one) and still not scoped to per-language
//     keyword sets (one shared list across TS/Go/C#/Python).
//   - No L2 bounded AST tree-edit-distance — the master plan's funnel
//     diagram names this as a distinct rung above MinHash+LSH; token-
//     shingle Jaccard stands in as the structural signal instead. Real
//     tree-edit distance is a legitimate, more precise future upgrade.
//   - No L4 semantic/embedding similarity — explicitly Phase 8 scope in
//     the master plan, unrelated to this package.
//   - The evaluation set docs/similarity-eval/ measures against is
//     honestly smaller than the master plan's ≥120-pair target (see its
//     own doc for the exact count and measured precision/recall) — not
//     silently claimed as meeting that target.
package similar

import (
	"path/filepath"
	"sort"

	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/srcread"
	"github.com/deatherick/cartograph/internal/store"
)

const (
	// minBodyTokens filters out trivial entities (one-line getters,
	// forwarding wrappers) whose near-identical token streams would
	// otherwise flood results with true-but-uninteresting matches — a
	// common practical filter in real clone-detection tools, not specific
	// to this implementation. Raised from 12 to 15 (ADR-0025's identifier-
	// normalization follow-up to ADR-0021): two 12-token trivial functions
	// differing only by name and one type-annotation identifier (this
	// package's own eval fixture's getX/getY) collapse to fully IDENTICAL
	// normalized token streams once identifiers are normalized, which 12
	// was too low a floor to filter — measured, not assumed, see ADR-0025.
	minBodyTokens = 15

	// candidateJaccardFloor is the MinHash-estimated Jaccard a candidate
	// pair must clear before its (more meaningful, still cheap) Overall
	// score is even computed — LSH's own bucketing already filters
	// heavily; this is a second, cheap floor ahead of behavioral scoring.
	candidateJaccardFloor = 0.3

	// structuralWeight/behavioralWeight combine into Overall. First
	// values tested that separated docs/similarity-eval's labeled
	// true/false pairs at a workable threshold — the same "first value
	// that clears the bar" methodology this project has used for every
	// other tuned constant (ADR-0007's relevanceFloorRatio, the IDF
	// seeding damping factor), not tuned by intuition. See
	// docs/similarity-eval/README.md for the actual measurement.
	structuralWeight = 0.6
	behavioralWeight = 0.4

	// DefaultThreshold is what `ctx duplicates`/context_duplicates use
	// when no --threshold is given — see docs/similarity-eval/README.md
	// for the measurement behind this exact number.
	DefaultThreshold = 0.6
)

// Pair is one candidate duplicate/similarity pair with a fully decomposed
// score — every field below is always populated; nothing here is ever
// collapsed into a single number without the components that produced it
// staying visible alongside it.
type Pair struct {
	A, B       model.EntityID
	Exact      bool    // byte-identical bodies (same Anchor.ContentHash)
	Structural float64 // 0..1, MinHash-estimated Jaccard of normalized token shingles
	Behavioral float64 // 0..1, Jaccard of each entity's outgoing (CALLS/USES/...) edge targets
	Overall    float64 // structuralWeight*Structural + behavioralWeight*Behavioral (1.0 if Exact)
}

// Key returns a stable, order-independent identifier for the pair (A,B) —
// used as Decisions' map key, so recording a decision for (A,B) also
// covers a later (B,A) listing of the same pair.
func (p Pair) Key() string { return pairKey(p.A, p.B) }

func pairKey(a, b model.EntityID) string {
	as, bs := string(a), string(b)
	if as > bs {
		as, bs = bs, as
	}
	return as + "_" + bs
}

type fingerprint struct {
	entity     model.Entity
	sig        signature
	behavioral map[string]struct{}
}

// Find computes similarity/duplicate pairs among every Function/Method
// entity snap knows about, reading each one's source from root (via its
// Anchor) to build a structural fingerprint, and snap's own resolved
// edges for a behavioral one. Returns every pair whose Overall score is
// >= minOverall (exact matches always included, regardless of
// minOverall), sorted by Overall descending. A source file that can no
// longer be read (moved/deleted since the last index) is skipped for that
// one entity, not a fatal error for the whole run.
func Find(snap *store.Snapshot, root string, minOverall float64) ([]Pair, error) {
	all := snap.All()
	fps := make([]fingerprint, 0, len(all))
	byContentHash := map[string][]model.EntityID{}

	for _, e := range all {
		if e.Kind != model.KindFunction && e.Kind != model.KindMethod {
			continue
		}
		src, err := srcread.Lines(filepath.Join(root, filepath.FromSlash(e.Anchor.File)), e.Anchor.StartLine, e.Anchor.EndLine)
		if err != nil {
			continue
		}
		tokens := tokenize(src)
		if len(tokens) < minBodyTokens {
			continue
		}
		shingles := shingleHashes(normalizeIdentifiers(tokens))
		fps = append(fps, fingerprint{
			entity:     e,
			sig:        minHashSignature(shingles),
			behavioral: behavioralFingerprint(snap, e.ID),
		})
		byContentHash[e.Anchor.ContentHash] = append(byContentHash[e.Anchor.ContentHash], e.ID)
	}

	seen := map[string]bool{}
	var pairs []Pair

	// L1: exact fingerprint — every pair sharing a ContentHash bypasses
	// MinHash/LSH entirely; there is nothing more to estimate.
	for _, ids := range byContentHash {
		if len(ids) < 2 {
			continue
		}
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				p := Pair{A: ids[i], B: ids[j], Exact: true, Structural: 1, Behavioral: 1, Overall: 1}
				pairs = append(pairs, p)
				seen[p.Key()] = true
			}
		}
	}

	// LSH candidate generation: bucket every fingerprint by each of its
	// signature's bands; anything sharing a bucket anywhere is a
	// candidate worth scoring exactly.
	buckets := map[uint64][]int{}
	for i, fp := range fps {
		for _, bk := range lshBuckets(fp.sig) {
			buckets[bk] = append(buckets[bk], i)
		}
	}
	candidateIdx := map[[2]int]bool{}
	for _, idxs := range buckets {
		for i := 0; i < len(idxs); i++ {
			for j := i + 1; j < len(idxs); j++ {
				a, b := idxs[i], idxs[j]
				if a > b {
					a, b = b, a
				}
				candidateIdx[[2]int{a, b}] = true
			}
		}
	}

	for idx := range candidateIdx {
		fa, fb := fps[idx[0]], fps[idx[1]]
		key := pairKey(fa.entity.ID, fb.entity.ID)
		if seen[key] {
			continue // already reported as an exact match
		}
		structural := estimatedJaccard(fa.sig, fb.sig)
		if structural < candidateJaccardFloor {
			continue
		}
		behavioral := jaccard(fa.behavioral, fb.behavioral)
		overall := combinedScore(structural, behavioral, len(fa.behavioral) == 0 && len(fb.behavioral) == 0)
		if overall < minOverall {
			continue
		}
		pairs = append(pairs, Pair{
			A: fa.entity.ID, B: fb.entity.ID,
			Structural: structural, Behavioral: behavioral, Overall: overall,
		})
		seen[key] = true
	}

	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Overall > pairs[j].Overall })
	return pairs, nil
}

// behavioralFingerprint is the set of (EdgeKind, target-bare-name) pairs
// an entity's outgoing edges name — "what it calls/uses", independent of
// which exact EntityID the target resolved to (so two different-but-
// same-named helpers, or a helper renamed after these two entities were
// written, don't spuriously break an otherwise-real behavioral match). An
// edge whose target isn't in snap (a residual/unresolved reference) falls
// back to the raw EntityID string — still a valid, comparable token, just
// less readable.
func behavioralFingerprint(snap *store.Snapshot, id model.EntityID) map[string]struct{} {
	out := map[string]struct{}{}
	for _, edge := range snap.FanOut(id) {
		name := string(edge.Dst)
		if target, ok := snap.Lookup(edge.Dst); ok {
			name = target.Name
		}
		out[string(edge.Kind)+":"+name] = struct{}{}
	}
	return out
}

// combinedScore is Overall's formula: a weighted blend of structural and
// behavioral, UNLESS neither entity has any behavioral signal at all (no
// resolved outgoing edges — a pure, self-contained function, or one whose
// only calls are builtins/externals that never produce a graph edge, see
// behavioralFingerprint's doc), in which case behavioral is genuinely
// absent evidence, not "0% match" — scoring on structural alone avoids
// penalizing exactly the kind of small, self-contained function real
// duplicate-detection cares about most. Found via this package's own
// eval fixture (docs/adr/0021): the naive weighted blend silently missed
// an exact-body-different-name pair (structural=0.86) because it had zero
// resolved calls, scoring 0.6*0.86+0.4*0=0.52 — below any reasonable
// threshold, for a pair that should obviously have matched.
func combinedScore(structural, behavioral float64, noBehavioralSignal bool) float64 {
	if noBehavioralSignal {
		return structural
	}
	return structuralWeight*structural + behavioralWeight*behavioral
}

// jaccard is the exact (not estimated) Jaccard similarity of two string
// sets — used for the behavioral score, where sets are small enough
// (an entity's own edge count) that exact computation is cheap; MinHash
// estimation (minhash.go) is reserved for the structural score, where
// shingle sets can be large.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0 // no behavioral signal from either side — neutral, never a false "perfect match"
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
