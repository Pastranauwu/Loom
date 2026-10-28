# Arquitectura de loom

Cómo funciona cada pieza. El resumen y las reglas viven en `CLAUDE.md`; las advertencias de cada paquete, en su propio `CLAUDE.md`.

## Tareas como Código y el Contrato de Tarea
Las tareas se almacenan en `.loom/tasks/T-00N.md`.
```yaml
---
version: 1
id: T-001
titulo: exportar clientes a CSV
porque: soporte pierde 3h/semana copiando a mano
listo_cuando: npm test -- export.spec.ts     # OBLIGATORIO: ejecutable, debe fallar hoy
tocar_solo: ["src/export/**"]                # Globs autorizados
no_tocar: ["src/auth/**", "migrations/**"]   # Globs restringidos
depende_de: ["T-000"]                        # Tareas que deben estar listas antes
expone: ["export.ToCSV(rows []Row) []byte"]  # Firmas públicas prometidas
usa: ["config.Load(p string) error"]         # Firmas consumidas de otras tareas
peso: liviana                                # liviana (haiku) | media (sonnet) | pesada (opus)
agente: backend                              # Arquetipo o rol
limite_intentos: 3
limite_lineas: 200
---
Notas libres: Enfoque sugerido para el ejecutor.
```

## Requirements as Code: el Spec Humano (`loom.spec.yml`)

Hay **dos niveles de especificación**, y no son alternativas sino un pipeline: el spec humano es el source, los contratos de tarea son el IR generado.

```text
loom.spec.yml        → source humano (QUÉ debe existir)
.loom/tasks/*.md     → task IR generado (CÓMO ejecutar y verificar)
código + commits         → resultado de ejecución
```

### Nivel 1: Spec Humano (la interfaz recomendada)
```yaml
feature: recuperación de contraseña

requirements:            # lista simple o categorías YAML anidadas
  functional:
    - solicitar recuperación por email
    - cambiar contraseña usando el token
  security:
    - token de un solo uso

rules:                   # también acepta `reglas`; se inyectan en las notas de cada prompt
  - no modificar sesiones

acceptance:              # también `aceptacion`
  - token expirado debe rechazarse          # textual: entra al razonamiento del plan
  - criterion: integración completa         # con comando: oráculo determinista al integrar
    command: go test ./... -run TestPasswordRecovery

constraints:
  no_tocar:
    - internal/session/**

ship: true               # `up` llega hasta la entrega
```
Con `requirements` y sin `tasks` (y con `up "<petición>"`), `completarSpec` delega en `planearRequirements` → `planearEsqueleto` (`cmd/loom/esqueleto.go`). Ver "El Esqueleto".

**Dos formas de `acceptance` con garantías distintas:**
- **Textual:** entra en el prompt del planificador y debe quedar cubierta por el plan. Por sí sola **no** es compuerta determinista; sin cobertura reconocible sale como advertencia.
- **Con `command`/`comando`:** se guarda en `.loom/feature.json` (`spec.SaveFeatureState`) y corre sobre la rama donde ya se integraron todas las tareas. Salida distinta de cero frena la entrega.

`constraints.no_tocar` se aplica en `Apply`, no en `Parse`: los globs se suman al `no_tocar` de **todo** contrato que se vaya a escribir, venga del YAML o lo haya generado el planificador. La restricción del humano sobrevive el viaje spec → IR.

Campos `architecture` y `delivery` están **reservados**: el parser los acepta y los ignora sin error.

### Nivel 2: Tasks escritos a mano en el spec (interfaz avanzada, no desapareció)
1. **Modo Rápido:** Una sola línea por tarea (`- enviar magic packet por udp`). El planificador completa `listo_cuando`, `tocar_solo`, dependencias y firmas, respetando siempre lo que el humano haya escrito.
2. **Modo Completo:** Lista detallada con límites globales, reglas comunes, agentes y configuración de `ship`.
3. **Resolución de dependencias relativas:** Si no hay IDs asignados, `depende_de: ["T-001"]` se interpreta relativo a ese spec sin colisionar con tareas preexistentes en el repo.

