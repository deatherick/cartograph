// Color helpers for the cosmos.gl graph canvas (src/pages/GraphPage.tsx).
//
// parseColor/writeNormalizedRGBA/readPastelScale/pastelAt are adapted from
// Grafel's webui-v2 lib/graph-colors.ts (MIT License — see NOTICE.md),
// which carries a hard-won, load-bearing lesson worth reusing verbatim
// rather than rediscovering: cosmos.gl 2.6.4 reads the per-point/per-link
// color attribute as a RAW float vec4 and assigns it straight to the
// fragment color — it does NOT divide by 255. Colors MUST be uploaded in
// the 0..1 float range; any channel > 1 clamps to 1.0 in the shader, so
// every node silently renders white. Kept the human-friendly 0-255 parse
// space and normalize to 0..1 only when writing the GPU buffer.
//
// Trimmed from the original: degree-gradient coloring and the cross-repo/
// cross-module link palette (multi-repo concepts Cartograph doesn't have
// yet, ADR-0012's documented gap) were not carried over — colorForKind
// below is Cartograph's own addition, keyed by model.Kind instead of by
// repo/community.

export type RGBA = [number, number, number, number] // rgb 0-255, a 0-1

export const PASTEL_SCALE_SIZE = 10

const SLATE_500: RGBA = [100, 116, 139, 1]

/** Parse #rrggbb / #rgb / rgb()/rgba() into [r,g,b,a] (rgb 0-255, a 0-1). */
export function parseColor(c: string | null | undefined): RGBA {
  if (!c || typeof c !== 'string') return SLATE_500
  const s = c.trim()
  if (s.startsWith('#')) {
    let hex = s.slice(1)
    if (hex.length === 3) hex = hex.split('').map((ch) => ch + ch).join('')
    const r = parseInt(hex.slice(0, 2), 16)
    const g = parseInt(hex.slice(2, 4), 16)
    const b = parseInt(hex.slice(4, 6), 16)
    const a = hex.length >= 8 ? parseInt(hex.slice(6, 8), 16) / 255 : 1
    if (Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b)) return SLATE_500
    return [r, g, b, a]
  }
  const m = s.match(/rgba?\(([^)]+)\)/)
  if (m) {
    const parts = m[1].split(',').map((p) => parseFloat(p.trim()))
    return [parts[0] ?? 0, parts[1] ?? 0, parts[2] ?? 0, parts[3] ?? 1]
  }
  return SLATE_500
}

/** Write an RGBA (rgb 0-255, a 0-1) into a packed GPU buffer at quad index
 * i, NORMALIZING rgb to the 0..1 range cosmos.gl's shaders expect. */
export function writeNormalizedRGBA(out: Float32Array, i: number, rgba: RGBA): void {
  out[i * 4] = rgba[0] / 255
  out[i * 4 + 1] = rgba[1] / 255
  out[i * 4 + 2] = rgba[2] / 255
  out[i * 4 + 3] = rgba[3]
}

/** Resolve the pastel categorical scale from tokens.css at runtime, so the
 * light/dark toggle flows through automatically. Returns 0-255 RGBA. */
export function readPastelScale(root: HTMLElement = document.documentElement): {
  fill: RGBA[]
  ink: RGBA[]
} {
  const style = getComputedStyle(root)
  const fill: RGBA[] = []
  const ink: RGBA[] = []
  for (let i = 1; i <= PASTEL_SCALE_SIZE; i++) {
    fill.push(parseColor(style.getPropertyValue(`--pastel-${i}`)))
    ink.push(parseColor(style.getPropertyValue(`--pastel-${i}-ink`)))
  }
  return { fill, ink }
}

export function pastelAt(scale: RGBA[], colorIndex: number): RGBA {
  if (scale.length === 0) return SLATE_500
  const idx = ((colorIndex - 1) % scale.length + scale.length) % scale.length
  return scale[idx]
}

// Cartograph's own Kind -> pastel-slot mapping (1-indexed, matching
// tokens.css's --pastel-1..10).
const KIND_SLOT: Record<string, number> = {
  Class: 1,
  Function: 2,
  Interface: 5,
  Method: 7,
  Enum: 6,
  TypeAlias: 9,
  Test: 4,
  Property: 3,
}

export function kindSlot(kind: string): number {
  return KIND_SLOT[kind] ?? 8
}

export const KIND_LEGEND = Object.keys(KIND_SLOT)
