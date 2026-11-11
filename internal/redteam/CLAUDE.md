# internal/redteam: advertencias

- **El lenguaje se decide una vez (`lenguajeDe`) y viaja en `Informe.Lenguaje`/`Runner`/`Dir`:** `Reabrir` lo lee de ahí. Detectarlo otra vez en `--reopen` reabría tareas Python con un test Go.
- **Lo que deja sin ataque posible se decide antes de pagar al modelo (`Atacar`):** TS sin vitest/jest en `package.json`, Rust sin biblioteca (`examiner.CrateExaminable`).
- **Un ataque que no compila es culpa del atacante (`noCompila`):** el código atacado ya existe, así que en Rust cualquier error de `cargo --no-run` lo descarta (al revés que el examinador, que tolera símbolos faltantes). En TS solo cuentan los errores de `tsc` en el archivo del ataque: el proyecto puede traer los suyos.
- **TS y Rust corren con `npx --no-install` y un `CARGO_TARGET_DIR` temporal por corrida:** el ataque nunca instala ni ensucia el `target/` del repo.
- Redteam TS y Rust: sin verificar con agentes ni toolchains reales (cargo y npx falsos en las pruebas).
