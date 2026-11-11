# internal/plan: advertencias

- **El planificador numera con los ids reales (`Contexto.PrimerID`) y ve el `expone` previo (`Expuestas`):** sin eso numera desde donde quiere y `traducirDependencias` lo lee por posición: plan lleno de ciclos. La traducción por posición solo aplica a ids que no son del plan ni previos.
- **`Parse` escapa saltos de línea crudos dentro de strings (`EscaparControles`):** los modelos escriben párrafos reales en vez de `\n`. Fixture: `testdata/plan-saltos-crudos.txt`.
- **Un plan sin tareas es válido (`ErrSinTareas`):** "no hay nada que cambiar" se acepta y se descarta el cuarto. Con capturas, lo confirma el revisor visual.
- **`RecortarArquitectura` deja árbol y firmas en lo que la tarea toca:** `tocar_solo`/`usa`/`expone`.
- **El JSON de una respuesta se saca con `CuerpoJSON`, nunca desde el primer `{`:** el modelo narra antes de responder y la prosa trae llaves; cada falso "JSON inválido" es una ronda pagada.
- **El planificador y el arquitecto piden pocas tareas grandes (2 a 6 por feature, por módulo y no por archivo):** cada tarea paga arranque, revisor y reintentos en cada intento. Antes pedían muchas chicas por paralelismo. Sin medir con agentes reales.
