# ADR-0001: Core en Go

- **Status**: Accepted
- **Date**: 2026-08-29

## Contexto

Se evaluaron Go, Rust, un híbrido Go+Rust y TypeScript/Node para el core (parser, grafo,
resolver, compilador de contexto, daemon).

## Decisión

**Go.** Single binary, cross-compile trivial, bindings de tree-sitter maduros y mantenidos
(`tree-sitter/go-tree-sitter`), daemon barato en memoria/CPU, y es el mismo lenguaje en el que
Grafel resolvió ya un volumen enorme de casos borde de resolución — reimplementar ese
conocimiento en el mismo lenguaje que se estudió reduce el riesgo de traducción.

## Consecuencias

- Concurrencia con goroutines/channels encaja bien con indexado paralelo por archivo y con el
  watcher.
- Sin runtime de VM, sin JVM, sin dependencia de Docker para distribuir.
- Se pierde el ecosistema de tooling de análisis estático de Rust (borrow checker) para el
  Similarity Engine; se acepta el riesgo — ver docs/research para el estado del arte que ya
  existe en Go vía `gonum/graph` y afines.

## Alternativas consideradas

- **Rust puro**: máximo control y rendimiento, pero reconstruir extractores, resolvers e
  indexado incremental desde cero alarga significativamente el V0.
- **Híbrido Go+Rust** (similarity engine vía FFI): complejidad de build/CI desde el día 1, sin
  beneficio claro hasta que el Similarity Engine (Fase 5) exista y se mida.
- **TypeScript/Node**: un solo lenguaje con la UI, pero peor rendimiento en repos grandes y
  daemon más pesado en memoria.
