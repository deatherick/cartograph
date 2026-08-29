# Research — discovery on Grafel (Phase 0a)

Grafel (`cajasmota/grafel`, Go, MIT) was cloned into `~/code/_ref/grafel` and read thoroughly so
as not to re-solve problems it already solved. **No code was copied.** These notes record,
by topic: what the problem is, how they solved it, how we solve it, and why differently.

Scale of the repo studied: 9,063 files, 27 ADRs, ~21k lines in the TS/JS extractor alone.

## Notes

| Note | Topic | Main finding |
|---|---|---|
| [01](01-parser-and-treesitter-binding.md) | Parser and tree-sitter binding | The `smacker` binding is dead; use the official one from day 1. They have **zero `.scm` files** and 21k lines of manual traversal for TS/JS, with the binding filtered down to 245 files |
| [02](02-refs-and-dispositions.md) | Refs and dispositions | Their best idea: dispositions taxonomy + `bug_rate = (bug-extractor + bug-resolver) / total` as an auditable metric. Their worst decision: transporting refs as strings with magic grammar |
| [03](03-import-resolution-and-bare-names.md) | Resolution | Bare names = biggest source of false positives. Allowlist + generic-name exclusion + per-file import table + receiver static type, in fixed order |
| [04](04-storage-and-graph-format.md) | Storage | JSON→FlatBuffers gave them **80×** on cold open (132ms→1.6ms). They left open: edges by string ID ⇒ neighbors in **O(R)**. We close it with integer IDs + CSR |
| [05](05-watcher-and-invalidation.md) | Watcher | **macOS/kqueue spends 1 descriptor per file**: 40,079 fds for a repo, 65% of the process ceiling. We use FSEvents. Also: three-layer exclusions with adaptive quarantine |
| [06](06-token-measurement.md) | Token measurement | Their benchmark measures tokens and **doesn't measure correctness**. Tokens are always saved by returning less |
| [07](07-identity-taxonomy-and-cross-repo.md) | Identity and taxonomy | Their ADR contradicts their code in `EntityID`. Excluding the line is correct and makes overloads and `partial` collide by construction |
| [08](08-process-architecture-and-residuals.md) | Process and residuals | Writer/reader handoff via atomic file + mmap + mtime, no locks. Plus the most useful calibration data point: **their real bug-rate is 8–12%** |
| [09](09-assessment-and-decisions.md) | **Assessment** | What we adopt, what we adapt, what we discard, license, and the 7 changes this introduces to the plan |
| [backlog](edge-case-backlog.md) | Edge cases | **80 cases** derived from their real bugs, ready to become fixtures |

## The five findings that change the plan

1. **macOS/kqueue exhausts descriptors** (note 05) — a real blocker on our primary platform,
   not an optimization. FSEvents on darwin.
2. **`encoding/gob` was the wrong choice** (note 04) — their numbers prove that any format
   with O(N) decode is the latency floor of *every* call. Mmap-able binary snapshot with
   CSR adjacency, which also closes the O(R) they left open.
3. **Zero use of tree-sitter queries** (note 01) — 21k lines of manual traversal for TS/JS.
   Our main bet, and the plan's main risk.
4. **The dispositions taxonomy and `bug_rate`** (note 02) — a graph-quality metric that
   wasn't in the plan and becomes a CI gate. Calibration: 8–12% is normal.
5. **Their token benchmark doesn't measure correctness** (note 06) — confirms that `recall@gold`
   alongside savings isn't a methodological detail, it's what separates an honest measurement
   from a useless one.

## Usage rule

`~/code/_ref/grafel` is a read-only reference, outside the project's repo. Never a submodule,
never a dependency, never vendored. See [09](09-assessment-and-decisions.md) §4 for license.
