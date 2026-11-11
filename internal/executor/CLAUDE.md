# internal/executor: advertencias

- **Un CLI nuevo se agrega en dos listas (`Todos`, `config.Clis`):** `TestClisDeConfigSonLosEjecutores` caza que se separen. Además: su prefijo de modelo en `ejecutorPara` y sus modelos en `config.preferidos`.
- **claude corre con contexto limpio (`contextoLimpio`) y prompt de sistema propio (`sistemaClaude`):** sin settings de usuario ni MCP; el contexto base baja de 11,9k a 5,8k tokens por turno. No agregues `--disable-slash-commands`: también apaga las skills de `.claude/skills` del proyecto. Probado solo con una edición real de haiku.
- **Un 429 de claude espera al `resetsAt` y relanza:** no gasta intento ni escala. En codex todavía lo gasta.
- **Sin saldo es de la corrida, no de la tarea (`SinSaldo`):** un 402 no se arregla reintentando ni escalando. La tarea queda pendiente y la corrida corta (`TestSinSaldoDejaTodoPendienteSinEscalar`).
- **`Request.Effort` llega a opencode como `--variant`:** sin ella deepseek razona a su default en cada turno (revisor en flash: 96 s sin variante, 32 s con `low`). Una variante que el modelo no define cae a su default.
- **opencode topa la salida en 32000 tokens (`entornoOpenCode`):** loom lo sube a 128000. Un plan cortado sale con 0 y `finish: "length"`; para diagnosticar: `opencode session list` y `opencode export <sesión>`.
- **El sandbox de codex no sirve para el implementador:** `workspace-write` no deja escribir cachés en el home (`go test`, npm, cargo). `input_tokens` incluye la caché leída; `parseCodexEvents` la resta.
- **El agente muere si loom muere (`morirConPadre`):** Pdeathsig, solo Linux. Fuera de Linux sigue abierto (`ponytail:`).
- **Avances del planificador (`Request.Avance`):** cada adaptador traduce su stream (`avanceOpenCode`, `avanceClaude`) a líneas cortas. Un plan largo no está colgado.
- **Las skills llegan como texto, nunca por el mecanismo de skills de Claude Code:** medido, los agentes casi nunca las invocan solos.
- **`Request.Sesion` la continúan claude (`--resume`) y opencode (`--session`, id de `sesionOpenCode`); `Request.TopeUSD` es solo de claude:** codex corre `--ephemeral` y la ignora, así que quien manda un prompt recortado por continuar sesión comprueba antes `executor.ContinuaSesion`. Sesión muerta en opencode: sale con 1 y cero turnos, igual que claude. La sesión sale del evento `result` (`finClaude`), cuyo `"type"` no va primero en el objeto. Un corte por tope es `ErrTope`.

- **`Usage.LastTurn` es el contexto del último turno de claude (`parseClaudeStream`):** lo que la sesión lleva al siguiente intento; la suma de `Input`/`CacheRead` es facturación, no tamaño de contexto. `ContextWindow` es una tabla (200k, 1M con `[1m]`): los CLI no la informan.
- **Con `--resume`, `modelUsage` y `total_cost_usd` del resultado son de la sesión entera (`parseClaudeStream` con `reanuda`):** se toman entrada y caché de la suma de mensajes y la salida de `usage`; el costo se prorratea. Sin esto cada reintento reanudado volvía a contar todos los anteriores (T de 4 intentos: $2,87 reportados, $0,34 reales).
- **`SinSaldo` cubre también el tope de gasto y la cuota del mes, que llegan como 429 ("spending cap", "exceeded your current quota"):** no confundir con el 429 de ventana de claude, que espera al reset.
- **claude corre con `BASH_MAX_OUTPUT_LENGTH=12000`:** lo que imprime un comando se relee en cada turno siguiente (una suite entera: 40k tokens arrastrados el resto del intento). opencode no tiene perilla equivalente: trunca a ~50 KB y solo queda la regla de `skills.Base` de no correr la suite entera.
