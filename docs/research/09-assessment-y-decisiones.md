# 09 — Assessment de reuso y decisiones derivadas

Entregable pedido por §51 del prompt maestro. Grafel es MIT y **no se copia código**: se
reimplementa el conocimiento. Ninguna dependencia a `github.com/cajasmota/grafel`.

## 1. Qué adoptamos conceptualmente casi tal cual

| Idea | Origen | Por qué |
|---|---|---|
| Taxonomía de **dispositions** + `bug_rate` como métrica | ADR-0015, `internal/resolve/refs.go` | Convierte calidad del grafo en un número auditable y separa "irresoluble por diseño" de "bug nuestro" |
| Allowlist de bare names + lista de exclusión de nombres genéricos | ADR-0011 | Es la mayor fuente de falsos positivos; su política está validada contra corpus |
| Tabla de imports por archivo, resuelta antes que bare names | ADR-0013 | La señal de scoping más alta sin inferencia de tipos |
| Tracking del tipo estático del receiver + regla del candidato único | ADR-0012 | Rentable en TS y C#; nunca adivina |
| Separación extractor → refs → resolver | ADR-0010 | Hace la extracción paralelizable y cacheable por archivo |
| Pre-cocinar centralidad/comunidad/PageRank al indexar | ADR-0005 | Query time es lectura de atributo; son términos directos de nuestro ranker |
| Handoff por archivo atómico + mmap + revalidación por mtime | ADR-0024 | Desacopla lector y escritor sin locks ni notificaciones |
| Exclusiones en 3 capas: skip estático + `.gitignore` + cuarentena adaptativa | `internal/daemon/watch/quarantine.go` | Defensa contra bucles de reindex por directorios de build |
| Identidad de 2 capas: ID local por repo, prefijo `<repo>::` en composición | ADR-0009 | Permite rebuild independiente por repo, sin estado global |
| Reglas de seguridad del bucle de reparación (allowlist de resoluciones, `source` por edge, reversibilidad, determinismo) | ADR-0015 | Buenas y gratis |
| Gate de error ratio 10%, timeout por parse, cierre del árbol en todos los caminos | `internal/treesitter/parser.go` | Salvaguardas contra archivos patológicos y fugas de heap de C |

## 2. Qué adaptamos con cambios deliberados

