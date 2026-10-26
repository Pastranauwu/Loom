# internal/examiner: advertencias

- **La veda de rutas de prueba se decide por tarea (`Examinable`), no por lenguaje ni proyecto:** solo se veda donde hay examen ciego que proteger. Una tarea sobre `package main` no se puede examinar y vedarle las pruebas la deja imposible. La esclusa y el bucle tienen que recibir la misma regla (`patronesPruebaTarea` en `run.go` y `check.go`).
- **Examina paquetes que todavía no existen:** el nombre sale del prefijo de `expone` (`numeros.Media(...)` → `numeros`). No examina `package main` ni Rust.
- **La suite resuelve sus propios imports (`importsFaltantes`):** contra el módulo y la stdlib. Lo que no se resuelve descarta la suite: mejor sin examen que con uno roto.
- **El examinador escribe la prueba de las tareas del esqueleto (`task.ExamenEsqueleto`, `contratoDelEsqueleto`):** la tarea sale con `examen_esqueleto: true`, `examen_visible: <ruta>` y sin la prueba en `tocar_solo`. Recibe solo firma y `Casos:` de cada stub (`casosYFirma`) y corre en un directorio vacío, no en el cuarto.
- **El lenguaje sale de la extensión de los archivos de la tarea antes que del repo (`lenguajeDeTarea`):** en un monorepo `DetectLanguage` da `""`, que se leía como Go. `pruebaVisibleDeTarea` acepta la prueba relativa a un `cd` o `--prefix`.
- **Sin examen, el implementador recupera la prueba y se le avisa en el primer prompt:** lenguaje sin soporte, `package main`, contrato ilegible, import relativo que no existe o suite que no parsea.
- **El bool de `Run` es "la prueba visible es del examinador", no "se selló":** la visible se escribe y commitea antes de juzgar la oculta; si ya existe no se vuelve a pagar (es suya si su primer commit es `exam: suite visible`).
- **Su gasto va a `runs/<id>/examinador-usage.jsonl`** y cuenta en presupuesto y `usage`.
- **TS se valida con el `typescript` del cuarto y JS con `node --check`:** sin dependencias nuevas en loom.
- Examen de tareas del esqueleto y TypeScript: sin verificar con agentes reales.
