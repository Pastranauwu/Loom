# internal/spec: advertencias

- **`ValidatePlan` distingue error de advertencia y solo el error aborta:** búscalos por `Code`, nunca por índice. La cobertura se estima por palabras de más de 4 letras: no es prueba semántica. Un requerimiento cuyo id está en algún `cubre` ya cuenta como cubierto.
- **`usa` se compara contra `expone` en forma canónica (`firmaCanonica`):** sin `def`/`func` al inicio y sin la lista de bases de una clase. Los tipos sí cuentan: `Iterator[Session]` contra `Generator[Session]` es error.
- **`write_overlap` no aplica entre tareas encadenadas por `depende_de`:** corren en fila; el esqueleto crea los stubs que después rellena cada tarea.
- **`constraints.no_tocar` se aplica en `Apply`, nunca en `Parse`:** `planearRequirements` agrega contratos después de parsear y `Apply` es el único punto por donde pasan los dos orígenes (`TestApplyPropagaConstraintsAlIRGenerado`).
- **La aceptación del feature vive en `.loom/feature.json` (`SaveFeatureState`/`LoadFeatureState`), no en los contratos:** solo las que tienen `command` son compuerta (`AcceptanceCommands`). Borrar `.loom/` borra la compuerta global.
- **Ids de requerimiento (`Spec.IDs`, `task.Cubre`):** sin id explícito es `R-` + sha1 corto del texto normalizado; con `- id:` + `texto:` sobrevive a una reescritura. `Intencion.Igual` completa los ids faltantes antes de comparar.
- **El spec de requirements es incremental (`.loom/intencion.json`, `Intencion`):** mismo spec, no se llama al arquitecto. Solo aplica al modo requirements sin `tasks`, no a `up "<frase>"`. Borrarlo fuerza replanear todo. Un repo sin ese archivo usa `feature.json` como plan previo, sin comparar la aceptación.
- **`architecture` y `delivery` están reservados:** el parser los acepta y los ignora.
- **`AceptacionSinPrueba` avisa de la aceptación que nombra una prueba que no está en el repo ni en el `tocar_solo` de ninguna tarea:** archivo o id con puntos de Python; si existe con otro prefijo lo dice. Advertencia, no error (un proyecto puede mover su `sys.path`). Vive fuera de `ValidatePlan` porque necesita el repo. La usan `apply` y el paso `aceptación` de la entrega.
