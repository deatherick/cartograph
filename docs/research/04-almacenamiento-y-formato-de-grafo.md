# 04 — Almacenamiento y formato del grafo

## Problema

Dónde vive el grafo entre invocaciones, y cuánto cuesta abrirlo. En una herramienta cuya
promesa es "responde en menos de 100ms", el costo de abrir el grafo *es* el producto.

## Cómo lo resolvió Grafel — una trayectoria de tres saltos

**Salto 1 (ADR-0006): en memoria + JSON en disco.** Rechazaron explícitamente Neo4j (JVM),
Memgraph (Windows), SQLite con esquema de property-graph (*"SQL no es un lenguaje de grafos y
la ganancia de almacenamiento sobre JSON es pequeña a nuestra escala"*) y DuckDB (analítico,
no traversal puntual). Razón central y correcta: **el único consumidor es un proceso**, así que
la razón principal de existir de una base de datos —acceso multi-proceso compartido— no aplica.

**Salto 2 (ADR-0016): FlatBuffers.** El JSON no aguantó. Números medidos sobre un fixture real
de 11.34 MB / 100k+ entidades:

| Operación | Tiempo | Allocs |
|---|---|---|
| `json.Unmarshal(graph.json)` | ~132 ms | 50 MB / 640k allocs |
| `fbreader.Open` (mmap, zero-copy) | **~1.6 ms** | 9.9 MB / **8 allocs** |
| Lookup caliente por ID (binary search) | **~40 ns** | — |

**~80× más rápido en cold open.** Ese costo de 132ms era el piso de *cada* llamada MCP.

Y el desengaño honesto: esperaban 3× menos tamaño en disco y obtuvieron **1.15×**. La razón
está escrita en el ADR: *"el costo dominante es el contenido de los strings (IDs de entidad,
qualified names, rutas) que FlatBuffers no comprime; solo se elimina el envoltorio JSON"*.

**Salto 3 (ADR-0026/0027): el precipicio de 2 GiB y mmap.** El builder de FlatBuffers en Go
entra en panic con `cannot grow buffer beyond 2 gigabytes`. Diseñaron sharding… y luego
**lo difirieron** tras medir el corpus real:

> 287,091 entidades · 1,335,957 relaciones · ~2.1M LOC → `graph.fb` de **0.404 GiB**.
> Densidad ~325 bytes/relación. Llegar a 2 GiB requeriría ~6.6M relaciones (~5× el corpus).

La estimación original sobre-contaba el costo por registro entre 5× y 13×. Escribieron el
diseño de sharding completo antes de medir.

## Las dos debilidades que dejaron abiertas

1. **Las aristas referencian entidades por string ID, no por índice de vector.** Consecuencia
   escrita en su propio ADR: `IterateRelationshipsFromID` es **O(R)** — un escaneo lineal de
   todo el vector de relaciones para encontrar los vecinos de un nodo. Está marcado como
   "phase-2 debería añadir un vector paralelo ordenado por from_id".
2. **Los string IDs son el driver dominante del tamaño**, y se repiten en cada arista.

Las dos tienen la misma causa: identidad por string en la representación en disco.

## Cómo lo resolvemos nosotros

El plan original decía "SQLite + snapshot con `encoding/gob`". Los números de arriba lo
invalidan: `gob` tiene el mismo problema que JSON (decode O(N), allocs masivas). Corregimos.

**Formato del snapshot: binario propio, mmap-able, con IDs enteros y adyacencia CSR.**

```
[header: magic, version, counts, offsets]
[string table]        strings internados, deduplicados, una sola vez
[entities]            registros de tamaño fijo; los strings son offsets uint32
[csr_offsets]         uint32[n_entities+1]      índice de fila
[csr_targets]         uint32[n_edges]           vecino, por ÍNDICE de entidad
[csr_edge_meta]       kind/confidence/provenance por arista
[id_index]            ordenado por EntityID → índice de vector, para binary search
```

Lo que esto compra frente a su diseño:
- **Vecinos en O(1)**, no O(R): `targets[offsets[i] : offsets[i+1]]`. Es exactamente la
  debilidad que ellos documentaron y no cerraron. Para un Context Compiler que hace propagación
  por el grafo en cada llamada, O(R) por salto es inaceptable.
- **Tamaño**: los string IDs aparecen **una vez** en la tabla de strings, no en cada arista.
  Ataca directamente el driver de tamaño que ellos identificaron y no pudieron atacar.
- **Sin `flatc`, sin codegen, sin bindings verbosos**, y sin el precipicio de 2 GiB del builder
  de FlatBuffers — escribimos el buffer nosotros, en streaming, sin un builder que duplique.
- Mismo zero-copy por mmap, misma velocidad de apertura.

**SQLite sí, pero para otra cosa.** El grafo no vive en SQLite — ellos tienen razón en que SQL
no es un lenguaje de grafos. SQLite guarda lo que sí es tabular y mutable: proyectos, repos,
archivos y sus hashes, decisiones humanas sobre duplicados, relaciones aprendidas, el ledger de
sesiones y las métricas. Eso es exactamente donde SQLite es mejor que un archivo binario:
escrituras pequeñas, transaccionales, consultables. Y FTS5 nos da la búsqueda de texto gratis.

**Sidecar para embeddings** (cuando lleguen, Fase 8): igual que ellos, el vector no va inline en
la entidad sino en un archivo aparte referenciado por content-hash. Ya validaron que inline
infla el artefacto principal.

**Atributos pre-cocinados en tiempo de índice (ADR-0005, adoptado).** Centralidad, comunidad y
PageRank se calculan al indexar y se guardan como atributos del nodo. El compilador los lee
como campos, nunca los recalcula. Son términos directos del ranker, así que esto no es un extra:
es lo que hace que el ranking sea gratis en query time.

**La regla que nos llevamos de su ADR-0026 diferido:** medir antes de diseñar para escala.
El snapshot en formato simple debe existir y estar medido antes de optimizar nada.
Su corpus real (287k entidades / 1.3M relaciones / 2.1M LOC) es una referencia excelente de
qué tamaño tiene "grande de verdad": mucho menos de lo que uno teme.
