# 07 — Identidad de entidades, taxonomía y cross-repo

## A. Identidad y namespacing (ADR-0009)

### Problema
Una función `formatTime` existe en el repo móvil y en el frontend. Son distintas y reales, y no
pueden compartir identidad en el grafo.

### Cómo lo resolvió Grafel — dos capas
1. **Capa de índice (por repo)**: el `graph.json` de cada repo guarda `entity.id` como ID
   **local** (hash de archivo + línea + nombre dentro de ese repo). **No** se prefijan en tiempo
   de índice. Cada entidad lleva además un atributo `repo`.
2. **Capa de composición (MCP)**: al servir vistas cross-repo, los IDs se prefijan como
   `<repo>::<localId>`. Con `repo_filter` de un solo repo, van sin prefijo.
3. **Archivo de links cross-repo**: **siempre** usa IDs prefijados en ambos extremos, así cada
   entrada es auto-descriptiva.

La consecuencia buscada: `index` es repo-local, sin estado global. Un watcher puede reconstruir
el grafo de un repo sin tocar los demás. Y el hash determinista da IDs estables entre reindexados,
así que un agente que cacheó un ID lo sigue pudiendo usar.

Rechazaron: IDs globalmente únicos en tiempo de índice (empuja el problema al indexer e impide
rebuilds independientes), prefijo en tiempo de índice (archivos más grandes, renombrar el repo
cambia todos los IDs) y match por tupla `(qualified_name, file, line)` (frágil ante refactors).

### Lo que dice el ADR contra lo que hace el código
El ADR-0009 dice que el ID local es *"un hash de archivo fuente + línea + nombre"*. **El código
no incluye la línea:**

```go
func EntityID(repo, kind, name, sourceFile string) string   // internal/graph/graph.go:259
```

El ADR quedó desactualizado. Excluir la línea es correcto —si no, mover una función veinte
líneas arriba cambiaría su ID— pero tiene una consecuencia que ellos documentan en el test
#6161 y que hay que resolver de frente:

> *"todo constructo que declara un nombre dos veces en un archivo colisiona por construcción"*:
> sobrecargas de método en Java, clases y métodos `partial` de C#/VB, declaraciones de
> sobrecarga de C++/TypeScript, `@overload` / `@singledispatch` / `def` bajo `if TYPE_CHECKING`
> en Python, clases reabiertas de Ruby.

El bug real: `convertExtractedRecords` añadía la entidad de cada registro sin condición, así que
dos registros con el mismo `EntityID` producían **dos filas** en el documento. La mitad de las
relaciones ya tenía guarda de deduplicación; la de entidades no.

Detalle de implementación que sí vale copiar: los campos se hashean **separados por un byte NUL**,
para que `("ab","c")` y `("a","bc")` no colisionen. Y dejan anotado que `(from, to, kind)` **no**
es clave única de una arista: hay productores que acuñan IDs distintos para aristas que comparten
la terna.

### Cómo lo resolvemos nosotros
Adoptamos las dos capas de namespacing tal cual: son correctas y compran rebuilds independientes
por repo. Y vamos un paso más allá en la identidad, **sacando también el archivo**:

```
EntityID  = hash(repo, kind, qualified_name, disambiguator)
Anchor    = { file, byte_range, content_hash, commit }   se re-ancla al reindexar
```

- **Sin línea y sin archivo** en la identidad: mover una función *entre archivos* dentro del
  mismo namespace no rompe las referencias. Con su esquema sí las rompe, porque `sourceFile`
  entra al hash.
- **`disambiguator` es obligatorio para kinds sobrecargables** (métodos, funciones): un hash de
  la aridad y los tipos de los parámetros normalizados. Esto ataca de raíz la colisión del #6161
  en vez de parchearla con una guarda de deduplicación aguas abajo. Para kinds no sobrecargables
  va vacío.
- **Guarda de deduplicación de todos modos**, en la frontera de conversión a documento, con un
  test que falla si aparecen dos entidades con el mismo ID. Cinturón y tirantes: el
  disambiguator cubre el caso conocido, la guarda cubre el que no anticipamos.
- **Separador NUL** en el hash, copiado tal cual.
- **Las aristas llevan ID propio**, no se identifican por `(src, dst, kind)`.

Esta es la base de que los handles del Context Ledger (`E7`) sigan siendo válidos entre llamadas
mientras el usuario edita. Con identidad que incluye el archivo, un refactor que mueve código
invalida los handles y el ledger deja de servir.

## B. Taxonomía de entidades (ADR-0003)

### Cómo lo resolvió Grafel
Una jerarquía namespaceada `SCOPE.*` con tres capas conceptuales: runtime (funciones, clases),
framework (controllers, rutas, colas, hooks, JSX) e infraestructura (recursos de IaC).
Kinds: `SCOPE.Operation`, `SCOPE.Component`, `SCOPE.Schema`, `SCOPE.Endpoint`, `SCOPE.Queue`,
`SCOPE.Datastore`, `SCOPE.InfraResource`, etc. Enum **cerrado** de kinds de aristas.

Detalle bueno: **la capa de render del MCP quita el prefijo `SCOPE.`** antes de mostrárselo al
agente. Internamente `SCOPE.Operation`, para el agente `Operation`. El almacenamiento mantiene
la forma namespaceada para que futuros namespaces coexistan sin colisión.

Costo que admiten: los extractores tienen que ponerse de acuerdo en dónde cae cada construcción
(¿un trait de Rust es `Schema` o `Pattern`?), y el enum cerrado hace que añadir un kind de arista
sea un cambio breaking.

### Cómo lo resolvemos nosotros
Su taxonomía está optimizada para **navegar** el grafo. La nuestra está optimizada para
**compilar contexto**, así que difiere a propósito:

- V0 usa kinds concretos y familiares (`Class`, `Interface`, `Function`, `Method`, `Property`)
  en vez de abstracciones de tres capas. Para una cápsula que un humano y un LLM van a leer,
  `Class` comunica más que `SCOPE.Component`, y no hay que traducir en el render.
- Nos llevamos el principio del render: **el vocabulario interno y el vocabulario del agente son
  decisiones separadas**. Lo que se serializa y lo que se muestra no tienen por qué coincidir.
- Los kinds de framework/infra llegan en fases posteriores, cuando haya extractores que
  justifiquen cada uno. No se declaran 30 kinds en V0 para tener 8 poblados: un kind sin
  extractor es una promesa que la UI incumple.
- El enum de aristas también es cerrado, con versionado de esquema. Ahí tienen razón.

## C. Cross-repo (ADR-0007)

Su puente cross-repo se apoya en documentación y en un archivo de links con confianza reducida
(0.7 cross-repo contra 0.95 intra-repo). Adoptamos el principio — **una arista cross-repo nunca
tiene la misma confianza que una resuelta estáticamente dentro del repo** — y el archivo de
overlay separado, que permite reconstruir un repo sin recalcular los links de los demás.

Lo que añadimos: la confianza no es solo un número guardado, es un campo que la cápsula **muestra**
y por el que el ranker penaliza. Una arista cross-repo inferida vale menos presupuesto que una
determinística.
