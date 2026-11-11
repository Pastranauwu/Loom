# internal/ventanas: advertencias

- **El ledger es global del usuario (`~/.loom/ventanas.jsonl`):** no se resetea borrando `.loom/` del proyecto.
- **El uso real de claude sale del endpoint del `/usage` de Claude Code (`CuentaClaude`, `api/oauth/usage`) con el token de `~/.claude/.credentials.json` (llavero en macOS):** no está documentado; si cambia, degrada con `Motivo` y el panel lo dice. Nunca se renueva el token: rotar el refresh token cerraría la sesión del CLI. Caché de 5 min en `~/.loom/cuenta-claude.json`; un 429 fija `Reintento` (o `Retry-After`) y ni `--probe` lo salta. Nunca un `/v1/messages` para medir: gasta tokens y con API key no trae las cabeceras `unified`.
- **Codex no tiene consulta: `CuentaCodex` lee `rate_limits` de su última sesión (`~/.codex/sessions`).** Es una foto; una ventana cuyo reinicio ya pasó se muestra en 0 %. Opencode no tiene medidor de cuenta.
