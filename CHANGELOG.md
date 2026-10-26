# Changelog

Qué trajo cada versión, en una línea. Lo anterior a v1.2.0 está contado en `docs/HISTORIA.md`.

## v2.0.0 (26 oct 2026)
**devclean pasa a llamarse loom**, por el telar de Jacquard: el humano diseña el patrón y la máquina teje el código. Binario `loom`, carpeta `.loom/` (un `.devclean/` existente se migra solo la primera vez), `loom.spec.yml` (`devclean.spec.yml` se sigue leyendo), ramas `loom/<id>` y variables `LOOM_*`. Inglés primero y contexto bajo control. **Migración desde 1.x:** los contratos y specs que escribe devclean ahora usan llaves en inglés (`title`, `done_when`, `only_touch`, `weight: light|medium|heavy`...), así que un binario 1.x no los lee; el 2.0 sigue leyendo todo lo escrito en español y las banderas en español (`--agentes`, `--revisar`, `--integrar`, `--fondo`, `--reintentar`, `--todas`) siguen funcionando como alias. Los prompts de todos los roles y el JSON que devuelve el modelo pasan a inglés (se aceptan también las llaves en español); `language: es` en `config.yml` pide en español solo la prosa para el humano. Contador de contexto por sesión: con claude, el siguiente intento continúa la sesión (`--resume`) con un mensaje corto mientras use menos del `resume_below_pct` (70 %) de la ventana, y arranca de cero por encima; `usage` muestra el pico de contexto y las reanudaciones. Código para agentes: los prompts piden código denso sin comentarios para humanos y el paso `ruido` frena código comentado, `TODO`/`FIXME`/`HACK`/`NOTE`/`NO TOCAR` y rachas de más de 2 líneas de comentario. Grafo incremental del repo (`devclean graph`, `.devclean/repo-graph.json`) que leen el planificador y el arquitecto. El spec es estado vivo: `.devclean/state.json` guarda un hash por requirement, regla, aceptación y restricción, `apply --dry-run` muestra el delta, el planificador recibe solo lo que cambió y se admiten varios specs (`specs/*.yml`). `README.md` en inglés y el español en `README.es.md`. Nuevos comandos: `loom dna` (arquetipos de diseño `hexagonal`, `zero-deps`, `crash-early`, `functional-core` que entran a cada prompt, y la esclusa `dna` que bloquea dependencias nuevas en manifiestos con `zero-deps` y que el dominio importe infraestructura con `hexagonal`/`functional-core`), `loom sketch` (entrevista de cuatro preguntas que termina en un spec) y `loom heal --from crash.log` (diagnostica un fallo, agrega al spec la regla y el criterio que faltaban y corre la tarea correctiva). Logo y encabezado de la TUI de loom. `config.yml` se escribe con llaves en inglés (`tests`, `models: light|medium|heavy`, `agent_timeout`...) y las españolas se siguen leyendo; `resume_below_pct: 0` ya no apaga `--resume` (0 es el default, `off` lo apaga). Los comandos `limpiar`, `modelos` y `reparar` pasan a `clean`, `models` y `repair`, con los nombres viejos como alias. El idioma se elige en `loom init`, con `loom language [en|es]` o en el instalador; la ayuda, los mensajes, el mapa y el tutorial lo siguen y los prompts siguen en inglés. `loom tutorial`: recorrido paginado para quien llega nuevo. `loom map` (alias `view`) dibuja el sistema sin abrir código: módulos, contratos `expone` con quién los consume, imports, ADN activo y estado de las compuertas, como árbol en terminal, `--mermaid` o `--json`.

## v1.11.5 (8 oct 2026)
El tope de intentos no cuenta los que se gastaron contra otra base: arreglarla y reintentar ya no pide tocar el contrato; aviso, al aplicar el plan y al fallar la aceptación, del comando que nombra una prueba que no existe (y dónde está si solo cambia el prefijo); `doctor` y `ship --todas` avisan antes de empezar si la cuenta activa de gh no puede subir a origin; comentarios y datos de prueba sin nombres de proyectos.

