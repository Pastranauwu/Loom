# internal/gate: advertencias

- **`Run` devuelve 6 chequeos: búscalos por nombre, nunca por índice.** El 0 siempre es `contrato válido` (`Validate()`); todo contrato lleva `version: 1` (`task.Version`).
- **`listo_cuando` se lee como `sh -c`, igual que el bucle (`programas`):** se revisa el primer programa de cada comando simple y se saltan builtins (`cd`, `export`…). Mirar solo el primer token rechaza `cd backend && …` y deja pasar un programa inexistente detrás del `&&`.
- **Verde con una prueba nombrada que no existe no es "ya pasa" (`task.PruebaSinEscribir`):** vitest ignora el archivo que falta, corre el resto y sale 0.
- **Recibe los patrones de prueba de la tarea, los mismos que el bucle:** con los del proyecto, la tarea de integración (solo escribe pruebas) se rechaza por `sin rutas de prueba`.
