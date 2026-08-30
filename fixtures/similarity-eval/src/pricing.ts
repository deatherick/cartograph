// Fixture for internal/similar's labeled evaluation
// (internal/similar/eval_test.go). Every function/pair below is
// hand-labeled by category in that test file's own comments — this file
// only needs to exist as real, extractable TypeScript source.

export function computeDiscount(price: number, pct: number): number {
  const discount = price * (pct / 100);
  const rounded = Math.round(discount * 100) / 100;
  return price - rounded;
}

// Exact duplicate of computeDiscount above (byte-identical body,
// different declared name) — a real copy-paste, category "exact".
export function applyDiscount(price: number, pct: number): number {
  const discount = price * (pct / 100);
  const rounded = Math.round(discount * 100) / 100;
  return price - rounded;
}

// Renamed-only duplicate of computeTotal below: identical body, only the
// function's own name differs — category "renamed".
export function computeSum(items: number[]): number {
  let total = 0;
  let processed = 0;
  for (const item of items) {
    total += item;
    processed += 1;
  }
  return total / processed;
}

export function computeTotal(items: number[]): number {
  let total = 0;
  let processed = 0;
  for (const item of items) {
    total += item;
    processed += 1;
  }
  return total / processed;
}

// Structurally similar to computeTotal/computeSum above (same shape: a
// loop accumulating a running value, then a final division) but with
// different variable names AND a different operation inside the loop —
// category "structural", expected to score meaningfully but lower than
// the renamed-only pair above.
export function computeAverageWeight(parcels: number[]): number {
  let weight = 0;
  let counted = 0;
  for (const parcel of parcels) {
    weight += parcel * 2;
    counted += 1;
  }
  return weight / counted;
}

// Behaviorally similar pair: different token structure/shape entirely,
// but both call the exact same two helper functions in the same way —
// category "behavioral".
export function validateAndSave(email: string, name: string): boolean {
  if (!checkEmailFormat(email)) {
    return false;
  }
  persistRecord(email, name);
  return true;
}

export function registerContact(email: string, name: string): boolean {
  const valid = checkEmailFormat(email);
  if (valid) {
    persistRecord(email, name);
  }
  return valid;
}

function checkEmailFormat(email: string): boolean {
  return email.includes("@");
}

function persistRecord(email: string, name: string): void {
  // no-op stub for the fixture
}

// Genuinely unrelated to everything above — category "unrelated", must
// not pair with anything.
export class HttpRequestBuilder {
  private headers: Record<string, string> = {};

  withHeader(name: string, value: string): HttpRequestBuilder {
    this.headers[name] = value;
    return this;
  }

  build(): Record<string, string> {
    return { ...this.headers };
  }
}

// A pair of trivially short one-liners — category "false-positive-risk":
// textually similar but both should be filtered out entirely by
// minBodyTokens before ever reaching a score, so neither should appear as
// a pair at all (a true negative by omission, not by scoring low).
export function getX(): number {
  return 1;
}

export function getY(): number {
  return 1;
}
