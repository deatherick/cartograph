# 02 — Modelo de refs y dispositions (calidad del grafo auditable)

## Problema

Un extractor ve `foo.bar(x)` y no sabe a qué entidad apunta. Alguien tiene que decidir si eso
se convierte en un edge, y si no, **por qué no**. Sin esa contabilidad no hay forma de saber
si el grafo es bueno.

## Cómo lo resolvió Grafel

**Separación extractor / resolver mediante un stream de refs.** El extractor emite
"referencias estructurales" (este sitio del AST menciona este símbolo de esta forma); el
resolver las convierte en un edge concreto o en una *disposition*.

ADR-0010 eligió **Formato A — registro plano** con un campo `kind` como discriminador y los
campos irrelevantes en cero, en vez de un sum type por variante. Razón: Go no tiene sum types
ergonómicos y el resolver es un switch sobre `kind` de todas formas.

**La mejor idea del repo — la taxonomía de dispositions.** Cada endpoint inspeccionado cae en
exactamente un bucket:

| Disposition | Significado |
|---|---|
| `resolved` | Se reescribió a un ID de entidad |
| `external-known` | Apunta a un paquete externo **en la allowlist** (django, react, fmt) |
| `external-unknown` | Paquete externo no catalogado |
| `external-sql` | Superficie SQL sin resolver, contada aparte |
| `dynamic` | Intrínsecamente irresoluble estáticamente (reflexión, import dinámico, nombres por env, strings de template). **No es un bug** |
| `bug-extractor` | El stub tiene forma `Kind:Name` pero el grafo tiene 0 entidades con ese nombre. Un extractor debió emitir una entidad y no lo hizo |
| `bug-resolver` | El nombre SÍ existe en el grafo pero el resolver no supo desambiguar |
| `unclassified` | Catch-all. Debe ser 0 en producción |

Y encima de eso, **la métrica que lo hace útil**:

```
bug_rate = (bug-extractor + bug-resolver) / total
```

Eso convierte "¿qué tan bueno es el grafo?" en un número auditable que se puede vigilar en CI.
Separa limpiamente "no se puede resolver por diseño" de "es culpa nuestra".

**El error que cometieron:** los stubs son *strings*. El resolver parsea una gramática de
strings mágicos (`scope:<kind>:<subtype>:<lang>:<file>:<name>`, `ext:<pkg>`, `var:<name>`) con
constantes de índices de segmento. El comentario del código admite el costo:
*"magic-string drift that caused issue #49"*. Hay un caso documentado (#3936) donde una clave
de ordenamiento de pymongo `var:order` se enlazó por error a un parámetro de query de OpenAPI
llamado `order`, porque el stub local se cross-resolvió contra el índice global de nombres.

## Cómo lo resolvemos nosotros

1. **Adoptamos la separación extractor → refs → resolver.** Es correcta: el extractor no
   necesita saber nada del resto del repo, lo que hace la extracción paralelizable por archivo
   y cacheable por `content_hash`.
2. **Adoptamos el registro plano** (`Ref` con `Kind` discriminador) por la misma razón que ellos.
3. **Adoptamos la taxonomía de dispositions y el `bug_rate` como métrica de CI con regresión
   bloqueante.** Esto no estaba en el plan original y es una mejora directa.
4. **Corregimos el error de los strings mágicos:** el target sin resolver es un **struct
   tipado**, no una cadena parseable:

   ```go
   type RefTarget struct {
       Scope   TargetScope // Local | SameFile | Imported | Qualified | External | Dynamic
       Module  string      // vacío si no aplica
       Name    string
       Member  string
       Lang    Lang
       File    string      // origen, en forma slash-normalizada
   }
   ```
   Un target con `Scope == Local` **no puede** consultar el índice global por construcción del
   tipo — el bug #3936 es imposible de escribir, no algo que haya que recordar no hacer.
5. **Normalización de rutas en la frontera:** todos los identificadores internos usan forma
   slash. `filepath.FromSlash` solo se llama al tocar el disco. (Ellos llegaron a esto por un
   bug de Windows; lo tomamos gratis.)

## Por qué distinto

La taxonomía de dispositions es su mejor idea y la tomamos entera. El transporte por strings
es su peor decisión estructural y la tipamos. El resultado es la misma auditabilidad con una
clase entera de bugs eliminada por el sistema de tipos.
