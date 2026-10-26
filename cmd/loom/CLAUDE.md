# cmd/loom: advertencias

## Plan y esqueleto (`esqueleto.go`, `plan.go`, `completar.go`)
- **Un plan pagado no se tira por un fallo posterior (`planGuardado`):** la respuesta cruda queda en `.loom/plan-crudo.json` con el sha256 del prompt. Envuelve solo al planificador; revisor y recursión usan `generadorPlan` directo.
- **La respuesta del arquitecto se guarda (`runs/T-00N/esqueleto-respuesta.json`, con el hash del pedido):** al relanzar se reverifica sin pagar otra vez. El `ARCHITECTURE.md` previo se lee de la rama base, no del cuarto, o el prompt cambiaría al retomar.
- **`traducirDependencias` quita siempre la autodependencia:** "depende de todas" se incluye a sí misma. Un ciclo de verdad (`ciclo`, `cicloDelPlan`) vuelve al arquitecto.
- **Un esqueleto sin cambios no es tarea (`vacio` en `planearEsqueleto`).**
- **Cada tarea de relleno pasa también `verificar` (`conVerificar`):** `listo_cuando` queda `<verificar> && (<prueba>)`. Los runners de pruebas no revisan tipos.
- **El arquitecto devuelve `pruebas`, la suite completa:** en un monorepo `DetectLanguage` no la encuentra. Se guarda en `config.yml` si estaba vacía.
- **Interfaz, por código y no por prompt:** `planearEsqueleto` pone `peso: pesada` a toda tarea que toca archivos de interfaz. Un pedido visual es UNA tarea de diseño dueña de esos archivos; las funcionales dependen de ella. El arquitecto reescribe o borra las pruebas que fijan clases.
- **`arquitecto_economico: true` (`arquitectos`, apagado por defecto):** modelo medio solo si la base ya tiene `ARCHITECTURE.md`; si agota `correccionesEsqueleto`, sube al planificador. No es default hasta medirlo.
- **Docker solo se exige en un repo vacío (`exigeDocker`):** sobre código existente el arquitecto borró un `docker-compose.yml` en uso para escribir su `compose.yaml`. "con docker" en el pedido lo pide igual.
- **La corrección del arquitecto continúa su sesión (`sesion` en `planearEsqueleto`, `PromptCorregirSesion`):** solo recibe los problemas; de cero releía el repo entero. Sin sesión (otro CLI, modelo de respaldo, sesión borrada: cero turnos) vuelve a `PromptCorregir`. La sesión se guarda junto a la respuesta.
- **Antes de pagar un plan sobre código existente se corre la suite en la base (`esqueleto.SuiteBase`):** si ya falla, se avisa y el arquitecto lo recibe como dato (`Pedido.SuiteRota`). Solo con cuarto limpio y sin respuesta guardada; la respuesta se guarda con el hash del pedido sin ese dato (`clave`), porque la salida de la suite cambia por tiempos.
- **`deltaRequirements` compara por id:** nuevos, iguales, cambiados y quitados; `cubiertoPor` dice qué tareas y archivos los implementaron. Sin id explícito, una redacción nueva llega como quitado + nuevo.

## Corrida (`run.go`, `preparar.go`)
- **`modelos:` puede mezclar CLIs (`ejecutorPara`):** id con proveedor → opencode; `claude-*`/`opus`/`sonnet`/`haiku` → claude; `gpt-*`/`codex-*` → codex, sin importar `cli:`.
- **Cada modelo se valida contra el CLI que lo corre y solo se reemplaza el inválido (`revisarModelos`).**
- **Solo `topePagados` (3, o `agentes_pagados:`) tareas de pago corren a la vez sin `--agentes`:** los `…-free` no cuentan y van primero. Todas las pagadas gastan la misma ventana de 5 h.
- **Un cuarto sin carpeta en `.loom/runs/<id>` es de otra corrida y se descarta (`soltarSinRastro`, antes de `room.Ensure` en `run` y en el esqueleto):** con `.loom` borrado los ids se repiten y `Ensure` reusaba el trabajo viejo.
- **`run` se frena si una dependencia `lista` no tiene rama ni está en la base (`verdesPerdidas`):** `sembrarVerdesPrevias` lee "lista sin rama" como "ya entregada".
- **`run` integra el esqueleto y los verdes previos con `sembrarVerdesPrevias`.**
- **El revisor corre con `Effort: "low"` y el modelo `media`:** corre en cada intento verde.

## Entrega y mantenimiento (`ship.go`, `task.go`, `limpiar.go`, `reparar.go`)
- **Una tarea suelta no se entrega si sus `depende_de` siguen con rama y sin entregar (`runShip`):** va con `ship --todas`.
- **`task rm` saca la tarea del `depende_de` de las demás (`quitarDependencia`)** y avisa si tenía trabajo verde sin entregar o si su `listo_cuando` es una aceptación del feature.
- **`archive` y `limpiar` liberan cuartos (`liberarCuartos`):** cuarto y rama de cada tarea; `_integra` y la carpeta de `_entrega` solo cuando no queda ninguna con cuarto. La rama `_entrega` se queda: es el PR. `runs/` no se toca.
- **`reparar [id]` reabre la tarea que rompió la entrega conjunta:** responsable por nombre de archivo y, si ningún módulo se llama como la prueba, la tarea que la escribió (`responsables`, heurística). Vuelve a correr solo lo que falló con el selector de su runner (`ship.Fallo.Selectores`). Agrega el fallo a `listo_cuando`, las pruebas a `tocar_solo`, una nota `REPARACIÓN` con `Obsoleto:` y sube el peso a `media` como mínimo. No hay agente reparador aparte. Sin verificar con agentes reales.
