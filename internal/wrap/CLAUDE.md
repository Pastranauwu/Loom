# internal/wrap: advertencias

- **Un puente nunca pasa por una shell (`verificarExec`, `correr`):** `exec.Command(bin, args...)` y sustitución por elemento; `bin` no lleva variables ni puede ser un intérprete (`interpretes`). Agregar un intérprete a esa lista es seguro; quitar uno, no.
- **Un valor que abre su token no empieza con `-` (`correr`, `permiteGuion`):** se leería como bandera del binario. Pasa tras `=` o un prefijo de la plantilla (`--out={v}`) o con `allow_leading_dash: true` en la propiedad. El mensaje es fijo; las pruebas lo comparan.
- **Toda `{var}` es propiedad requerida del `inputSchema` (`Verify`):** una opcional quedaría como `{var}` literal en el argv o la url. Lo opcional es otra herramienta.
- **La sustitución es de una pasada (`sustituir`):** un valor con `{otra}` no se vuelve a expandir. En http la url usa `QueryEscape` y el cuerpo escape JSON; la plantilla pone las comillas.
- **Los secretos de encabezados no llegan al modelo (`Request.header`, `secretos`):** se cambian por `{nombre}` antes del prompt.
- **OpenAPI no gasta tokens (`FromOpenAPI`):** si el converter no alcanza, se arregla el converter, no se manda el spec al modelo.
