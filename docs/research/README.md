# Research — discovery sobre Grafel (Fase 0a)

Grafel (`cajasmota/grafel`, Go, MIT) se clonó en `~/code/_ref/grafel` y se leyó a fondo para no
volver a resolver los problemas que ya resolvió. **No se copió código.** Estas notas registran,
por tema: cuál es el problema, cómo lo resolvieron, cómo lo resolvemos nosotros y por qué distinto.

Escala del repo estudiado: 9,063 archivos, 27 ADRs, ~21k líneas solo en el extractor de TS/JS.

## Notas

| Nota | Tema | Hallazgo principal |
|---|---|---|
| [01](01-parser-y-binding-treesitter.md) | Parser y binding de tree-sitter | El binding `smacker` está muerto; usar el oficial desde el día 1. Ellos tienen **cero archivos `.scm`** y 21k líneas de traversal manual para TS/JS, con el binding filtrado a 245 archivos |
| [02](02-refs-y-dispositions.md) | Refs y dispositions | Su mejor idea: taxonomía de dispositions + `bug_rate = (bug-extractor + bug-resolver) / total` como métrica auditable. Su peor decisión: transportar refs como strings con gramática mágica |
| [03](03-resolucion-imports-y-bare-names.md) | Resolución | Bare names = mayor fuente de falsos positivos. Allowlist + exclusión de nombres genéricos + tabla de imports por archivo + tipo estático del receiver, en orden fijo |
| [04](04-almacenamiento-y-formato-de-grafo.md) | Almacenamiento | JSON→FlatBuffers les dio **80×** en cold open (132ms→1.6ms). Dejaron abierto: aristas por string ID ⇒ vecinos en **O(R)**. Lo cerramos con IDs enteros + CSR |
| [05](05-watcher-e-invalidacion.md) | Watcher | **macOS/kqueue gasta 1 descriptor por archivo**: 40,079 fds para un repo, 65% del techo del proceso. Usamos FSEvents. Más: exclusiones en 3 capas con cuarentena adaptativa |
| [06](06-medicion-de-tokens.md) | Medición de tokens | Su benchmark mide tokens y **no mide corrección**. Siempre se ahorran tokens devolviendo menos |
| [07](07-identidad-taxonomia-y-cross-repo.md) | Identidad y taxonomía | Su ADR contradice su código en el `EntityID`. Excluir la línea es correcto y hace colisionar sobrecargas y `partial` por construcción |
| [08](08-arquitectura-de-proceso-y-residuals.md) | Proceso y residuals | Handoff escritor/lector por archivo atómico + mmap + mtime, sin locks. Y el dato de calibración más útil: **su bug-rate real es 8–12%** |
| [09](09-assessment-y-decisiones.md) | **Assessment** | Qué adoptamos, qué adaptamos, qué descartamos, licencia, y los 7 cambios que esto introduce en el plan |
| [backlog](backlog-casos-borde.md) | Casos borde | **80 casos** derivados de sus bugs reales, listos para ser fixtures |

## Los cinco hallazgos que cambian el plan

1. **macOS/kqueue agota descriptores** (nota 05) — bloqueador real en nuestra plataforma primaria,
   no una optimización. FSEvents en darwin.
2. **`encoding/gob` estaba mal elegido** (nota 04) — sus números demuestran que cualquier formato
   con decode O(N) es el piso de latencia de *cada* llamada. Snapshot binario mmap-able con
   adyacencia CSR, que además cierra el O(R) que ellos dejaron abierto.
3. **Cero uso de queries de tree-sitter** (nota 01) — 21k líneas de traversal manual para TS/JS.
   Nuestra apuesta principal, y el riesgo principal del plan.
4. **La taxonomía de dispositions y el `bug_rate`** (nota 02) — métrica de calidad del grafo que
   no estaba en el plan y que se vuelve gate de CI. Calibración: 8–12% es lo normal.
5. **Su benchmark de tokens no mide corrección** (nota 06) — confirma que `recall@gold` junto al
   ahorro no es un detalle metodológico, es lo que separa una medición honesta de una inútil.

## Regla de uso

`~/code/_ref/grafel` es referencia de lectura, fuera del repo del proyecto. Nunca submódulo,
nunca dependencia, nunca vendorizado. Ver [09](09-assessment-y-decisiones.md) §4 para licencia.
