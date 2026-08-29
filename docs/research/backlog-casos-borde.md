# Backlog de casos borde → fixtures y tests

Cada entrada es un caso real encontrado en el código, los tests o los ADRs de Grafel. La
referencia `#NNNN` es su número de issue, conservada como trazabilidad de dónde salió el caso.
**Ninguna entrada se toma de su código; se toma del problema que su código documenta.**

Columna `Fase`: cuándo debe existir el test.

## A. Identidad y deduplicación de entidades

| # | Caso | Origen | Fase |
|---|---|---|---|
| A1 | Dos métodos sobrecargados en el mismo archivo producen el mismo ID y dos filas de entidad | #6161 | 1 |
| A2 | Clase `partial` de C# declarada en dos archivos | #6161 | 3 |
| A3 | Método `partial` de C# (declaración + implementación) | #6161 | 3 |
| A4 | Declaraciones de sobrecarga de TypeScript (`function f(a:string):void; function f(a:number):void;`) seguidas de la implementación | #6161 | 1 |
| A5 | `@overload` y `@singledispatch` de Python | #6161 | 3 |
| A6 | `def` duplicado bajo `if TYPE_CHECKING:` | #6161 | 3 |
| A7 | Hash sin separador: `("ab","c")` no debe colisionar con `("a","bc")` | `graph.go:259` | 1 |
| A8 | Dos aristas con la misma terna `(src, dst, kind)` deben poder coexistir con IDs distintos | `graph.go:271` | 1 |
| A9 | Mover una función a otro archivo del mismo namespace **no** debe cambiar su `EntityID` | ADR-0009 vs código | 1 |
| A10 | Mover una función 20 líneas dentro del mismo archivo no cambia ID ni invalida aguas arriba | ADR-0009 | 1 |

## B. Resolución de símbolos

| # | Caso | Origen | Fase |
|---|---|---|---|
| B1 | Un binding declarado **dentro de un cuerpo de función** no puede tomar el slot de nombre de todo el repositorio; un import del mismo nombre en otro archivo no debe enlazar a él | #6467 | 1 |
| B2 | Un "rechazo" del resolver que luego cae al índice global por nombre y **sí** enlaza — el rechazo debe terminar la escalera, no continuarla | #6125 | 1 |
| B3 | Dos métodos `T.Do` en el mismo paquete + una función `Do` no relacionada en otro: el tier de receiver debe dar disposition, no enlazar a la función ajena | #6125 | 1 |
| B4 | Un stub scope-local (variable local, clave de ordenamiento) **nunca** debe cross-resolver contra el índice global de nombres | #3936 | 1 |
| B5 | Nombre genérico (`get`, `format`, `run`, `value`) nunca produce arista por bare name | ADR-0011 | 1 |
| B6 | Un lenguaje nuevo arranca con allowlist vacía; CI rechaza entradas sin fixture que las justifique | ADR-0011 | 1 |
| B7 | Rutas OS-nativas (backslash de Windows) contra stubs en forma slash deben resolver igual | #49 | 1 |
| B8 | Un placeholder de import que sombrea un símbolo externo | #6369 | 1 |
| B9 | Alias de raíz de módulo (`@app/*` → `src/*`) | #4705 | 1 |
| B10 | Llamada calificada contra bare name con el mismo nombre en el mismo archivo | #4554 | 1 |
| B11 | Filtro por kind del nombre hoja: `Foo.bar` no debe enlazar a un `bar` de kind incompatible | #6141 | 1 |
| B12 | Import dinámico / `require()` con string construido → disposition `dynamic`, no bug | ADR-0011 | 1 |

## C. TypeScript / JavaScript (Fase 1)

| # | Caso | Origen |
|---|---|---|
| C1 | `tsconfig.json` con `paths` y `baseUrl`, incluidos wildcards | Resolución TS |
| C2 | Resolución de `index.ts` / `index.tsx` al importar un directorio | Resolución TS |
| C3 | Extensiones implícitas (`./foo` → `foo.ts`, `foo.tsx`, `foo.d.ts`) | Resolución TS |
| C4 | `export * from './x'` y re-exports encadenados a dos niveles | ADR-0013 |
| C5 | `export { a as b }` — el alias debe seguir apuntando a la entidad original |ADR-0013 |
| C6 | `import type { X }` — import solo de tipos, no debe generar arista de runtime | TS |
| C7 | Mezcla ESM + CJS (`require` y `import` en el mismo repo) | JS |
| C8 | Llamadas desestructuradas: `const { foo } = require('./m'); foo()` | #2625 |
| C9 | Métodos de clase como propiedades de arrow function (`foo = () => {}`) | `class_arrow_measure` |
| C10 | Destructuring de constantes usado como puerta de extracción | #2338 |
| C11 | Archivo `.d.ts` de solo tipos: entidades sin cuerpo | TS |
| C12 | Monorepo con workspaces: import cruzando el límite de paquete | Cross-repo |

## D. C# (Fase 3)

| # | Caso | Origen |
|---|---|---|
| D1 | `using static` y alias de `using` | ADR-0013 |
| D2 | Frontera de visibilidad por `.csproj` / `.sln` — no resolver a través de proyectos sin referencia | Resolución C# |
| D3 | Namespaces con `file-scoped namespace` y anidados | C# |
| D4 | `record` y `record struct` | C# |
| D5 | Métodos de extensión: el receiver es el primer parámetro | C# |
| D6 | Interfaces genéricas: `IRepository<T>` vs `IRepository<Employee>` | ADR-0012 |
| D7 | Detección de tests: xUnit, NUnit y MSTest con atributos distintos | C# |