### El Esqueleto (`internal/esqueleto`, `planearEsqueleto`)
El modelo grande no reparte prosa: escribe el **plano en código**, sin lógica ni pruebas (cada línea suya la paga el modelo caro; en una corrida real, el arquitecto que además escribía pruebas y cableado fue el 70% del gasto). Corre con el modelo `planificador`, herramientas de escritura, en el cuarto de la primera tarea (`T-00N`), y deja:
- estructura, manifiestos, `.gitignore` y dependencias instaladas;
- `ARCHITECTURE.md` en la raíz: módulos, responsabilidades, dependencias permitidas, flujo de datos. Es la fuente de verdad que el siguiente cambio **evoluciona** (el prompt se lo pasa si ya existe);
- tipos, entidades, interfaces y puertos solo como **declaraciones**;
- todo lo demás como **stub**, también el cableado: firma exacta, comentario de contrato (entradas, salida, errores, a quién llama y quién lo usa, y `Casos:` con 2 a 5 ejemplos) y cuerpo que lanza `loom: sin implementar` (`esqueleto.Marca`).

**Sobre código existente no hay stubs:** lo que ya funciona se cambia, no se reemplaza. Una tarea que toca archivos que ya estaban en el commit base lleva su contrato con `Casos:` en `como`, su `listo_cuando` corre una prueba nueva del comportamiento nuevo y recibe `notaCambio` en vez de `notaRelleno` (`notaPara` decide por la marca). El prompt manda respetar el stack, las librerías y el sistema de diseño que ya hay.
- **Docker por defecto:** un Dockerfile por servicio, `compose.yaml` en la raíz (`docker compose up --build` levanta todo), `.env.example` y `.dockerignore`. Se apaga escribiendo "sin docker" en el spec (una librería o un CLI no se despliegan). Solo aplica a un repo vacío: sobre código existente no se exige ni se verifica, salvo que el spec diga "con docker".
- **Interfaz web:** el sistema de diseño que el proyecto ya tenga; solo si no hay, Tailwind y la librería estándar de su framework instaladas con su CLI (componentes generados, no tokens).

Las pruebas las escribe **cada tarea de relleno** a partir de los `Casos:` de su contrato, con fakes para las dependencias (así el relleno es una sola ola en paralelo). Una tarea final escribe la prueba de punta a punta: depende de todas y su `listo_cuando` es `integracion`.

Responde un JSON con `verificar` (build/typecheck que pasa con los stubs), `integracion` (la prueba de punta a punta), `pruebas` y `tareas` (una por stub, con el stub y su archivo de prueba en `tocar_solo`). **`esqueleto.Problemas` lo verifica sin modelo:** existe `ARCHITECTURE.md`, `verificar` pasa, cada tarea tiene `listo_cuando` y `tocar_solo`, cada archivo no-prueba de `tocar_solo` existe, trae al menos un stub con la marca y un `Casos:` (`contrato`), y alguna tarea tiene `integracion` como `listo_cuando`. La marca y los `Casos:` del archivo se exigen solo en archivos **nuevos** respecto de `Verificacion.Base` (el commit con que arrancó el cuarto); un cambio a un archivo existente exige `Casos:` en `como`. Con docker: `compose.yaml`, `.dockerignore` y, si docker está instalado, `docker compose config -q`. No corre los `listo_cuando` (las pruebas todavía no existen), salvo `integracion`: si falla hoy, una tarea tiene que tenerla como `listo_cuando`; si ya pasa, es la regresión de un proyecto existente y ninguna tarea puede usarla (la esclusa la rechazaría por "ya pasa"). Un cambio solo visual deja `integracion` como regresión sin tarea final; exigir siempre una tarea final contradecía la regla anterior y el arquitecto no podía cumplir las dos. Si algo falla, el arquitecto recibe la lista con el mismo prompt delante (caché) y corrige en el mismo cuarto (con claude continúa su propia sesión por `--resume` y solo recibe la lista), hasta `correccionesEsqueleto` veces. `room.Ensure` reusa el cuarto: un esqueleto cortado se corrige, no se rehace.

Al pasar: commit `wip: T-00N esqueleto`, estado **lista** y contrato con `listo_cuando: <verificar>`. Cada stub es una tarea de relleno con `depende_de: [T-00N]`, **sin `expone`/`usa`** (las firmas las valida el compilador) y la nota `notaRelleno`. `verificar`, `integracion` y (con docker instalado) `docker compose build` entran como `acceptance.command`. `run` integra el esqueleto por `sembrarVerdesPrevias`, igual que cualquier verde previo. Log del arquitecto en `.loom/runs/T-00N/esqueleto-N.log`.

### Análisis Estático del Task Graph (`spec.ValidatePlan`)
Corre en `Apply` sobre el IR ya con IDs, **antes** de escribir tareas o gastar implementación. Los `Issue{Level:"error"}` abortan el `Apply` completo; los `"warning"` solo se imprimen.

