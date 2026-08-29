# 01 — Parser y binding de tree-sitter

## Problema

Elegir un binding de tree-sitter para Go y decidir cómo los extractores consumen el árbol.
La decisión parece menor y resulta ser la más cara de revertir.

## Cómo lo resolvió Grafel

- Arrancó con `smacker/go-tree-sitter`. Ese binding **está muerto**: el commit que tenían
  pinneado (2024-08-27) *es* el HEAD de upstream, `ahead_by: 0`. Sin gramáticas frescas y
  sin forma de automatizar el bump.
- ADR-0023 documenta la migración al binding oficial `tree-sitter/go-tree-sitter` (v0.24.0,
  vivo), donde **cada gramática es su propio módulo Go** (`tree-sitter/tree-sitter-<lang>/bindings/go`),
  bumpeable de forma independiente por Renovate.
- El costo medido de esa migración: **245 archivos** importan el binding, **1,758** referencias
  a `sitter.Node`, **102** call sites de `GetLanguage()`. Cambian nombres de métodos
  (`Type()`→`Kind()`, `StartPoint()`→`StartPosition()`, `Content()`→`Utf8Text()`) y tipos
  (`uint32`→`uint`).
- **Cero uso del motor de queries de tree-sitter.** No hay un solo archivo `.scm` en el repo.
  Toda la extracción es traversal manual depth-first escrito a mano.
- Salvaguardas que sí acertaron:
  - Gate de **ratio de error de sintaxis del 10%**: si el árbol tiene más de 10% de nodos ERROR,
    el archivo se rechaza en vez de producir basura.
  - **Watchdog por parse** (`GRAFEL_PARSE_TIMEOUT`): tree-sitter puede colgarse en archivos
    patológicos (minificados, generados, líneas de 2MB).
  - El árbol se cierra explícitamente **también en el camino de error** — no hacerlo filtraba
    heap de C durante toda la vida del proceso.
  - Un parser independiente por llamada, con concurrencia acotada. El mutex global que tenían
    era un workaround de un race de estado compartido en smacker, no una restricción real.

## El costo real de la traversal manual

| Extractor | Archivos (sin tests) | Líneas |
|---|---:|---:|
| JavaScript/TypeScript | 39 | **21,128** |
| Python | 38 | **14,188** |
| Go | 14 | 6,239 |
| C# | 6 | 2,510 |

21k líneas de Go escritas a mano para extraer estructura de TS/JS, y el binding filtrado
a 245 archivos.

## Cómo lo resolvemos nosotros

1. **Binding oficial desde el día 1**: `github.com/tree-sitter/go-tree-sitter` + un módulo de
   gramática por lenguaje. No pagamos la migración que ellos pagaron.
2. **El binding NUNCA sale de `internal/parser`.** Ningún tipo `sitter.*` aparece en la firma
   de nada fuera de ese paquete. Un test de arquitectura (grep sobre imports) lo hace cumplir.
   Este es el punto entero: su migración costó 245 archivos porque el tipo se filtró.
3. **Extracción declarativa con queries `.scm`**, no traversal manual. Un lenguaje nuevo es
   un archivo de queries + un mapeo de capturas a entidades, no 900 líneas de Go.
   La traversal manual queda reservada para lo que las queries no expresan (resolución de
   alcance, tablas de imports).
4. Heredamos las tres salvaguardas tal cual: gate de error ratio 10%, timeout por parse,
   cierre del árbol en todos los caminos.

## Por qué distinto

El 80% estructural (clases, funciones, métodos, imports, llamadas, herencia) es exactamente
lo que el motor de queries de tree-sitter hace bien y de forma declarativa. Grafel no lo usó
y terminó con 21k líneas por lenguaje. Su ventaja es que la traversal manual sí captura
patrones de framework (hooks de React, rutas de Express) que una query sola no ve — por eso
nuestro diseño deja una capa de patrones encima de las queries, en lugar de sustituir una
por otra.

**Riesgo aceptado:** si las queries resultan insuficientes para C#, se cae a traversal manual
solo para ese lenguaje. La frontera `internal/parser` hace que esa decisión sea local.
