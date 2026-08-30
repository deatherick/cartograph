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
