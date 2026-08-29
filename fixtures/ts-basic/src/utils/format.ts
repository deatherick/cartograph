export function formatCents(cents: number): string {
  return `$${(cents / 100).toFixed(2)}`;
}

// NOTE: near-duplicate of formatCents, same shape, different unit —
// deliberately included so the Similarity Engine (Fase 5) has a real
// near-duplicate pair to detect once it exists.
export function formatPercent(basisPoints: number): string {
  return `${(basisPoints / 100).toFixed(2)}%`;
}

export function truncate(text: string, maxLen: number): string {
  if (text.length <= maxLen) return text;
  return text.slice(0, maxLen - 1) + "…";
}
