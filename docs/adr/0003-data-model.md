# ADR-0003: Modelo de datos V0 — identidad, dispositions y almacenamiento

- **Status**: Accepted
- **Date**: 2026-08-29
- **Related**: docs/research/04, 07, 09

## Contexto

El modelo de datos determina la corrección del índice incremental (¿un edit invalida de más o
de menos?) y el costo de cada consulta del Context Compiler (¿cuánto cuesta encontrar los
vecinos de una entidad?). Grafel midió estos costos en producción; ver docs/research/04 y 07
para los números.

## Decisión

**Identidad de entidad:** `EntityID = hash(repo, kind, qualified_name, disambiguator)`, sin
archivo ni línea. El `disambiguator` (aridad + tipos de parámetros normalizados) es obligatorio
en kinds sobrecargables, para que sobrecargas/`partial`/`@overload` no colisionen por
construcción. La ubicación vive en un `Anchor` separado y mutable
(`file, byte_range, content_hash, commit`), re-anclado al reindexar sin invalidar identidad.

**Dispositions:** toda referencia sin resolver cae en un bucket tipado
(`resolved | external-known | external-unknown | dynamic | ambiguous | bug-extractor |
bug-resolver`), nunca en un string parseable. `bug_rate = (bug-extractor + bug-resolver) / total`
es métrica de CI con regresión bloqueante.

**Almacenamiento:** el grafo vive en un snapshot binario propio, mmap-able, con IDs enteros y
adyacencia CSR (vecinos en O(1)), escrito atómicamente (temp + rename). SQLite se usa para lo
tabular y mutable (proyectos, repos, decisiones humanas, ledger de sesiones, FTS5), nunca para
el grafo en sí.

## Consecuencias

- Mover código entre archivos o líneas dentro del mismo namespace no invalida `EntityID` — los
  handles del Context Ledger sobreviven a ediciones normales.
- El snapshot evita tanto el costo de parseo O(N) de JSON/gob como la debilidad de vecinos O(R)
  que Grafel dejó documentada y sin cerrar en su propio formato binario.
- Costo: escribir el snapshot y su índice CSR es más código que serializar con `encoding/gob`;
  se acepta porque el costo se paga una vez en el escritor y el ahorro se cobra en cada lectura.
- El `disambiguator` añade una responsabilidad a cada extractor (normalizar tipos de parámetros
  de forma consistente); sin él, el caso de colisión por sobrecarga (documentado como bug real
  en Grafel, issue #6161) reaparece.

## Alternativas consideradas

- **`encoding/gob`** (plan original): descartado tras medir — mismo problema de decode O(N) que
  JSON, sin ninguna de las ventajas de mmap.
- **FlatBuffers**: descartado — trae codegen (`flatc`), bindings verbosos, el precipicio de
  2 GiB del builder en Go, y no resuelve el O(R) de vecinos por sí solo (Grafel lo dejó como
  trabajo futuro no hecho).
- **Grafo en SQLite con esquema de property-graph**: descartado — SQL no es un lenguaje de
  grafos y la ganancia de almacenamiento sobre un formato binario dedicado es pequeña a esta
  escala.
- **ID incluyendo archivo+línea** (como el ADR de Grafel describe, aunque su código no lo
  implementa así): descartado — invalida referencias cacheadas ante cualquier movimiento de
  código.
