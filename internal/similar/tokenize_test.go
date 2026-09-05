package similar

import "testing"

func TestTokenize_StripsComments(t *testing.T) {
	src := "function foo() { // a comment\n  return 1; /* block\n comment */ }"
	tokens := tokenize(src)
	for _, tok := range tokens {
		if tok == "comment" || tok == "block" {
			t.Fatalf("expected comment text stripped, got token %q in %v", tok, tokens)
		}
	}
}

func TestTokenize_CollapsesNumbersAndStrings(t *testing.T) {
	tokens := tokenize(`return "hello world" + 42.5;`)
	got := map[string]int{}
	for _, tok := range tokens {
		got[tok]++
	}
	if got["STR"] != 1 {
		t.Errorf("expected exactly one STR token, got %+v", got)
	}
	if got["NUM"] != 1 {
		t.Errorf("expected exactly one NUM token, got %+v", got)
	}
	if got["hello"] > 0 || got["world"] > 0 {
		t.Errorf("expected the string literal's contents not to leak as separate tokens, got %+v", got)
	}
}

func TestTokenize_IdentifierRunsAreSingleTokens(t *testing.T) {
	tokens := tokenize("myVariable_2 + other")
	want := []string{"myVariable_2", "+", "other"}
	if len(tokens) != len(want) {
		t.Fatalf("got %v, want %v", tokens, want)
	}
	for i, w := range want {
		if tokens[i] != w {
			t.Errorf("token %d: got %q, want %q", i, tokens[i], w)
		}
	}
}

func TestTokenize_TwoDifferentlyFormattedButEquivalentSnippets_ProduceSameTokens(t *testing.T) {
	a := tokenize("function add(a, b) { return a + b; }")
	b := tokenize("function   add(a,b)   {\n  return a + b;\n}\n// trailing comment")
	if len(a) != len(b) {
		t.Fatalf("expected reformatting/whitespace/trailing-comment differences to not change the token count: %v vs %v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("token %d differs: %q vs %q", i, a[i], b[i])
		}
	}
}

func TestNormalizeIdentifiers_RenamedVariablesBecomeIdenticalStreams(t *testing.T) {
	a := normalizeIdentifiers(tokenize("let total = 0; for (const item of items) { total += item; } return total;"))
	b := normalizeIdentifiers(tokenize("let weight = 0; for (const parcel of parcels) { weight += parcel; } return weight;"))
	if len(a) != len(b) {
		t.Fatalf("expected identical token counts after normalization, got %d vs %d: %v vs %v", len(a), len(b), a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("token %d differs after normalization: %q vs %q (%v vs %v)", i, a[i], b[i], a, b)
		}
	}
}

func TestNormalizeIdentifiers_KeywordsStayLiteral(t *testing.T) {
	tokens := normalizeIdentifiers(tokenize("if (x) { return y; } else { return z; }"))
	wantLiteral := map[string]bool{"if": true, "return": true, "else": true}
	for _, tok := range tokens {
		if wantLiteral[tok] {
			continue
		}
		if tok == "(" || tok == ")" || tok == "{" || tok == "}" || tok == ";" {
			continue
		}
		if tok != "ID1" && tok != "ID2" && tok != "ID3" {
			t.Errorf("unexpected token %q in normalized stream %v", tok, tokens)
		}
	}
	// x, y, z are three DIFFERENT identifiers -> three distinct placeholders.
	seen := map[string]bool{}
	for _, tok := range tokens {
		if tok == "ID1" || tok == "ID2" || tok == "ID3" {
			seen[tok] = true
		}
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 distinct placeholders for x/y/z, got %v in %v", seen, tokens)
	}
}

func TestNormalizeIdentifiers_SameIdentifierReusedKeepsSamePlaceholder(t *testing.T) {
	tokens := normalizeIdentifiers(tokenize("total = total + 1;"))
	// Both occurrences of "total" must map to the SAME placeholder — this
	// is what preserves a real reuse pattern (an accumulator referenced
	// twice) instead of collapsing to a blanket "every identifier is the
	// same" placeholder.
	var placeholders []string
	for _, tok := range tokens {
		if tok == "ID1" {
			placeholders = append(placeholders, tok)
		}
	}
	if len(placeholders) != 2 {
		t.Errorf("expected both occurrences of the same identifier to normalize to the same placeholder, got tokens %v", tokens)
	}
}

func TestNormalizeIdentifiers_DifferentOperationStillDiffers(t *testing.T) {
	// Same variable-reuse shape, but a genuinely different operation
	// (`* 2`) inside the loop — normalization must NOT erase a real
	// structural difference, only a naming one.
	a := normalizeIdentifiers(tokenize("total += item;"))
	b := normalizeIdentifiers(tokenize("weight += parcel * 2;"))
	if len(a) == len(b) {
		t.Errorf("expected a genuinely different operation to still produce a different token count after normalization, got %v vs %v", a, b)
	}
}

func TestLooksLikeIdentifier(t *testing.T) {
	cases := map[string]bool{
		"foo": true, "_bar": true, "myVar2": true,
		"NUM": false, "STR": false, "+": false, ";": false, "(": false,
	}
	for tok, want := range cases {
		if got := looksLikeIdentifier(tok); got != want {
			t.Errorf("looksLikeIdentifier(%q) = %v, want %v", tok, got, want)
		}
	}
}

func TestShingleHashes_ShortTokenStream_YieldsOneShingle(t *testing.T) {
	set := shingleHashes([]string{"a", "b"})
	if len(set) != 1 {
		t.Fatalf("expected exactly one shingle for a token stream shorter than shingleSize, got %d", len(set))
	}
}

func TestShingleHashes_Empty_YieldsEmptySet(t *testing.T) {
	if set := shingleHashes(nil); len(set) != 0 {
		t.Fatalf("expected an empty shingle set for no tokens, got %+v", set)
	}
}

func TestShingleHashes_IdenticalTokens_YieldIdenticalSets(t *testing.T) {
	tokens := []string{"a", "b", "c", "d", "e", "f", "g"}
	s1 := shingleHashes(tokens)
	s2 := shingleHashes(append([]string(nil), tokens...))
	if len(s1) != len(s2) {
		t.Fatalf("expected identical token streams to produce identically-sized shingle sets, got %d vs %d", len(s1), len(s2))
	}
	for h := range s1 {
		if _, ok := s2[h]; !ok {
			t.Errorf("shingle %d present in s1 but not s2", h)
		}
	}
}
