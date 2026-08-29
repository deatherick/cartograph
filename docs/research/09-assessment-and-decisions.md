# 09 — Reuse assessment and derived decisions

Deliverable requested by §51 of the master prompt. Grafel is MIT and **no code is copied**: the
knowledge is reimplemented. No dependency on `github.com/cajasmota/grafel`.

## 1. What we adopt conceptually almost as-is

| Idea | Origin | Why |
|---|---|---|
| Taxonomy of **dispositions** + `bug_rate` as a metric | ADR-0015, `internal/resolve/refs.go` | Turns graph quality into an auditable number and separates "unresolvable by design" from "our bug" |
| Bare-name allowlist + generic-name exclusion list | ADR-0011 | It's the largest source of false positives; its policy is validated against a corpus |
| Per-file import table, resolved before bare names | ADR-0013 | The highest scoping signal without type inference |
| Tracking the receiver's static type + single-candidate rule | ADR-0012 | Pays off in TS and C#; never guesses |
| Separation extractor → refs → resolver | ADR-0010 | Makes extraction parallelizable and cacheable per file |
| Pre-computing centrality/community/PageRank at index time | ADR-0005 | Query time is attribute reading; these are direct terms in our ranker |
| Atomic file handoff + mmap + mtime revalidation | ADR-0024 | Decouples reader and writer with no locks or notifications |
| Three-layer exclusions: static skip + `.gitignore` + adaptive quarantine | `internal/daemon/watch/quarantine.go` | Defense against reindex loops from build directories |
| Two-layer identity: local ID per repo, `<repo>::` prefix in composition | ADR-0009 | Allows independent per-repo rebuild, with no global state |
| Repair-loop safety rules (resolution allowlist, `source` per edge, reversibility, determinism) | ADR-0015 | Good and free |
| Error-ratio gate of 10%, per-parse timeout, tree closed on every path | `internal/treesitter/parser.go` | Safeguards against pathological files and C heap leaks |

## 2. What we adapt with deliberate changes

| Topic | Them | Us | Reason |
|---|---|---|---|
| tree-sitter binding | Started with `smacker` (dead), migrating to the official one: 245 files, 1,758 refs to `Node` | Official binding from day 1, **encapsulated in `internal/parser`** with an architecture test | Not paying for a migration that's already documented |
| Extraction | Manual traversal: 21k lines for TS/JS, 14k for Python. **Zero `.scm` files** | Declarative `.scm` queries for the structural 80%, traversal only for scoping | A new language should be a query file |
| Transport of unresolved refs | Strings with magic grammar (`scope:k:sub:lang:file:name`), with documented drift (#49) and a cross-resolution bug (#3936) | Typed struct with an enumerated `Scope` | Makes bug #3936 impossible to write |
| Entity identity | Hash of **file + line** + name | Hash of `(repo, qualified_name, kind)`; location in a mutable `Anchor` | With their scheme, moving a function invalidates its ID and breaks the ledger's handles |
| On-disk format | JSON → FlatBuffers; edges by **string ID** → neighbors in **O(R)** | Custom mmap-able binary with integer IDs and **CSR adjacency** → neighbors in O(1) | Closes the weakness their own ADR-0016 leaves open, and attacks the size driver (repeated strings) |
| Tabular persistence | Rejected SQLite for the graph | SQLite **not** for the graph, yes for projects, decisions, ledger, metrics and FTS5 | They're right about the graph; SQLite wins where there are small, transactional writes |
| Watcher on macOS | fsnotify/kqueue + descriptor budget (1 fd per file: 40k fds for a repo) | **FSEvents** on darwin: a single recursive stream, zero fds per file | Eliminating the problem instead of managing the scarcity, on our primary platform |
| Token benchmark | Tokens only, `len/4` estimator, "read the right files" baseline | Tokens **+ recall@gold + precision@gold**, real tokenizer, baseline per trace | Without recall, saving tokens is trivial by returning less |
| Taxonomy | 3-layer `SCOPE.*` (runtime/framework/infra), ~20 kinds | Concrete kinds in V0 (`Class`, `Function`…); framework/infra when there's an extractor | A capsule is read by human and LLM; and a kind without an extractor is a broken promise |

## 3. What we discard

| What | Why |
|---|---|
| **Split serve/engine processes** | Solution to scale incidents we don't have. We adopt the handoff pattern that makes it cheap afterward |
| **Writer sharding** | They themselves **deferred it after measuring**: their corpus of 287k entities / 1.3M relationships gives 0.4 GiB, 5× margin. They wrote the design before measuring; we write none |
| **11,333 lines of external-package synthesis** | The long tail of frameworks chased by hand. We bet on covering it with the residuals loop + disambiguation in the capsule |
| **50+ languages, 263 frameworks** | V0 is three languages done well |
| **FlatBuffers and its `flatc` codegen** | Brings us the builder's 2 GiB cliff, verbose bindings and a codegen step, and solves neither size nor edge O(R) |
| **`len/4` token estimator** | Unbounded error over code |
| **Closed `SCOPE.*` enum in V0** | Adding an edge kind is breaking; we don't fix the vocabulary before having the extractors |

## 4. License implications

- Grafel is **MIT**. Copying would be legal with attribution; **we don't copy**, so no derivative
  work is generated.
- `~/code/_ref/grafel` is a read-only reference outside the project's repo. Never a submodule,
  never a dependency, never vendored.
- `NOTICE.md` credits only real OSS dependencies (tree-sitter and its grammars, fsnotify
  or fsevents, SQLite, etc.).
- If a specific algorithm is ever adapted closely, it's flagged in the file
  (`// adapted from grafel (MIT), see docs/research/...`) and credited. It must be a documented
  exception. **There is none today.**
- We don't reuse MCP tool names, on-disk file names (`.grafel/graph.fb`),
  the `SCOPE.*` schema, or the stub grammar.

## 5. Changes this discovery introduces to the approved plan

1. **Snapshot**: `encoding/gob` is dropped. Mmap-able binary format with integer IDs and
   CSR adjacency. (Note 04 — justified with their measured numbers.)
2. **Watcher**: FSEvents on macOS, not fsnotify/kqueue. Cost model per build tag.
   (Note 05 — it's a real blocker, not an optimization.)
3. **Extraction**: declarative `.scm` queries, with the binding encapsulated and an
   architecture test that enforces it. (Note 01.)
4. **New CI metric**: dispositions `bug_rate`, alongside tokens and recall.
   V1 target informed by their real measurement: **≤12%, aiming for ≤8%**.
5. **Residuals enter the capsule** as disambiguation questions with candidates, not
   just as a list of failures. (Note 08 — it's product differentiation, not a footnote.)
6. **The source ladder gains an evidence rung**: every item carries visible `provenance`
   and `confidence`, fed by the dispositions taxonomy.
7. **Phase 1 adds the fixed-order resolution pipeline**
   (`same-file → import-table → receiver-type → bare-name → disposition`) as an
   explicit structure, not as something that emerges.

## 6. Main risk identified

The strongest bet of the plan is that **declarative `.scm` queries cover the structural 80%**
where Grafel wrote 21k lines of manual traversal for TS/JS. If that bet fails, Phase 1
lengthens significantly.

**Mitigation:** Phase 1 is a single language precisely to discover this cheaply. The exit
criterion (import-resolution precision ≥95% on an annotated fixture) is the signal. If
midway through Phase 1 the queries don't measure up, it falls back to manual traversal only
for TS, and is reevaluated before touching C# and Python.
