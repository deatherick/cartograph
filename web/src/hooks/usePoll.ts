// Runs fn immediately and then every intervalMs — the "real-time" feel
// for multi-project ctxd (ADR-0019): a snapshot's entity/edge counts and
// operations status change out from under the browser whenever ctxd's
// watcher reindexes, and there is no push channel (WebSocket/SSE) yet, so
// polling is the pragmatic way to reflect that without a manual reload.
// Not a general-purpose data-fetching hook — no caching, no dedup, no
// retry/backoff — just the one thing every poller here needs: re-run fn
// on an interval and stop cleanly on unmount or when deps change.
import { useEffect, useRef } from 'react'

export function usePoll(fn: () => void, deps: readonly unknown[], intervalMs = 3000) {
  const fnRef = useRef(fn)
  fnRef.current = fn

  useEffect(() => {
    fnRef.current()
    const id = setInterval(() => fnRef.current(), intervalMs)
    return () => clearInterval(id)
    // deps is the caller's own dependency list (e.g. [project]) — fn is
    // read through fnRef above so it's never stale, but the interval must
    // still restart when the caller's real inputs change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)
}
