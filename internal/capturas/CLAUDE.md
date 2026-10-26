# internal/capturas: advertencias

- **Lo visual lo juzga quien ve capturas, no las pruebas:** pruebas que solo miran clases CSS salen verdes sin que la pantalla cambie. El arquitecto declara `pantallas` (`levantar`, `url`, `rutas`), guardadas en `config.yml` (`pantallas_*`). Las ve el arquitecto antes de planear (`runs/T-00N/antes/`), el revisor visual por tarea de UI (`runs/T-00N/visual-N/`, `loop.TopeVisual` = 2) y el PR (`.loom/pr/capturas/`).
- **Todo degrada en abierto:** sin navegador (`Navegador`: chrome-headless-shell de Playwright o chromium/chrome; Brave se cuelga en headless) o si la app no levanta, se avisa y se sigue.
- **Cada ruta se captura en celular y escritorio (`Tamanios`), con datos (`Pantallas.Semilla`) y flujos (`Pantallas.Script`):** el script deja PNG en `$CAPTURAS` y recibe `$BASE_URL`. Lo del arquitecto se suma con `Pantallas.Completar`; la config manda.
- **El script de flujos se prueba antes de repartir (`ProbarFlujos`):** su error vuelve al arquitecto. En las capturas normales, un script que falla se descarta entero. Los PNG de `flujos-prueba-N` se borran: en un proyecto nuevo son stubs en blanco.
- **Las capturas mandan sobre el código y son actuales:** si la captura no muestra lo que el código promete, el código está mal. Los prompts lo dicen.
