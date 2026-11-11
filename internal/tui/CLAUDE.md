# internal/tui: advertencias

- **Los colores salen del tema (`AplicarTema`, `Temas`):** `root` lo aplica en cada comando desde `~/.loom/theme`. Nada de hex sueltos en la TUI: usa `presion`, `apagado`, `superficie`, `borde`, `temaActual.Fondo`.
- **El mapa cambia solo de vista (`EnObra`):** con algo pendiente, en curso o detenido, tarjetas con tareas (`VistaMapa`); con todo listo, el bosquejo por capas (`VistaArquitectura`), sin pruebas ni migraciones (`esAndamio`).
