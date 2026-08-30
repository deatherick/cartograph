package similar

import "testing"

func TestMinHashSignature_Deterministic(t *testing.T) {
	shingles := shingleHashes(tokenize("function add(a, b) { return a + b; }"))
	s1 := minHashSignature(shingles)
	s2 := minHashSignature(shingles)
	if s1 != s2 {
		t.Fatal("expected the same shingle set to always produce the same MinHash signature")
	}
}

func TestEstimatedJaccard_IdenticalSets_IsOne(t *testing.T) {
	shingles := shingleHashes(tokenize("function add(a, b) { return a + b; }"))
	sig := minHashSignature(shingles)
	if got := estimatedJaccard(sig, sig); got != 1.0 {
		t.Fatalf("expected identical signatures to estimate Jaccard=1.0, got %v", got)
	}
}

func TestEstimatedJaccard_CompletelyDifferentTokens_IsLow(t *testing.T) {
	a := shingleHashes(tokenize("function add(a, b) { return a + b; }"))
	b := shingleHashes(tokenize("class Widget extends Component { render() { return null; } }"))
	sigA, sigB := minHashSignature(a), minHashSignature(b)
	got := estimatedJaccard(sigA, sigB)
	if got > 0.3 {
		t.Errorf("expected two structurally unrelated snippets to estimate a low Jaccard, got %v", got)
	}
}

func TestEstimatedJaccard_MostlySameTokens_IsHigh(t *testing.T) {
	// A single inserted statement in a longer function shifts only a
	// small fraction of its 5-token shingles — unlike a short snippet,
	// where one insertion can shift most of them (shingling is inherently
	// sensitive to length; that's expected, not a bug).
	base := "function computeTotal(items) { let sum = 0; let count = 0; for (const item of items) { sum += item.price; count += 1; } return sum / count; }"
	changed := "function computeTotal(items) { let sum = 0; let count = 0; console.log('computing'); for (const item of items) { sum += item.price; count += 1; } return sum / count; }"
	a := shingleHashes(tokenize(base))
	b := shingleHashes(tokenize(changed))
	sigA, sigB := minHashSignature(a), minHashSignature(b)
	got := estimatedJaccard(sigA, sigB)
	if got < 0.4 {
		t.Errorf("expected a near-identical function with one inserted statement to estimate a reasonably high Jaccard, got %v", got)
	}
}

func TestLSHBuckets_IdenticalSignatures_ShareEveryBand(t *testing.T) {
	shingles := shingleHashes(tokenize("function add(a, b) { return a + b; }"))
	sig := minHashSignature(shingles)
	b1 := lshBuckets(sig)
	b2 := lshBuckets(sig)
	if b1 != b2 {
		t.Fatal("expected identical signatures to produce identical bucket keys in every band")
	}
}

func TestLSHBuckets_SimilarEntities_ShareAtLeastOneBand(t *testing.T) {
	// A realistic near-duplicate pair should collide in at least one LSH
	// band — otherwise Find's candidate generation would never even
	// consider scoring it, regardless of how similar it truly is.
	a := shingleHashes(tokenize("function computeTotal(items) { let sum = 0; for (const item of items) { sum += item.price; } return sum; }"))
	b := shingleHashes(tokenize("function computeTotal(items) { let sum = 0; for (const item of items) { sum += item.price * item.qty; } return sum; }"))
	sigA, sigB := minHashSignature(a), minHashSignature(b)
	bucketsA, bucketsB := lshBuckets(sigA), lshBuckets(sigB)
	shared := false
	for i := range bucketsA {
		if bucketsA[i] == bucketsB[i] {
			shared = true
		}
	}
	if !shared {
		t.Error("expected two near-duplicate functions to share at least one LSH band bucket")
	}
}
