# 02 — Refs and dispositions model (auditable graph quality)

## Problem

An extractor sees `foo.bar(x)` and doesn't know which entity it points to. Someone has to decide
whether that becomes an edge, and if not, **why not**. Without that accounting there's no way to know
whether the graph is good.

## How Grafel solved it

**Extractor / resolver separation via a refs stream.** The extractor emits
"structural references" (this AST site mentions this symbol in this way); the
resolver turns them into a concrete edge or into a *disposition*.

ADR-0010 chose **Format A — flat record** with a `kind` field as discriminator and
irrelevant fields left zeroed, instead of a sum type per variant. Reason: Go doesn't have ergonomic
sum types and the resolver is a switch over `kind` either way.

**The best idea in the repo — the dispositions taxonomy.** Every inspected endpoint falls into
exactly one bucket:

| Disposition | Meaning |
|---|---|
| `resolved` | Rewritten to an entity ID |
| `external-known` | Points to an external package **on the allowlist** (django, react, fmt) |
| `external-unknown` | External package not catalogued |
| `external-sql` | Unresolved SQL surface, counted separately |
| `dynamic` | Intrinsically unresolvable statically (reflection, dynamic import, env-based names, template strings). **Not a bug** |
| `bug-extractor` | The stub has shape `Kind:Name` but the graph has 0 entities with that name. An extractor should have emitted an entity and didn't |
| `bug-resolver` | The name DOES exist in the graph but the resolver failed to disambiguate |
| `unclassified` | Catch-all. Must be 0 in production |

And on top of that, **the metric that makes it useful**:

```
bug_rate = (bug-extractor + bug-resolver) / total
```

That turns "how good is the graph?" into an auditable number that can be watched in CI.
It cleanly separates "can't be resolved by design" from "it's our fault."

**The mistake they made:** the stubs are *strings*. The resolver parses a grammar of
magic strings (`scope:<kind>:<subtype>:<lang>:<file>:<name>`, `ext:<pkg>`, `var:<name>`) with
segment-index constants. The code comment admits the cost:
*"magic-string drift that caused issue #49"*. There's a documented case (#3936) where a pymongo
sort key `var:order` was mistakenly linked to an OpenAPI query parameter
named `order`, because the local stub was cross-resolved against the global name index.

## How we solve it

1. **We adopt the extractor → refs → resolver separation.** It's correct: the extractor doesn't
   need to know anything about the rest of the repo, which makes extraction parallelizable per file
   and cacheable by `content_hash`.
2. **We adopt the flat record** (`Ref` with a discriminator `Kind`) for the same reason they did.
3. **We adopt the dispositions taxonomy and `bug_rate` as a CI metric with a blocking
   regression gate.** This wasn't in the original plan and is a direct improvement.
4. **We fix the magic-string mistake:** the unresolved target is a **typed
   struct**, not a parseable string:

   ```go
   type RefTarget struct {
       Scope   TargetScope // Local | SameFile | Imported | Qualified | External | Dynamic
       Module  string      // empty if not applicable
       Name    string
       Member  string
       Lang    Lang
       File    string      // origin, in slash-normalized form
   }
   ```
   A target with `Scope == Local` **cannot** query the global index by construction of the
   type — bug #3936 is impossible to write, not something you have to remember not to do.
5. **Path normalization at the boundary:** every internal identifier uses slash form.
   `filepath.FromSlash` is only called when touching disk. (They arrived at this via a
   Windows bug; we get it for free.)

## Why different

The dispositions taxonomy is their best idea and we adopt it wholesale. String transport
is their worst structural decision and we type it. The result is the same auditability with
an entire class of bugs eliminated by the type system.
