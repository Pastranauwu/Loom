# internal/graph: advertencias

- **`Update` compara contra el árbol de trabajo, no contra HEAD:** `git diff --name-only <commit_sha>` más los untracked; el hash del contenido evita re-parsear lo que no cambió. Si el commit guardado ya no existe (rebase), reconstruye todo.
- **`imported_by` se recalcula entero en cada actualización (`enlazar`):** es barato y evita aristas viejas.
- **Los imports se guardan ya resueltos a la clave del destino (`claves`):** directorio en Go (solo del módulo del repo), ruta sin extensión en JS/TS/Python. Los externos se descartan.
- **`Resumen` es determinista y vacío si no hay archivos:** el prompt del planificador debe seguir siendo estable byte a byte.
- **Sin git o sin commits el grafo no se construye:** quien lo llame degrada en abierto (`resumenGrafo`).
- **Sube `version` si cambia lo que `parsear` extrae (`Doc`, símbolos, imports):** el hash del contenido no cambia y un grafo viejo nunca se re-parsearía; con otra versión se reconstruye entero.
- **`Doc` es una sola frase corta (`primeraFrase`):** viaja en cada prompt del planificador por `Resumen` y en `loom_map`; un párrafo entero multiplicaría tokens.
