# 05 — Watcher, exclusiones e invalidación incremental

## Problema

Mantener el índice fresco sin quemar la máquina. Suena a "usar fsnotify y listo". No lo es.

## Cómo lo resolvió Grafel

### La trampa de macOS: descriptores de archivo (el hallazgo más caro de este discovery)

`fsnotify` v1.10.1 **no trae backend de FSEvents** — su `backend_kqueue.go` lleva el build tag
`darwin`. Y kqueue no puede vigilar una ruta sin un descriptor abierto sobre ella. Peor: para
un directorio, fsnotify llama a `watchDirectoryFiles()` y abre **un descriptor por cada archivo
dentro**.

Su medición en vivo sobre **un solo repo**:

> 40,079 descriptores — 32,073 archivos regulares + 7,995 directorios.
> El término por archivo era **4× el término por directorio**.
> Contra `kern.maxfilesperproc = 61,440`, eso es **65% del techo del proceso para UN repo**.

Su cap anterior contaba solo directorios, así que era ciego al término que domina: a 100,000
directorios el cap viejo era efectivamente infinito.

En Linux inotify gasta un watch descriptor por directorio de `max_user_watches`, y nada por
archivo. En Windows `ReadDirectoryChangesW` difiere otra vez. El modelo de costo se selecciona
por **build tag, nunca por un check de `runtime.GOOS`** — para que la constante esté disponible
al compilador y a los tests con build tag.

Su solución fue un **presupuesto de descriptores** con modelo de costo por plataforma
(`kqueue: perDir 1, perFile 1` / `inotify: perDir 1, perFile 0`), derivado del límite
**efectivo** (el `RLIMIT_NOFILE` leído de vuelta tras el clamp del kernel, no el que se pidió —
launchd pide 65536, el kernel lo baja a 61440 en silencio), reservando **un cuarto** del límite
para todo lo que no son watches (mmaps del grafo, socket unix, listener del dashboard, logs,
pipes de los subprocesos de git), con piso de 1024.

Su justificación de por qué el presupuesto va del lado de los watches y no del store:
*"quedarse sin descriptores para el store es riesgo de corrupción; quedarse sin presupuesto de
watch solo cuesta frescura"*. Correcto y no obvio.

### Exclusiones en tres capas

1. **Lista estática de skip** — el ~90% de la basura conocida, barato.
2. **`.gitignore`** — respetado de verdad.
3. **Cuarentena adaptativa por churn** — la capa que no se me habría ocurrido. Un directorio
   de build no gitignoreado que churna patológicamente dispara un bucle de reindex continuo
   (su "clase de incidente #5392"). Mecanismo:

   ```
   Observar → Detectar → Cuarentena → (persistir) → Auto-sanar
   ```

   - Se atribuye cada evento que sobrevive los filtros a su **directorio**, con conteo de churn
     en ventana deslizante.
   - Al cruzar el umbral, el directorio se pone en cuarentena: los eventos bajo él se descartan
     en la frontera del evento y la decisión se escribe en un log de auditoría.
   - El set se persiste en disco, así que un bucle de build que se auto-cuarentenó **no vuelve a
     thrashear tras reiniciar el daemon**.
   - `Sweep()` re-evalúa periódicamente y saca de cuarentena lo que lleva rato quieto.

   Invariante de seguridad explícito: *nunca poner en cuarentena un directorio fuente
   legítimamente activo*. Lo garantizan con umbral de churn **sostenido** (una ráfaga humana de
   guardados queda muy por debajo; solo un bucle mecánico de decenas o cientos de escrituras en
   la ventana lo dispara) e histéresis (`HealQuiet` >> `ChurnWindow`) para no oscilar.

### Otros detalles ganados a golpes

- **Poller de `.git/HEAD`** para detectar cambio de rama, en vez de intentar inferirlo de los
  eventos de archivo.
- **Reloj inyectable** en toda la ruta de debounce/coalesce, para que los tests de timing sean
  deterministas y no dependan del scheduler de CI.
- **Reconcile / catch-up** al arrancar: el daemon estuvo caído, el árbol cambió.

## Cómo lo resolvemos nosotros

1. **En macOS no usamos kqueue.** Usamos **FSEvents** directamente (`fsnotify/fsevents` o
   equivalente), que vigila un árbol completo de forma recursiva con **un solo stream y cero
   descriptores por archivo**. Ese es el fix de raíz al problema que ellos gestionaron con un
   presupuesto. Ellos administran la escasez; nosotros la eliminamos en la plataforma donde
   duele. macOS es nuestra plataforma primaria, así que esto no es opcional.
   - Costo: FSEvents da eventos por directorio con granularidad más gruesa y coalescing propio,
     así que hay que re-statear el directorio para saber qué cambió. A cambio, el costo es
     constante en el número de archivos.
   - En Linux seguimos con inotify (el término por archivo es cero, no hay problema) y
     mantenemos el cap por directorio.
   - El modelo de costo se selecciona **por build tag**, tal como ellos aprendieron.
2. **Adoptamos las tres capas de exclusión completas**, incluida la cuarentena adaptativa con
   persistencia y auto-sanado. Es defensa contra una clase de incidente real que de otro modo
   descubriríamos en producción.
3. **Adoptamos el poller de `.git/HEAD`** y el reloj inyectable.
4. **La invalidación es por `content_hash`, no por mtime.** Un `git checkout` toca el mtime de
   cientos de archivos cuyo contenido vuelve a ser el mismo que ya teníamos indexado. Si el
   hash del cuerpo de una entidad no cambió, se re-ancla el rango de bytes y **no se invalida
   nada aguas arriba**. Esto es lo que hace viable el objetivo de "checkout de 400 archivos en
   menos de 10s" y es donde nuestro modelo de anchors paga.
5. **Reserva de descriptores para el store** de todas formas, con su razonamiento: la frescura
   es sacrificable, la integridad del store no.
