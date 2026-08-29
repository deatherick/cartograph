# 03 — Resolución: imports, bare names y dispatch por tipo de receiver

## Problema

Convertir `foo(...)`, `mod.foo(...)` y `x.Foo(...)` en aristas correctas. Es *el* problema
difícil del indexado estático y donde se gana o se pierde la calidad del grafo.

## Cómo lo resolvió Grafel — tres ADRs, tres lecciones

### ADR-0011 — Bare names: allowlist, no blacklist

Un "bare name" es un callsite donde solo se ve un identificador sin calificar. Es
**la mayor fuente individual de falsos positivos del resolver**. Los dos extremos fallan:

- Resolver todo optimistamente: `format(...)` matchea todas las entidades llamadas `format`,
  incluyendo métodos de clases sin relación. Las aristas se multiplican y el grafo es ruido.
- Rechazar todos: se pierden aristas reales en lenguajes dinámicos donde llamar helpers de
  módulo sin calificar es idiomático.

Su solución, en tres capas:
1. **Allowlist por lenguaje** — nombres que se pueden matchear optimistamente. Una entrada
   entra a la allowlist solo cuando matchearla bare no puede plausiblemente estar mal.
2. **Lista de exclusión de nombres colisión-propensos** — `format`, `get`, `set`, `run`,
   `make`, `init`, `new`, `value`… Nunca se matchean, aunque pasaran el gate de la allowlist.
3. **Todo lo demás → disposition**, categoría `bare_name_no_scope`. Nunca un edge.

Principio explícito: *"whitelisting es más seguro que blacklisting para la calidad del grafo:
las aristas que existen son reales, aunque se pierdan algunas."*

Cada lenguaje nuevo empieza con la allowlist **vacía** y crece solo cuando un fixture demuestra
que un nombre es seguro.

### ADR-0013 — Resolución cross-file consciente de imports

El binding casi siempre llega por un `import` arriba del archivo y el destino vive en otro
archivo. Ignorar eso degrada la calidad en todo lenguaje con namespaces.

Solución: cada extractor emite una **tabla de imports por archivo** (alias → destino
calificado). El resolver, ante un callsite con calificador no vacío:
1. busca el calificador en la tabla de imports del archivo → traduce alias a módulo canónico;
2. resuelve el canónico contra el índice de entidades del repo;
3. **solo** cae al resolver de bare names si no hay calificador.

La tabla de imports es salida del extractor, no se recomputa en query time.

### ADR-0012 — Tracking del tipo estático del receiver

En lenguajes tipados, `x.Foo(...)` despacha por el tipo estático de `x`. Cuando `x` es una
interfaz de la stdlib (`io.Reader`, `IEnumerable<T>`), el resolver sabe el nombre del método
pero no la implementación. Fallan igual perder la arista y matchear por nombre en todo el grafo.

Solución: los extractores registran el **tipo estático del receiver**, y un paso dedicado:
1. reconoce un conjunto **curado** de interfaces de stdlib por lenguaje;
2. busca en el scope del callsite tipos concretos que puedan llegar ahí;
3. si hay **exactamente un** candidato que define el método → emite el edge;
4. si no → disposition `stdlib_interface_unresolved`. **Nunca adivina.**

El conjunto es curado, no heurístico: cada entrada cuesta cero o un falso positivo en los
tests de corpus antes de entrar.

## Cómo lo resolvemos nosotros

Los tres se adoptan casi tal cual — son conocimiento ganado a golpes y no hay una versión
más lista de esto. Las diferencias:

1. **Orden del pipeline fijo y documentado**, igual que ellos:
   `same-file → import-table → receiver-type → bare-name(allowlist) → disposition`.
2. **La allowlist y la exclusión van en datos, no en código.** Ellos lo dejaron explícito:
   *"runtime tuning is not supported in v1; allowlist edits require a binary rebuild"*.
   Nosotros las cargamos de un archivo embebido pero override-able por proyecto, porque un
   monorepo grande tiene su propio vocabulario de nombres genéricos. La lista de exclusión
   base arranca con la suya, que ya está validada.
3. **La regla del candidato único se generaliza**: no solo para interfaces de stdlib, sino como
   política global del resolver — *si hay exactamente un candidato, edge con confianza alta;
   si hay más de uno, disposition con la lista de candidatos como evidencia*. La cápsula de
   contexto puede mostrar esa ambigüedad al agente, que muchas veces sí sabe desambiguar.
   Ese es un uso del Context Compiler que Grafel no tiene: convertir la ambigüedad del resolver
   en una pregunta concreta en vez de en una arista perdida.
4. **La regla de la allowlist vacía por lenguaje se hace cumplir en CI**: un lenguaje nuevo no
   puede añadir entradas sin un fixture que las justifique.

## Aplicación a nuestros tres lenguajes

- **TypeScript/JS**: tabla de imports es obligatoria (ESM + CJS + `export *` re-exports).
  Además `tsconfig.json` con `paths`/`baseUrl`, resolución de `index.ts`, y extensiones
  implícitas. Aquí el 90% de las aristas dependen de la tabla de imports.
- **C#**: `using` + `using static` + alias, namespaces, y el grafo de proyectos (`.csproj`/`.sln`)
  como frontera de visibilidad. Receiver-type es especialmente rentable por ser tipado.
- **Python**: `from x import y`, imports relativos, re-exports vía `__init__.py`. Es donde más
  pesa la política de bare names.
