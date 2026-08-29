# Baseline — Fase 0b — fixture sintético `ts-basic`

- **Fecha**: 2026-08-29
- **Fixture**: `fixtures/ts-basic` (15 archivos, 407 líneas, propio, vendorizado en el repo)
- **Task set**: `fixtures/tasks/ts-basic.json` (12 tareas)
- **Comando**: `./bin/ctxbench --baseline`
- **Capsule tokens**: N/A — el Context Compiler no existe todavía (llega en Fase 2)

## Resultado

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

**Total oráculo: 7,796 · Total trazado: 10,569.**

## Lectura

Este es el fixture de control: pequeño, propio, sin ruido real (sin lockfiles, sin
`node_modules`, sin binarios). Sirve para detectar regresiones del propio harness — si este
número cambia sin que cambien los archivos o las tareas, algo se rompió en `ctxbench` mismo,
no en el proyecto que mide.
