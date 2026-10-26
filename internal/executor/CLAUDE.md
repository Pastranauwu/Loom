# internal/executor: advertencias

- **Un CLI nuevo se agrega en dos listas (`Todos`, `config.Clis`):** `TestClisDeConfigSonLosEjecutores` caza que se separen. Además: su prefijo de modelo en `ejecutorPara` y sus modelos en `config.preferidos`.
- **claude corre con contexto limpio (`contextoLimpio`) y prompt de sistema propio (`sistemaClaude`):** sin settings de usuario ni MCP; el contexto base baja de 11,9k a 5,8k tokens por turno. No agregues `--disable-slash-commands`: también apaga las skills de `.claude/skills` del proyecto. Probado solo con una edición real de haiku.
- **Un 429 de claude espera al `resetsAt` y relanza:** no gasta intento ni escala. En codex todavía lo gasta.
- **Sin saldo es de la corrida, no de la tarea (`SinSaldo`):** un 402 no se arregla reintentando ni escalando. La tarea queda pendiente y la corrida corta (`TestSinSaldoDejaTodoPendienteSinEscalar`).
- **opencode topa la salida en 32000 tokens (`entornoOpenCode`):** loom lo sube a 128000. Un plan cortado sale con 0 y `finish: "length"`; para diagnosticar: `opencode session list` y `opencode export <sesión>`.
- **El sandbox de codex no sirve para el implementador:** `workspace-write` no deja escribir cachés en el home (`go test`, npm, cargo). `input_tokens` incluye la caché leída; `parseCodexEvents` la resta.
- **El agente muere si loom muere (`morirConPadre`):** Pdeathsig, solo Linux. Fuera de Linux sigue abierto (`ponytail:`).
- **Avances del planificador (`Request.Avance`):** cada adaptador traduce su stream (`avanceOpenCode`, `avanceClaude`) a líneas cortas. Un plan largo no está colgado.
- **Las skills llegan como texto, nunca por el mecanismo de skills de Claude Code:** medido, los agentes casi nunca las invocan solos.
- **`Request.Sesion` y `Request.TopeUSD` son solo de claude (`--resume`, `--max-budget-usd`):** los demás adaptadores los ignoran, así que quien manda un prompt recortado por continuar sesión comprueba antes `ex.Name() == "claude"`. La sesión sale del evento `result` (`finClaude`), cuyo `"type"` no va primero en el objeto. Un corte por tope es `ErrTope`.
