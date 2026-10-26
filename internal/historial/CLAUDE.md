# internal/historial: advertencias

- **`archive` solo archiva lo probado:** exige `.loom/aceptacion.json` aprobado con todos los comandos en verde, y rechaza un spec editado después de probarse (`spec.Intencion.Hash`; motivo y comentarios no cuentan). Un criterio sin comando se anota "sin comando", no bloquea.
- **Escribe `.loom/historial/NNNN-<slug>/{spec.yml,resultado.yml}` y una línea en `.loom/index.md`,** deja el spec en 0 bytes y commitea en la rama actual. Se corre en la rama del PR antes del merge.
- **Al archivar se borran `intencion.json`, `feature.json` y `aceptacion.json`:** con la intención vieja el siguiente spec mandaría a borrar lo "quitado".
- **Antes de escribir comprueba con `git check-ignore` que el historial no quede ignorado.** Si el proyecto ignora `.loom/` entero, `abrirGitignore` cambia la línea por `.loom/*` con excepciones: con la carpeta ignorada git no mira adentro.
- Fuera por ahora: `--pr`, hook pre-push, `archivar: auto` y pasar el index al arquitecto.