## v1.11.4 (8 oct 2026)
Una entrega conjunta cortada (Ctrl+C, kill) ya no deja las ramas de los cuartos aplanadas: sus puntas se guardan en `.devclean/puntas.json` y el `ship --todas` o `run` siguiente las devuelve (`RestaurarPuntas`); el fallo del conjunto nombra la prueba que falló y `reparar` encuentra sola a la tarea que la escribió, con unittest/Django, pytest y jest/vitest; ningún git de la entrega pregunta credenciales, tampoco por el askpass de un IDE, y el `fetch` tiene tope de tiempo; el paso `ruido` deja imprimir al archivo que declara un `main` aunque no viva en `cmd/` o `scripts/`.

## v1.11.3 (8 oct 2026)
Docker solo se exige en proyectos generados desde cero (sobre código existente se pide con "con docker"); entrega local aunque haya remoto (`--local` en `ship` y `up`, o `entrega: local` en `config.yml`); cuartos y ramas de otra corrida se descartan en vez de reusarse (`soltarSinRastro`); `run` se frena si el trabajo verde del que depende una tarea ya no existe; aviso cuando la rama actual no es la base; la respuesta del arquitecto con prosa antes del JSON ya no cuesta una ronda de corrección (`plan.CuerpoJSON`); Django detecta `python manage.py test`; con claude, la corrección del arquitecto continúa su sesión en vez de releer el repo (`--resume`) y `tope_arquitecto_usd` corta cada invocación (`--max-budget-usd`); los cuartos heredan el entorno de Python del repo y del cuarto del que parten en vez de reinstalar de cero; la suite se corre en la base antes de planear y, si ya falla, se avisa; lo que un agente arregla fuera de su alcance se revierte pero queda como parche aplicable; la base por defecto es la rama actual, no `main`; `claude-sonnet-5-5` y `claude-haiku-5-5` en el catálogo de claude y como preferidos; los `.env*` que nombran un ambiente real (`prod`, `production`, `staging`, `stage`, `live`) no se copian a los cuartos; un cuarto reusado cuyo rebase choca en commits intermedios se aplana sobre la base nueva, y si no puede, avisa; con entrega local la esclusa no toca la red y el `fetch` nunca pregunta credenciales; la rama de entrega hereda el entorno de Python de los cuartos que integra.

## v1.11.2 (1 oct 2026)
`arquitecto_economico` arranca cambios incrementales con el modelo medio; el parser de contratos tolera campos desconocidos (aviso en lugar de fallo); modificadores y genéricos de TypeScript en la verificación de firmas (`NombreDeFirma`, `FirmaVerificable`).

## v1.11.1
Libera los cuartos al archivar (`devclean limpiar`) y rechaza la prueba del agente que pasa sin su implementación.

## v1.11.0
Examinador ciego conectado a las tareas del esqueleto y extendido a TypeScript/JavaScript (y a Python en monorepos); `devclean reparar`; la entrega ya no reescribe las ramas de los cuartos.

## v1.10.0
Codex como tercer CLI; tablero que cabe en la terminal (filtro, detalle, entrega conjunta); `devclean archive` (historial tipo ADR de los features probados); arreglos de entrega: dry-run que reescribía la rama, "ya pasa" falso y pruebas viejas que nadie podía actualizar.

## v1.9.0
Cuartos fuera del árbol del proyecto (`room.Dir`); lo pensado por el arquitecto viaja como `Idea:` en cada stub (una tarea es un módulo entero); menos contexto por turno: prompt de sistema propio para claude y lista de archivos al final del prompt.

## v1.8.0
Mantenimiento incremental por spec (un `up` sobre el mismo spec no replanea; uno editado solo manda lo que cambió, `spec.IntencionFile`; ids de requerimiento con `cubre`); evolución de código existente con docker compose; revisión visual con capturas de flujos.

## v1.7.0
Esqueleto plano (cada tarea escribe su prueba desde `Casos:`); presupuesto que cuenta la caché; modelos gratis de opencode mezclados con claude; tope de 3 agentes pagados; escalera sin opus.

## v1.1.0 (18 sep 2026)
Requirements as Code: `devclean.spec.yml` declara requirements, reglas, aceptación y restricciones; los contratos de tarea quedan como IR; el grafo se valida antes de ejecutar y la aceptación global corre sobre el conjunto integrado.
