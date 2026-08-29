# ADR-0002: Protocolo de reuso de Grafel — estudiar y reimplementar

- **Status**: Accepted
- **Date**: 2026-08-29

## Contexto

Grafel (`cajasmota/grafel`, MIT) ya resolvió a lo largo de 9,063 archivos y 27 ADRs una cantidad
sustancial de problemas difíciles de indexado estático: resolución de imports, bare names,
watcher en distintos SO, formato de grafo en disco, taxonomía de entidades. Copiar sería legal
(MIT), pero copiar arrastra su arquitectura y convierte este proyecto en "Grafel + botón de
duplicados", sin diferenciación real.

## Decisión

Se clona Grafel en `~/code/_ref/grafel` (fuera de este repo, nunca como submódulo ni
dependencia) y se lee a fondo en una fase de discovery dedicada (Fase 0a, completada). El
conocimiento se extrae en `docs/research/` como notas propias (*problema → cómo lo resolvieron
→ cómo lo resolvemos → por qué distinto*) y como un backlog de casos borde que se convierten en
fixtures de test.

**No** se vendoriza ni se importa ningún paquete de `github.com/cajasmota/grafel`. **No** se
copia código literal — si algún algoritmo puntual se adapta de cerca, se marca inline
(`// adaptado de grafel (MIT), ver docs/research/...`) y se acredita en `NOTICE.md`; debe ser la
excepción documentada, no la norma. **No** se copian nombres de tools MCP, formatos de archivo
en disco ni el esquema de entidades — el modelo de este proyecto gira alrededor del Context
Compiler, no de la navegación del grafo.

## Consecuencias

- Se evitan meses de trabajo re-descubriendo casos borde ya resueltos (ver
  `docs/research/backlog-casos-borde.md`, 80 casos).
- Cada decisión de arquitectura de este proyecto se justifica por sí sola en su propio ADR,
  nunca con "Grafel lo hace así" — evita heredar supuestos sin escrutinio.
- El costo es que el discovery es una fase explícita que no produce código directamente
  (Fase 0a, ≈1-2 sesiones) antes de que el proyecto visible empiece a moverse.

## Alternativas consideradas

- **Fork de Grafel**: más rápido para arrancar, pero arrastra toda la arquitectura y el
  historial; alto riesgo de terminar siendo un fork superficial.
- **Clean-room total** (sin siquiera clonar, solo README público): máxima libertad de diseño,
  pero desperdicia conocimiento ya validado (allowlists de bare names, modelo de costo de
  descriptores por SO, formato de grafo) que costó sprints reales descubrir.