## E. Python (Fase 3)

| # | Caso | Origen |
|---|---|---|
| E1 | Imports relativos (`from . import x`, `from ..pkg import y`) | ADR-0013 |
| E2 | Re-exports vía `__init__.py` | ADR-0013 |
| E3 | `import x.y.z as w` y su uso calificado | ADR-0013 |
| E4 | Decoradores que envuelven y renombran funciones | Python |
| E5 | Métodos de módulo llamados sin calificar (el caso idiomático que la allowlist debe cubrir) | ADR-0011 |

## F. Indexado incremental (Fase 3)

| # | Caso | Origen |
|---|---|---|
| F1 | **Árbol nulo → pérdida silenciosa total de datos.** El paso incremental desaloja las entidades del archivo y las re-añade desde la re-extracción; si la re-extracción recibe un árbol nulo y el extractor devuelve `nil, nil`, el archivo queda **vacío**, con éxito reportado y sin error | #6151 |
| F2 | **Bucle infinito de reindex** por entradas obsoletas del manifiesto: archivos que ya no están en el walk se detectan como borrados en cada pase → demasiados cambios → fallback que descarta el GC del manifiesto → repetir | #5667 |
| F3 | Un pase de extracción que falla no debe dejar el grafo a medias: se preserva el último snapshot bueno | #6209, ADR-0026 |
| F4 | Renombrar un archivo sin cambiar contenido: reancla, no reindexa | Anchors |
| F5 | `git checkout` de rama con cientos de archivos: sin reindex completo | ADR watcher |
| F6 | Cambio de rama detectado por `.git/HEAD`, no inferido de eventos de archivo | `githead_poller.go` |
| F7 | Archivo borrado y recreado con el mismo contenido dentro de la ventana de debounce | Watcher |
| F8 | Un archivo cuyo contenido vuelve a un estado ya indexado (undo): el `content_hash` coincide, no se invalida nada aguas arriba | Anchors |
| F9 | Daemon caído durante cambios: reconcile / catch-up al arrancar | `reconcile.go` |

## G. Watcher y sistema de archivos

| # | Caso | Origen |
|---|---|---|
| G1 | **Agotamiento de descriptores en macOS**: un repo de ~32k archivos consume ~40k descriptores con kqueue, contra un techo de 61,440 | #6180 |
| G2 | El presupuesto se deriva del `RLIMIT_NOFILE` **efectivo** tras el clamp del kernel, no del solicitado | #6218 |
| G3 | El modelo de costo se selecciona por **build tag**, no por `runtime.GOOS` | #6218 |
| G4 | Directorio de build no gitignoreado que churna → cuarentena tras umbral sostenido | #5392 |
| G5 | Una ráfaga humana de guardados **no** debe disparar cuarentena | #5394 |
| G6 | Un directorio en cuarentena que queda quieto se recupera solo | #5394 |
| G7 | La cuarentena sobrevive un reinicio del daemon (no re-thrashea) | #5394 |
| G8 | Lectura de archivo sobre un filesystem lento o colgado: apertura con deadline, no bloqueo indefinido | #6416 |
| G9 | Tests de debounce/coalesce con reloj inyectado, sin depender del scheduler de CI | `clock.go` |

## H. Parser

| # | Caso | Origen |
|---|---|---|
| H1 | Archivo con más de 10% de nodos ERROR → se rechaza en vez de producir entidades basura | `maxErrorRatio` |
| H2 | Archivo patológico (minificado, generado, línea de 2MB) → timeout de parse, no cuelgue | `GRAFEL_PARSE_TIMEOUT` |
| H3 | El árbol se cierra también en el camino de error (fuga de heap de C) | #5963 |
| H4 | Archivo binario o con encoding no-UTF8 disfrazado de fuente | Parser |
| H5 | Archivo vacío y archivo solo con comentarios | Parser |
| H6 | Ningún tipo del binding de tree-sitter aparece fuera de `internal/parser` (test de arquitectura) | ADR-0023 |

## I. Context Compiler y ledger (Fase 2 — propios, sin origen en Grafel)

| # | Caso |
|---|---|
| I1 | Presupuesto tan pequeño que solo cabe la entidad primaria: debe degradar por peldaño de escalera, no truncar a media línea |
| I2 | Presupuesto mayor que todo el contexto disponible: no rellenar con ruido |
| I3 | Cuota mínima por categoría: ninguna categoría desaparece entera |
| I4 | El mismo ítem pedido dos veces en una sesión sale como handle la segunda vez |
| I5 | Un ítem entregado en L1 y luego necesario en L3: sube de peldaño, no se reenvía L1 |
| I6 | El archivo cambia entre dos llamadas de la misma sesión: el handle se revalida por `content_hash` y se reenvía si cambió |
| I7 | La misma cápsula por CLI, MCP y HTTP es byte-idéntica |
| I8 | Una cápsula con residuals muestra los candidatos de desambiguación |
| I9 | Dos indexados del mismo fuente producen un snapshot byte-idéntico (determinismo) |
| I10 | Reordenar los archivos de entrada no cambia la salida |
