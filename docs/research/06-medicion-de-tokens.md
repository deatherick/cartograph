# 06 — Medición de economía de tokens

## Problema

Probar que la herramienta ahorra tokens. Es la tesis central del producto, así que la medición
tiene que ser honesta o el producto entero descansa sobre un número inventado.

## Cómo lo resolvió Grafel

`cmd/bench-tokens` — 179 + 142 líneas. Compara, por pregunta:

- **Graph tokens**: el costo del payload de `grafel_find` (el subgrafo mínimo que responde).
- **File-read tokens**: el costo de leer **completo** cada archivo distinto que la respuesta toca.
- **Ratio** = file-read ÷ graph. Más alto es mejor.

Detalles de implementación:
- Estimador de tokens: `len(s) / 4`. Explícitamente el mismo char/4 que usa la capa MCP para
  reportar `payload_token_estimate`, así que los números "cuadran" con lo que el MCP dice.
- Los archivos del baseline se recuperan del propio payload con un regex `path:line`, sin un
  segundo round-trip al grafo.
- Fallback de 3,000 tokens por archivo ilegible, *"para que el baseline no se subestime en
  silencio"* — buen instinto.
- Salida en Markdown, con ratio por fila y ratio agregado que salta las filas con costo cero
  para no distorsionar.

## Los tres huecos

**1. No hay ninguna medida de corrección.** El benchmark mide solo tokens. No hay recall, no hay
precisión, no hay gold set. Y ese es el problema entero de este tipo de métrica:
**siempre se pueden ahorrar tokens devolviendo menos**. Un `grafel_find` que devuelve una línea
vacía tiene un ratio infinito. Sin una medida de si la respuesta compacta contenía de verdad la
información necesaria, el número de ahorro no significa nada.

**2. El baseline es un hombre de paja, en la dirección que los favorece y en la que no.**
"Leer completos los archivos que la respuesta toca" asume que el agente **ya sabe qué archivos
leer** — que es precisamente lo que la herramienta le da. El baseline real de un agente sin
grafo es peor (búsquedas exploratorias, archivos leídos y descartados, callejones sin salida),
pero también es más difícil de defender. Tal como está, el baseline no corresponde a ningún
comportamiento real de agente.

**3. `len/4` no es un tokenizer.** Es una aproximación razonable para prosa en inglés y bastante
mala para código: identificadores en camelCase, sangría, símbolos y rutas tokenizan muy distinto.
El error no está acotado ni medido en ningún lado.

## Cómo lo resolvemos nosotros

`ctxbench` se construye en la Fase 0b, **antes** que el producto, y arregla los tres:

1. **Gold set obligatorio.** Cada tarea del task set trae anotado el conjunto de entidades y
   rangos que una respuesta correcta necesita. Se reportan siempre juntos:

   ```
   tokens_capsule · tokens_baseline · recall@gold · precision@gold · latency
   ```

   **Un ahorro de tokens sin su recall al lado no se reporta.** Regla de presentación, no solo
   de implementación: la métrica de ahorro nunca aparece sola, en ningún reporte ni en la UI.
   El gate de CI es conjunto — `≥70% de ahorro **con** recall ≥0.85`. Bajar el recall para subir
   el ahorro rompe el build.

2. **Baseline por traza, no por adivinanza.** Se graba una traza real de un agente resolviendo
   la tarea sin la herramienta (búsquedas y lecturas incluidas, callejones incluidos) y se cuenta
   lo que consumió. Es más trabajo y es el único baseline defendible. Se versiona junto al task
   set para que sea reproducible.
   - Se reporta también el baseline "oráculo" de ellos (leer los archivos correctos completos)
     como cota inferior conservadora. Tener las dos cifras es más honesto que tener una: la
     primera dice cuánto ahorramos en la práctica, la segunda cuánto ahorramos incluso contra un
     agente con suerte perfecta.

3. **Tokenizer real**, no `len/4`. Y si no hay uno disponible, se mide el error del estimador
   contra el tokenizer real sobre el propio corpus y se publica la desviación. Un estimador con
   error conocido es utilizable; uno con error desconocido no.

4. **Se mide el ahorro del Context Ledger explícitamente**, que es una dimensión que ellos no
   tienen: una sesión de 5 llamadas encadenadas contra 5 llamadas independientes.

## Por qué distinto

Su benchmark responde "¿cuánto más chico es nuestro payload?". El nuestro tiene que responder
"¿cuánto más barato es resolver la tarea **bien**?". Son preguntas distintas y solo la segunda
justifica el producto.
