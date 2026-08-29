# 06 — Token economy measurement

## Problem

Proving that the tool saves tokens. It's the product's central thesis, so the measurement has to
be honest or the whole product rests on a made-up number.

## How Grafel solved it

`cmd/bench-tokens` — 179 + 142 lines. Compares, per question:

- **Graph tokens**: the cost of the `grafel_find` payload (the minimal subgraph that answers it).
- **File-read tokens**: the cost of reading **in full** each distinct file the answer touches.
- **Ratio** = file-read ÷ graph. Higher is better.

Implementation details:
- Token estimator: `len(s) / 4`. Explicitly the same char/4 the MCP layer uses to report
  `payload_token_estimate`, so the numbers "line up" with what the MCP reports.
- Baseline files are recovered from the payload itself via a `path:line` regex, without a second
  round-trip to the graph.
- Fallback of 3,000 tokens per unreadable file, *"so the baseline isn't silently underestimated"*
  — good instinct.
- Markdown output, with a per-row ratio and an aggregate ratio that skips zero-cost rows so they
  don't distort it.

## The three gaps

**1. There is no correctness measure at all.** The benchmark measures only tokens. No recall, no
precision, no gold set. And that's the entire problem with this kind of metric:
**you can always save tokens by returning less**. A `grafel_find` that returns an empty line has
an infinite ratio. Without a measure of whether the compact answer actually contained the needed
information, the savings number means nothing.

**2. The baseline is a straw man, in the direction that both favors and disfavors them.**
"Read in full the files the answer touches" assumes the agent **already knows which files to
read** — which is precisely what the tool provides. The real baseline of a graph-less agent is
worse (exploratory searches, files read and discarded, dead ends), but it's also harder to
defend. As it stands, the baseline corresponds to no real agent behavior.

**3. `len/4` is not a tokenizer.** It's a reasonable approximation for English prose and fairly
bad for code: camelCase identifiers, indentation, symbols and paths tokenize very differently.
The error is neither bounded nor measured anywhere.

## How we solve it

`ctxbench` is built in Phase 0b, **before** the product, and fixes all three:

1. **Mandatory gold set.** Every task in the task set carries an annotated set of entities and
   ranges that a correct answer requires. These are always reported together:

   ```
   tokens_capsule · tokens_baseline · recall@gold · precision@gold · latency
   ```

   **A token saving is never reported without its recall alongside it.** A presentation rule, not
   just an implementation one: the savings metric never appears alone, in any report or in the
   UI. The CI gate is joint — `≥70% savings **with** recall ≥0.85`. Lowering recall to raise
   savings breaks the build.

2. **Baseline from trace, not from guesswork.** A real trace is recorded of an agent solving the
   task without the tool (searches and reads included, dead ends included) and what it consumed
   is counted. It's more work and it's the only defensible baseline. It's versioned alongside the
   task set so it's reproducible.
   - Their "oracle" baseline (reading the correct files in full) is also reported as a
     conservative lower bound. Having both figures is more honest than having one: the first says
     how much we save in practice, the second how much we save even against a perfectly lucky
     agent.

3. **A real tokenizer**, not `len/4`. And if none is available, the estimator's error against the
   real tokenizer is measured over the corpus itself and the deviation is published. An estimator
   with known error is usable; one with unknown error is not.

4. **The Context Ledger's savings are measured explicitly**, a dimension they don't have: a
   5-call chained session versus 5 independent calls.

## Why it's different

Their benchmark answers "how much smaller is our payload?" Ours has to answer "how much cheaper
is it to solve the task **correctly**?" These are different questions, and only the second one
justifies the product.
