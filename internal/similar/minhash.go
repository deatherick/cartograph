package similar

import (
	"encoding/binary"
	"hash/fnv"
)

// numHashes is H, the MinHash signature length. numBands*rowsPerBand must
// equal numHashes. 64/16/4 is a standard, unremarkable choice for this
// scale (thousands, not millions, of candidate entities) — not tuned
// against a benchmark, since LSH's banding parameters trade recall against
// candidate-set size in a way that only matters at a scale this project
// doesn't operate at yet; revisit with real numbers if it ever does.
const (
	numHashes   = 64
	numBands    = 16
	rowsPerBand = numHashes / numBands
)

type signature [numHashes]uint64

// minHashSignature computes shingles' MinHash signature: for each of
// numHashes independent hash functions, the minimum hash value over every
// shingle in the set. Standard MinHash property this whole package relies
// on: the fraction of positions where two signatures agree is an unbiased
// estimator of the Jaccard similarity of the underlying sets (see
// estimatedJaccard) — this is what makes comparing two fixed-size uint64
// arrays a stand-in for comparing two arbitrarily large shingle sets.
func minHashSignature(shingles map[uint64]struct{}) signature {
	var sig signature
	for i := range sig {
		sig[i] = ^uint64(0)
	}
	for shingle := range shingles {
		for i := 0; i < numHashes; i++ {
			if h := seededHash(shingle, i); h < sig[i] {
				sig[i] = h
			}
		}
	}
	return sig
}

// seededHash produces numHashes independent-enough hash functions from a
// single hash primitive (FNV-1a) by mixing in a per-function seed, rather
// than a mathematical universal-hash family (avoiding modular-arithmetic
// overflow bookkeeping for no real accuracy benefit at this package's
// scale) — a common practical approach for MinHash implementations.
// Deterministic: the same shingle+seed always hashes the same way, run to
// run, machine to machine — required for reproducible signatures (and
// therefore reproducible test expectations).
func seededHash(x uint64, seed int) uint64 {
	h := fnv.New64a()
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[0:8], x)
	binary.LittleEndian.PutUint64(buf[8:16], uint64(seed))
	_, _ = h.Write(buf[:])
	return h.Sum64()
}

// estimatedJaccard is the fraction of matching positions between two
// MinHash signatures — see minHashSignature's doc for why this estimates
// the true Jaccard similarity of the two entities' shingle sets without
// ever comparing those (potentially large) sets directly.
func estimatedJaccard(a, b signature) float64 {
	matches := 0
	for i := range a {
		if a[i] == b[i] {
			matches++
		}
	}
	return float64(matches) / float64(numHashes)
}

// lshBuckets returns one bucket key per band of sig. Two entities sharing
// ANY band's bucket key become LSH candidates — the mechanism that turns
// "compare every pair" (O(N^2), infeasible past a few thousand entities,
// per the master plan's own funnel design) into "compare only entities
// that hashed into the same bucket somewhere" (O(N) to generate,
// typically far fewer actual candidate pairs than N^2/2). A real Jaccard
// similarity below the LSH threshold this banding is tuned for MAY be
// missed entirely (a false negative) — the standard, accepted LSH
// tradeoff, not a bug.
func lshBuckets(sig signature) [numBands]uint64 {
	var buckets [numBands]uint64
	for b := 0; b < numBands; b++ {
		h := fnv.New64a()
		var buf [8 * rowsPerBand]byte
		for r := 0; r < rowsPerBand; r++ {
			binary.LittleEndian.PutUint64(buf[r*8:r*8+8], sig[b*rowsPerBand+r])
		}
		_, _ = h.Write(buf[:])
		buckets[b] = h.Sum64()
	}
	return buckets
}
