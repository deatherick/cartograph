# Baseline — Phase 0b — synthetic fixture `ts-basic`

- **Date**: 2026-08-29
- **Fixture**: `fixtures/ts-basic` (15 files, 407 lines, our own, vendored in the repo)
- **Task set**: `fixtures/tasks/ts-basic.json` (12 tasks)
- **Command**: `./bin/ctxbench --baseline`
- **Capsule tokens**: N/A — the Context Compiler doesn't exist yet (arrives in Phase 2)

## Result

| Task | Gold files | Oracle tokens | Traced tokens | char/4 ratio |
|---|---:|---:|---:|---:|
| T01 | 3 | 567 | 1152 | 1.08 |
| T02 | 3 | 615 | 1213 | 1.11 |
| T03 | 4 | 865 | 950 | 1.08 |
| T04 | 1 | 148 | 287 | 0.93 |
| T05 | 5 | 1136 | 1659 | 1.06 |
| T06 | 3 | 524 | 608 | 1.10 |
| T07 | 3 | 718 | 878 | 1.05 |
| T08 | 3 | 740 | 806 | 1.17 |
| T09 | 1 | 300 | 339 | 1.15 |
| T10 | 4 | 647 | 878 | 1.07 |
| T11 | 4 | 926 | 986 | 1.07 |
| T12 | 2 | 610 | 813 | 1.09 |

**Total oracle: 7,796 · Total traced: 10,569.**

## Reading

This is the control fixture: small, our own, without real-world noise (no lockfiles, no
`node_modules`, no binaries). It serves to detect regressions in the harness itself — if this
number changes without the files or tasks changing, something broke in `ctxbench` itself,
not in the project it measures.
