# 08 — Arquitectura de proceso, handoff y el bucle de residuals

## A. El handoff escritor/lector (ADR-0024) — el patrón más elegante del repo

### Problema
El indexador escribe y el servidor de queries lee. Coordinar los dos suele traer locks,
notificaciones, invalidación de caché y una clase entera de bugs de concurrencia.

### Cómo lo resolvió Grafel
No coordina nada. El protocolo completo de notificación es **un archivo nuevo**:

- El caché de grafos mantiene handles `mmap` lazy, en un LRU concurrente.
- En cada `Get`, hace `stat` del archivo y compara el `ModTime().UnixNano()` contra el mtime
  capturado al abrir. Si el motor escribió un `graph.fb` más nuevo, reabre el mmap de forma
  transparente.
- Los handles viejos quedan **pinneados hasta que sus lectores terminan**, y recién ahí se
  hace `Munmap`.
- Su frase textual: *"un proceso lector no necesita ninguna notificación del escritor — un
  archivo atómico fresco es el handoff entero"*.

Eso les permitió partir el daemon en dos procesos (serve / engine) de forma barata, motivados
por un fallo estructural real: **cualquier panic del motor tumbaba la conexión MCP**. Un panic
del fbwriter, un `grafel update` que solo toca código del motor, o un reindex completo
compartían proceso —y destino— con queries sensibles a latencia.

### Cómo lo resolvemos nosotros
Adoptamos el patrón completo desde V0, aunque arranquemos en un solo proceso:

- El snapshot se escribe **siempre** de forma atómica (escribir a temporal + `rename`).
- El lado lector abre por `mmap`, revalida por mtime y reabre transparente.
- Los handles viejos se pinnean hasta que terminan sus lectores.

El costo de hacerlo así desde el principio es casi cero, y compra dos cosas: el reindex nunca
bloquea una query, y partir el daemon después es una decisión de despliegue, no un rediseño.
Ellos llegaron aquí tras el incidente; nosotros empezamos aquí.

**Corolario para nuestra Fase 3:** un panic en el indexado nunca puede tumbar la sesión MCP de
un agente. La extracción corre con recuperación y fail-soft: si un pase de índice falla, se
preserva el último snapshot bueno en vez de dejar el grafo a medias. (Ellos lo aprendieron con
el precipicio de 2 GiB: su fix fue abortar limpio preservando el `graph.fb` anterior.)

## B. El bucle de reparación de residuals (ADR-0015)

### El dato más útil de todo el discovery: cuánto residual es normal

Su bug-rate medido en corpus reales:

| Corpus | Bug-rate |
|---|---|
| Corpus sintético de ship-gate | 12% |
| `django-realworld` | 7.83% |
| Grupo `client-fixture` (3 repos) | 11.34% |

**Entre 8% y 12% de las referencias cross-símbolo no se resuelven**, después de dieciocho
sprints de trabajo por lenguaje. Es la calibración honesta de qué esperar de un resolver
estático maduro, y hay que decirlo en la documentación: ellos anotan como consecuencia negativa
que *"los usuarios ven 12% en un índice fresco y creen que la herramienta está rota"*.

También documentan el costo de perseguir ese residual con reglas: `internal/external/synth.go`
tiene **11,333 líneas** y `internal/resolve/refs.go` **3,196**. Cada ola de allowlists por
framework baja el bug-rate un poco. Funciona para stacks populares y estables, y no escala a
la cola larga.

### Su solución
Convertir el residual en una **cola de trabajo para un agente**:
1. El indexador emite candidatos con contexto suficiente para decidir sin releer el repo
   (`from_entity`, `relation`, `original_stub`, `disposition_reason`, `candidates`,
   `context_window`).
2. El agente escribe resoluciones con `resolution` / `confidence` / `reasoning` / `source`.
3. Un pase de aplicación las mete **antes** de la clasificación de dispositions, como overrides.

Reglas de seguridad que valen la pena copiar enteras:
- **Modelo de confianza por allowlist, no por blocklist**: el indexador enumera *qué*
  resoluciones acepta y rechaza cualquier otra cosa con una razón registrada.
- **Las reparaciones no mutan el grafo directamente**: mutan la tabla del resolver antes de
  clasificar, así el emisor del grafo sigue siendo el único escritor.
- **Todo edge lleva `source`** — *"convierte el grafo de 'confía en el binario' a 'audita el
  binario'"*.
- **Reversibilidad total**: borrar el archivo de reparaciones y reindexar vuelve a estático puro.
- **Determinismo**: se aplican en orden de `edge_id`, así mismo fuente + mismas reparaciones →
  salida byte-idéntica.

### Cómo lo resolvemos nosotros

Esto encaja exactamente con nuestras "learned relationships" (Fase 7), y lo mejora en un punto:

1. **Adoptamos las cinco reglas de seguridad tal cual.** Son buenas y son gratis.
2. **Priorización por centralidad desde el día 1.** Ellos la dejaron para una fase 3 futura,
   tras notar que un grafo con 5,000 residuals no es interactivo con un round-trip de LLM por
   arista. Como nosotros ya calculamos centralidad al indexar (nota 04), ordenar la cola por
   impacto es gratis: reparar los 50 residuals más centrales vale más que reparar 5,000
   periféricos.
3. **El residual no espera a un agente: se muestra en la cápsula.** Esta es la diferencia real.
   Cuando el compilador arma contexto para una tarea y hay un residual en el radio de impacto,
   la cápsula lo declara:

   ```
   RESIDUALS (sin resolver, 2)
     ?  RestrictionApi.fetch → 3 candidatos: E12, E31, E44
   ```

   El agente que ya está trabajando en esa tarea tiene el contexto para desambiguarlo, y su
   decisión se puede capturar. El bucle de reparación deja de ser un pase batch separado y se
   vuelve un subproducto del uso normal. Ellos tienen el bucle y la cápsula por separado;
   juntarlos es nuestro producto.
4. **El bug-rate es métrica de CI con regresión bloqueante**, y se reporta **separado** del
   residual legítimo (dynamic / external), para no repetir su problema de que un usuario vea
   12% y crea que está roto. La UI muestra tres números distintos: resuelto, irresoluble por
   diseño, y bug nuestro.

## C. Lo que NO copiamos

- **El split de procesos en V0.** Ellos llegaron a él por incidentes de escala que nosotros no
  tenemos. Adoptamos el patrón de handoff que lo hace barato, y partimos solo si duele.
- **Las 11,333 líneas de síntesis de paquetes externos.** Es la cola larga de frameworks
  perseguida a mano. Nuestra apuesta es que el bucle de residuals + la desambiguación en la
  cápsula cubren esa cola larga sin ese código. Si a los tres lenguajes de V0 el bug-rate no
  baja del ~12% con ese enfoque, la apuesta falló y hay que reconsiderar.