Errores (bloquean): `cycle` (dependencia circular), `missing_dependency`, `orphan_interface` (`usa` que nadie expone), `incompatible_interface` (existe una firma expuesta con el mismo nombre pero distinta signatura), `duplicate_interface` (dos tareas exponen lo mismo), `write_overlap` (globs de `tocar_solo` que se pisan).

Advertencias (no bloquean): `requirement_coverage`, `acceptance_coverage` (no se reconoce cobertura textual en títulos/motivos/notas/comandos) e `integration_test` (varias tareas y ninguna aceptación global). La cobertura se estima por coincidencia de palabras de más de 4 letras: señala huecos evidentes, **no** demuestra corrección semántica.

## La Doble Esclusa

### Esclusa de Entrada (`internal/gate`)
Antes de invocar al agente o gastar un token:
1. Contrato válido (`version: 1`, campos reconocidos).
2. `listo_cuando` es ejecutable y **falla hoy** (si ya pasa, la tarea carece de sentido).
3. `tocar_solo` no se solapa con tareas activas en la misma oleada.
4. `tocar_solo` no contiene zonas prohibidas (`migrations/**`, `go.sum`, CI, etc.).
5. `tocar_solo` no apunta a archivos de pruebas (evita que el agente altere las pruebas para falsear el verde).
6. No tiene `usa` huérfanos (no consume firmas que ninguna otra tarea exponga).

### Esclusa de Salida (`internal/ship`)
Secuencia determinista de 9 a 10 pasos en `loom ship`:
1. `base`: Rebase limpio sobre la rama base.
2. `historial`: Aplana los commits `wip:` internos en Conventional Commits con trailer `Agent: <modelo>`.
3. `ruido`: Escaneo de prints de debug (`console.log`, `fmt.Println`), bloques de código comentado y archivos temporales.
4. `secretos`: Detección de claves privadas, API keys y credenciales en claro.
5. `presupuesto`: Comprueba que las líneas y archivos respeten `limite_lineas`.
6. `interfaces`: Comprueba que el diff realmente implemente lo prometido en `expone`.
7. `dependencias` (condicional): Valida el grafo de imports según `reglas_import`.
8. `bisectable`: Comprueba que cada commit compile y pase la suite de pruebas individualmente.
9. `handoff`: Genera un informe determinista (qué cambió, qué NO se hizo, riesgos y verificación).
10. `pr`: Publica en GitHub (`gh pr create`) o entrega en rama local `loom/_entrega` con descripción en `.loom/pr/`.

En la entrega conjunta (`ship --todas` → `ship.EntregarTodas`) la secuencia sobre el resultado integrado es: esclusa por tarea → integración commit por commit → paso `integradas` (suite `pruebas` del proyecto) → paso **`aceptación`** (un paso por cada `acceptance.command` del spec, leído de `.loom/feature.json`) → `pr`. El primer comando de aceptación que falle corta la entrega. Este paso **solo existe en `--todas`**: el `ship` de una tarea suelta no tiene conjunto integrado que aceptar.

## Aislamiento y Bucle de Trabajo (`internal/loop`, `internal/room`)
- Cada tarea corre en un cuarto aislado: `~/.loom/rooms/<proyecto>-<hash>/T-00N/` (`room.Dir`, fuera del árbol del proyecto) montado mediante `git worktree` sobre la rama `loom/T-00N`.
- En cada intento:
  1. Se inyecta en el prompt, primero lo común y después lo de la tarea: `skills.Base` (cómo trabajar, código limpio resumido, no narrar), Constitución, reglas, arquitectura común; luego las skills que la tarea declara en `skills:` (o las de su rol si el contrato no trae el campo), Contrato, Interfaces expuestas/usadas, árbol y firmas recortados, Presupuesto y el error/diagnóstico del intento anterior.
  2. El agente (`claude` u `opencode`) ejecuta modificaciones.
  3. `revertFueraDeAlcance` revierte cualquier archivo modificado fuera de `tocar_solo` o dentro de `patrones_prueba`.
  4. Se crea un commit de respaldo `wip: <id> intento <n>`.
  5. Se ejecuta `listo_cuando`.
  6. Si pasa: el `revisor` (modelo evaluador de intención) revisa el diff. Si pide cambios, el intento se marca rojo y su crítica entra en el siguiente prompt.
  7. Si falla: se capturan las últimas líneas del error y se reintenta hasta `limite_intentos`.