| Tema | Ellos | Nosotros | Razón |
|---|---|---|---|
| Binding tree-sitter | Empezaron con `smacker` (muerto), migran al oficial: 245 archivos, 1,758 refs a `Node` | Binding oficial desde el día 1, **encapsulado en `internal/parser`** con test de arquitectura | No pagar una migración que ya está documentada |
| Extracción | Traversal manual: 21k líneas para TS/JS, 14k para Python. **Cero archivos `.scm`** | Queries `.scm` declarativas para el 80% estructural, traversal solo para scoping | Un lenguaje nuevo debería ser un archivo de queries |
| Transporte de refs sin resolver | Strings con gramática mágica (`scope:k:sub:lang:file:name`), con drift documentado (#49) y un bug de cross-resolución (#3936) | Struct tipado con `Scope` enumerado | Hace el bug #3936 imposible de escribir |
| Identidad de entidad | Hash de **archivo + línea** + nombre | Hash de `(repo, qualified_name, kind)`; ubicación en un `Anchor` mutable | Con su esquema, mover una función invalida su ID y rompe los handles del ledger |
| Formato en disco | JSON → FlatBuffers; aristas por **string ID** → vecinos en **O(R)** | Binario propio mmap-able con IDs enteros y **adyacencia CSR** → vecinos en O(1) | Cierra la debilidad que su propio ADR-0016 deja abierta, y ataca el driver de tamaño (strings repetidos) |
| Persistencia tabular | Rechazaron SQLite para el grafo | SQLite **no** para el grafo, sí para proyectos, decisiones, ledger, métricas y FTS5 | Tienen razón en el grafo; SQLite gana donde hay escrituras pequeñas y transaccionales |
| Watcher en macOS | fsnotify/kqueue + presupuesto de descriptores (1 fd por archivo: 40k fds para un repo) | **FSEvents** en darwin: un stream recursivo, cero fds por archivo | Eliminar el problema en vez de administrar la escasez, en nuestra plataforma primaria |
| Benchmark de tokens | Solo tokens, estimador `len/4`, baseline "leer los archivos correctos" | Tokens **+ recall@gold + precision@gold**, tokenizer real, baseline por traza | Sin recall, ahorrar tokens es trivial devolviendo menos |
| Taxonomía | `SCOPE.*` de 3 capas (runtime/framework/infra), ~20 kinds | Kinds concretos en V0 (`Class`, `Function`…); framework/infra cuando haya extractor | Una cápsula la leen humano y LLM; y un kind sin extractor es una promesa incumplida |

## 3. Qué descartamos

| Qué | Por qué |
|---|---|
| **Split de procesos serve/engine** | Solución a incidentes de escala que no tenemos. Adoptamos el patrón de handoff que lo hace barato después |
| **Sharding del writer** | Ellos mismos lo **difirieron tras medir**: su corpus de 287k entidades / 1.3M relaciones da 0.4 GiB, 5× de margen. Escribieron el diseño antes de medir; nosotros no escribimos ninguno |
| **11,333 líneas de síntesis de paquetes externos** | La cola larga de frameworks perseguida a mano. Apostamos a cubrirla con el bucle de residuals + desambiguación en la cápsula |
| **50+ lenguajes, 263 frameworks** | V0 son tres lenguajes hechos bien |
| **FlatBuffers y su codegen `flatc`** | Nos trae el precipicio de 2 GiB del builder, bindings verbosos y un paso de codegen, y no resuelve ni el tamaño ni el O(R) de aristas |
| **Estimador de tokens `len/4`** | Error no acotado sobre código |
| **Enum `SCOPE.*` cerrado en V0** | Añadir un kind de arista es breaking; no fijamos el vocabulario antes de tener los extractores |

## 4. Implicaciones de licencia

- Grafel es **MIT**. Copiar sería legal con atribución; **no copiamos**, así que no se genera
  obra derivada.
- `~/code/_ref/grafel` es una referencia de lectura fuera del repo del proyecto. Nunca submódulo,
  nunca dependencia, nunca vendorizado.
- `NOTICE.md` acredita únicamente dependencias OSS reales (tree-sitter y sus gramáticas, fsnotify
  o fsevents, SQLite, etc.).
- Si alguna vez se adapta de cerca un algoritmo puntual, se marca en el archivo
  (`// adaptado de grafel (MIT), ver docs/research/...`) y se acredita. Debe ser excepción
  documentada. **Hoy no hay ninguna.**
- No reutilizamos nombres de tools MCP, nombres de archivos en disco (`.grafel/graph.fb`),
  el esquema `SCOPE.*` ni la gramática de stubs.

## 5. Cambios que este discovery introduce en el plan aprobado

1. **Snapshot**: `encoding/gob` queda descartado. Formato binario mmap-able con IDs enteros y
   adyacencia CSR. (Nota 04 — justificado con sus números medidos.)
2. **Watcher**: FSEvents en macOS, no fsnotify/kqueue. Modelo de costo por build tag.
   (Nota 05 — es un bloqueador real, no una optimización.)
3. **Extracción**: queries `.scm` declarativas, con el binding encapsulado y un test de
   arquitectura que lo hace cumplir. (Nota 01.)
4. **Nueva métrica de CI**: `bug_rate` de dispositions, junto a los tokens y el recall.
   Objetivo V1 informado por su medición real: **≤12%, aspirando a ≤8%**.
5. **Los residuals entran en la cápsula** como preguntas de desambiguación con candidatos, no
   solo como una lista de fallos. (Nota 08 — es diferenciación de producto, no una nota al pie.)
6. **La escalera de fuente gana un peldaño de evidencia**: cada ítem lleva `provenance` y
   `confidence` visibles, alimentados por la taxonomía de dispositions.
7. **Fase 1 añade el pipeline de resolución en orden fijo**
   (`same-file → import-table → receiver-type → bare-name → disposition`) como estructura
   explícita, no como algo que emerge.

## 6. Riesgo principal identificado

La apuesta más fuerte del plan es que **queries `.scm` declarativas cubren el 80% estructural**
donde Grafel escribió 21k líneas de traversal manual para TS/JS. Si esa apuesta falla, la Fase 1
se alarga significativamente.

**Mitigación:** la Fase 1 es un solo lenguaje precisamente para descubrirlo barato. El criterio
de salida (precisión de resolución de imports ≥95% sobre fixture anotado) es la señal. Si a
mitad de la Fase 1 las queries no alcanzan, se cae a traversal manual solo para TS y se
reevalúa antes de tocar C# y Python.
