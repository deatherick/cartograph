# Baseline — Fase 0b — repo real `typescript-node-express-realworld-example-app`

- **Fecha**: 2026-08-29
- **Fixture**: implementación TypeScript/Express/Mongoose del spec RealWorld
  (github.com/skopekreep/typescript-node-express-realworld-example-app), 20 archivos TS,
  1,174 líneas. Clonado en `~/code/_ref/realworld-ts`, **no vendorizado** en este repo (mismo
  tratamiento que Grafel: referencia externa de solo lectura).
- **Task set**: `fixtures/tasks/realworld-ts.json` (12 tareas, autoría propia sobre el código
  real leído — no generadas mecánicamente)
- **Comando**: `./bin/ctxbench --baseline --fixtures-root ~/code/_ref --tasks fixtures/tasks/realworld-ts.json`
- **Capsule tokens**: N/A — el Context Compiler no existe todavía (llega en Fase 2)

## Resultado

| Task | Gold files | Oracle tokens | Traced tokens | char/4 ratio |
|---|---:|---:|---:|---:|
| R01 | 3 | 3204 | 3616 | 1.07 |
| R02 | 3 | 3493 | 4879 | 1.06 |
| R03 | 3 | 1555 | 2322 | 1.02 |
| R04 | 3 | 3139 | 3439 | 1.07 |
| R05 | 3 | 2610 | 4013 | 1.09 |
| R06 | 2 | 1220 | 2558 | 1.00 |
| R07 | 1 | 1946 | 2026 | 1.11 |
| R08 | 2 | 1682 | 1787 | 1.01 |
| R09 | 2 | 1193 | 1647 | 1.01 |
| R10 | 3 | 3120 | 4156 | 1.09 |
| R11 | 3 | 2688 | 2892 | 1.09 |
| R12 | 1 | 548 | 671 | 1.02 |

**Total oráculo: 26,398 · Total trazado: 34,006.**

## Hallazgo que produjo esta corrida: exclusiones reales

Clonar un repo real (no un fixture escrito a mano) expuso de inmediato un hueco: el working
tree de este repo incluye `conduit/`, un directorio de datos de MongoDB con páginas binarias
`.wt` — el `filepath.Walk` ingenuo de `ctxbench` las leía como texto. Se creó
`internal/exclude` (directorios de dependencias/build, lockfiles, y detección de binario por
byte NUL) **antes** de correr el baseline; sin eso el número de arriba estaría inflado con
basura binaria y no significaría nada. Ver `internal/exclude/exclude_test.go` para el caso de
regresión que fija este hallazgo.

## Lectura

~3.4× más tokens que el fixture sintético en el baseline trazado (34,006 vs 10,569) con el
mismo número de tareas (12) — el código real tiene más superficie por archivo y las tareas
tocan módulos más grandes. El ratio traced/oracle (1.29×) es más bajo que en el fixture
sintético (1.36×): en código real los `grep_steps` autorados tienden a converger más rápido
porque los nombres son más específicos (`toProfileJSONFor`, `generateJWT`) que en un fixture
pequeño donde varias tareas comparten vocabulario genérico.

Este número —no el del fixture sintético— es el que más importa como termómetro del proyecto:
mide contra código que nadie escribió pensando en este benchmark.