- **Latido (Heartbeat):** Una goroutine refresca `latido.json` cada 15 segundos. Si un proceso muere por SIGKILL (suspensión de laptop, caída de SSH), el latido queda rancio tras 90 segundos y el sistema lo detecta como corrida interrumpida/muerta, permitiendo retomarla con `run --reintentar`.

## Detección de Solapamiento en 3 Niveles (`internal/overlap`)
1. **Nivel 1 (Textual):** `git merge-tree` entre pares de ramas activas.
2. **Nivel 2 (Semántico):** Símbolos exportados modificados en común (extraídos de `attempts.jsonl`).
3. **Nivel 3 (Funcional):** Corre **después** de la oleada sobre pares verdes sospechosos. Monta la fusión en un worktree temporal y corre los `listo_cuando` de ambas tareas para atrapar el escenario crítico: **dos ramas verdes por separado que rompen al juntarse**.

## Examinador Ciego y Suite Oculta (`internal/examiner`, `internal/sealed`)
- Implementado para **Go** y **Python**.
- Antes de que el agente implemente, un examinador genera pruebas basadas únicamente en la frontera pública (`expone`).
- El 70% de las pruebas queda visible para el agente en el cuarto; el 30% restante se sella con hash en `.loom/sealed/<id>/` en el repositorio principal y corre solo una vez en la esclusa de salida.

## Presupuesto y Ventanas Rodantes (`internal/ventanas`, `internal/budget`)
- Los proveedores de IA tienen límites de gasto en ventanas de 5 horas, semanales y mensuales.
- loom mantiene un ledger en `~/.loom/ventanas.jsonl` (global a la máquina, compartido entre proyectos). Si una corrida va a superar el presupuesto configurado, se detiene con advertencia antes de agotar cuota.

## Ejecución en Segundo Plano (`--fondo`)
- Disponible en `loom up --fondo` y `loom run --fondo`.
- Realiza las preguntas interactivas primero; una vez listo el entorno, se desprende de la terminal (`setsid` en Unix, flags de proceso desacoplado en Windows) y redirige la salida a `.loom/corridas/<fecha>.log`.
- Sobrevive al cierre de terminal o pérdida de conexión.

## Puentes (`loom wrap`)
- Fuente → manifiesto: `--cli` lee `--help` (o `man -P cat`, 16 KB), `--curl` y `--har` se reducen a llamadas con forma de cuerpo y respuesta (secretos de encabezados como `{variable}`); las tres pasan por el planificador (`wrap.Prompt`/`wrap.Parse`, respuesta guardada en `.loom/wrap-crudo.json`). `--openapi` se convierte por código (`wrap.FromOpenAPI`): solo parámetros requeridos y campos escalares requeridos del cuerpo.
- Compuerta (`wrap.Verify`) antes de guardar: nombres `[a-z0-9_]`, un solo `exec` o `http` por herramienta, `bin` simple y fuera de la lista de shells e intérpretes, toda `{var}` es propiedad requerida del `inputSchema`, url http(s); prueba de humo `--version`/`--help` (binario ausente rechaza, mudo avisa).
- Ejecución (`wrap.Invoke`): tipos del schema comprobados, sustitución en una pasada y por elemento del argv; en http, la url escapa con `QueryEscape` y el cuerpo con escape JSON.
- `loom mcp` relee `.loom/bridges/*.json` en cada petición (`mcp.Serve(..., dyn)`); una herramienta dinámica nunca pisa a una estática.

## Sandbox de verificación (bwrap)
- Todo `listo_cuando`, `pruebas`, aceptación y ataque del red team corre por `task.Shell`/`task.Argv`. Con bwrap usable (sonda única): raíz en solo lectura, `/tmp` limpio, el cuarto y el `.git` común en escritura, cachés del usuario (`~/.cache`, `~/.npm`, `~/.cargo`, `~/go`...) montadas, y `--unshare-net` salvo `network: true` en la tarea. El esqueleto y la entrega conjunta conservan la red (instalan y compilan todo).
- Sin bwrap, o con user namespaces bloqueados, corre nativo en el cuarto con un aviso. `sandbox: off` en `config.yml` lo apaga; `loom doctor` dice el estado.

## Docker solo en release
- El bucle diario y `ship` normal nunca construyen imágenes. `loom ship --all --release` (o `--tag <versión>`, que además deja un tag anotado local) suma el paso `docker` (`docker compose build`) sobre el conjunto integrado, fuera del sandbox.
