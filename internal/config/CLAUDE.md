# internal/config: advertencias

- **Con ahorro (default, `escalar_tras`/`escalate_after`), `PesoInicial` baja la media a liviana:** `ModeloConstructor` y `ModeloEscalado` pasan por ahí, así haiku→sonnet vale en tareas planas y subtareas. `off` (-1) restaura media en sonnet. Sin modelo liviano no baja, ni a un liviano de opencode con medio en otro CLI (`otroCLI`).
- **La escalera nunca sube a `pesada` (`ModeloEscalado`):** liviana→media y ahí para. Opus solo corre en tareas `peso: pesada` y en el planificador. Una tarea que el medio no resuelve suele tener el contrato mal hecho.
- **El campo del CLI es `cli`, no `ejecutor`:** choca con el rol `ejecutor` de `proveedores` por cómo `kv` pisa claves repetidas.
- **`gratis` reconoce `-free` (opencode) y `:free` (OpenRouter).**
- **`tope_arquitecto_usd` es por invocación del arquitecto, a precio de lista y solo con claude:** con suscripción no es dinero cobrado, es la misma cuenta que usa el CLI para cortar.
- **La base por defecto es la rama actual (`DetectBaseBranch`), no `main`:** en un repo con ramas de feature los agentes partían de otro código y la entrega caía en otra rama. `main`/`master` solo si se está en una rama `loom/*` o sin rama.
- **Revisor y examinador caen a `media`, nunca a `pesada` (`ModeloRol`); sin `modelos:` y con claude, `medioPorDefecto` (cmd/loom) da sonnet:** con "" claude usa el default de la cuenta, que puede ser opus.
