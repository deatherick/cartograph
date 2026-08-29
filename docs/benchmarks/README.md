# Benchmarks

Cada archivo aquí es la salida congelada de `ctxbench --baseline` en un punto del proyecto,
para comparar fases entre sí sobre el mismo fixture y task set. Se regenera (nuevo archivo,
fechado) cada vez que una fase introduce el Context Compiler o cambia de forma significativa
el pipeline de indexado — nunca se sobreescribe uno viejo, así queda la serie histórica.

| Archivo | Fase | Fixture | Qué mide |
|---|---|---|---|
| [2026-08-29-fase0b-ts-basic.md](2026-08-29-fase0b-ts-basic.md) | 0b | sintético (`fixtures/ts-basic`) | Baseline inicial, repo de control propio |
| [2026-08-29-fase0b-realworld-ts.md](2026-08-29-fase0b-realworld-ts.md) | 0b | real (`typescript-node-express-realworld-example-app`, clonado en `~/code/_ref/realworld-ts`) | Baseline inicial contra código real, no sintético |

## Cómo reproducir

```bash
# fixture sintético (vendido en el repo)
./bin/ctxbench --baseline

# repo real (clonar antes de correr; no se vendoriza)
git clone --depth 1 \
  https://github.com/skopekreep/typescript-node-express-realworld-example-app \
  ~/code/_ref/realworld-ts
./bin/ctxbench --baseline --fixtures-root ~/code/_ref --tasks fixtures/tasks/realworld-ts.json
```
