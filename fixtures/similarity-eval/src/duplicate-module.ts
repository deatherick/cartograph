// A whole function copy-pasted verbatim (same name, same body) into a
// different file — the real "exact" category: internal/similar's L1
// fingerprint (Anchor.ContentHash) only matches when the declaration text
// is byte-identical, name included, which is what this represents.
export function computeDiscount(price: number, pct: number): number {
  const discount = price * (pct / 100);
  const rounded = Math.round(discount * 100) / 100;
  return price - rounded;
}
