# internal/esqueleto: advertencias

- **`Problemas` verifica el plano sin modelo y mira la forma, no si el reparto es bueno.** No corre los `listo_cuando`, salvo `integracion`: si falla hoy, una tarea la tiene como `listo_cuando`; si ya pasa, es regresión y ninguna puede usarla. Un cambio solo visual no lleva tarea final.
- **Archivos que no son código (`EsCodigo` falso: plantillas, estilos, datos) nunca son stub:** se tratan como existentes, con `Casos:` en `como`, y no se exige que el arquitecto los cree.
- **Una corrección en sesión puede contestar `{"unchanged": true}` (`SinCambios`):** `planearEsqueleto` reusa el JSON de la vuelta anterior (`jsonPrevio`).
- **La marca y `Casos:`/`Idea:` se exigen solo en archivos nuevos respecto de `Verificacion.Base`:** un cambio a un archivo existente exige `Casos:` en `como`. `Idea:` y `Casos:` son literales (`Casos (con mocks):` no pasa).
- **`Idea:` lleva lo pensado por el arquitecto (`contrato`):** algoritmo, estructuras y trampas en 1 a 8 líneas, sin código. Con eso una tarea es un módulo entero. Sin verificar con agentes reales.
- **Un plan con tareas de interfaz en un proyecto web exige `pantallas.script`.**
- **Un ciclo de verdad o un manifiesto que no instala es un problema para el arquitecto, no un aborto:** entra a la lista y se corrige en la siguiente vuelta.
- **El contrato del stub va en comentarios de línea (`//`, `#`); un stub con `/**` es problema (`contrato`), y un `tsconfig` con opciones deprecadas también (`config.TsconfigObsoleto`):** el prompt lo pide, pero se fuerza por código.
- **Cada carpeta de código nueva declara para qué es (`sinProposito`, con `graph.Doc`):** un archivo con encabezado de una línea; lo lee el mapa y el siguiente agente. Carpetas con código previo, la raíz y andamio (`topo.EsAndamio`) no se exigen.
- **Un archivo en una carpeta `migrations` no es stub (`EsCodigo`):** una migración que lanza la marca tumba la base de pruebas de todas las tareas. Va como archivo existente, con `Casos:` en `como`.
- **Un `only_touch` en zona vedada es problema del plano (`Verificacion.Zonas`, `gate.AlcanceProhibido`):** `sanearAlcance` lo quita al repartir y el stub quedaría sin dueño. El prompt lista las zonas (`Pedido.Zonas`).
- **Más de `TopeTareas` (8) tareas se devuelve una sola vez (`Demasiadas`, en la vuelta 0 de `planearEsqueleto` y sin respuesta guardada):** si el arquitecto mantiene su reparto, se acepta; no es un tope duro. Sin verificar con agentes reales.
