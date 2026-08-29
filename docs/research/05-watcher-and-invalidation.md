# 05 — Watcher, exclusions and incremental invalidation

## Problem

Keeping the index fresh without burning the machine. Sounds like "just use fsnotify and be done."
It isn't.

## How Grafel solved it

### The macOS trap: file descriptors (the most expensive finding of this discovery)

`fsnotify` v1.10.1 **does not ship a FSEvents backend** — its `backend_kqueue.go` carries the build
tag `darwin`. And kqueue cannot watch a path without an open descriptor on it. Worse: for a
directory, fsnotify calls `watchDirectoryFiles()` and opens **one descriptor per file inside**.

Their live measurement on **a single repo**:

> 40,079 descriptors — 32,073 regular files + 7,995 directories.
> The per-file term was **4× the per-directory term**.
> Against `kern.maxfilesperproc = 61,440`, that's **65% of the process ceiling for ONE repo**.

Their previous cap counted only directories, so it was blind to the term that dominates: at
100,000 directories the old cap was effectively infinite.

On Linux, inotify spends one watch descriptor per directory out of `max_user_watches`, and nothing
per file. On Windows, `ReadDirectoryChangesW` differs again. The cost model is selected by
**build tag, never by a `runtime.GOOS` check** — so the constant is available to the compiler and
to build-tagged tests.

Their solution was a **descriptor budget** with a per-platform cost model
(`kqueue: perDir 1, perFile 1` / `inotify: perDir 1, perFile 0`), derived from the **effective**
limit (the `RLIMIT_NOFILE` read back after the kernel clamp, not the one requested — launchd asks
for 65536, the kernel silently lowers it to 61440), reserving **one quarter** of the limit for
everything that isn't watches (graph mmaps, unix socket, dashboard listener, logs, git subprocess
pipes), with a floor of 1024.

Their justification for why the budget sits on the watch side and not the store side:
*"running out of descriptors for the store is corruption risk; running out of watch budget only
costs freshness"*. Correct and not obvious.

### Exclusions in three layers

1. **Static skip list** — ~90% of known junk, cheap.
2. **`.gitignore`** — actually honored.
3. **Adaptive churn quarantine** — the layer I wouldn't have thought of. A non-gitignored build
   directory that churns pathologically triggers a continuous reindex loop (their "incident class
   #5392"). Mechanism:

   ```
   Observe → Detect → Quarantine → (persist) → Auto-heal
   ```

   - Every event that survives the filters is attributed to its **directory**, with a
     sliding-window churn count.
   - On crossing the threshold, the directory is quarantined: events under it are dropped at the
     event boundary and the decision is written to an audit log.
   - The set is persisted to disk, so a build loop that quarantined itself **does not thrash
     again after the daemon restarts**.
   - `Sweep()` periodically re-evaluates and un-quarantines what has been quiet for a while.

   Explicit safety invariant: *never quarantine a legitimately active source directory*. They
   guarantee this with a **sustained** churn threshold (a human burst of saves stays well below
   it; only a mechanical loop of dozens or hundreds of writes within the window triggers it) and
   hysteresis (`HealQuiet` >> `ChurnWindow`) to avoid oscillation.

### Other details earned the hard way

- **`.git/HEAD` poller** to detect a branch change, instead of trying to infer it from file
  events.
- **Injectable clock** across the whole debounce/coalesce path, so timing tests are deterministic
  and don't depend on the CI scheduler.
- **Reconcile / catch-up** at startup: the daemon was down, the tree changed.

## How we solve it

1. **On macOS we don't use kqueue.** We use **FSEvents** directly (`fsnotify/fsevents` or
   equivalent), which watches a whole tree recursively with **a single stream and zero
   per-file descriptors**. That's the root fix for the problem they managed with a budget. They
   manage scarcity; we eliminate it on the platform where it hurts. macOS is our primary
   platform, so this isn't optional.
   - Cost: FSEvents gives per-directory events with coarser granularity and its own coalescing,
     so you have to re-stat the directory to know what changed. In exchange, the cost is constant
     in the number of files.
   - On Linux we keep inotify (the per-file term is zero, no problem there) and keep the
     per-directory cap.
   - The cost model is selected **by build tag**, just as they learned.
2. **We adopt all three exclusion layers**, including adaptive quarantine with persistence and
   auto-healing. It's defense against a real incident class we would otherwise discover in
   production.
3. **We adopt the `.git/HEAD` poller** and the injectable clock.
4. **Invalidation is by `content_hash`, not by mtime.** A `git checkout` touches the mtime of
   hundreds of files whose content is once again identical to what we already indexed. If an
   entity body's hash didn't change, its byte range is re-anchored and **nothing upstream is
   invalidated**. This is what makes the "400-file checkout in under 10s" target viable, and it's
   where our anchors model pays off.
5. **We reserve descriptors for the store anyway**, with their same reasoning: freshness is
   expendable, store integrity is not.
