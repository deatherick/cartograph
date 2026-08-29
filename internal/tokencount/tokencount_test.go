package tokencount

import "testing"

func TestCount_Deterministic(t *testing.T) {
	s := "function formatCents(cents: number): string { return `$${(cents/100).toFixed(2)}`; }"
	a := Count(s)
	b := Count(s)
	if a != b {
		t.Fatalf("Count is not deterministic: %d != %d", a, b)
	}
	if a <= 0 {
		t.Fatalf("Count returned non-positive: %d", a)
	}
}

func TestCount_EmptyString(t *testing.T) {
	if got := Count(""); got != 0 {
		t.Fatalf("Count(\"\") = %d, want 0", got)
	}
}

func TestCount_LongerTextMoreTokens(t *testing.T) {
	short := "hello"
	long := "hello world, this is a much longer piece of text with many more words in it"
	if Count(long) <= Count(short) {
		t.Fatalf("expected longer text to have more tokens: short=%d long=%d", Count(short), Count(long))
	}
}

func TestEstimatorError_NonZero(t *testing.T) {
	s := "const x = 1; function foo(bar) { return bar + x; }"
	ratio := EstimatorError(s)
	if ratio <= 0 {
		t.Fatalf("EstimatorError ratio should be positive, got %f", ratio)
	}
}

func TestEstimatorError_EmptyString(t *testing.T) {
	if got := EstimatorError(""); got != 1.0 {
		t.Fatalf("EstimatorError(\"\") = %f, want 1.0", got)
	}
}
