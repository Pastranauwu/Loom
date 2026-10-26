# loom: contexto para agentes

> **Este archivo se carga entero en cada sesión. Tope: 200 líneas.**
> - Una advertencia nueva va al `CLAUDE.md` de su paquete (`internal/<paquete>/`, `cmd/loom/`), no aquí. Regla, función y una línea de por qué; la anécdota va en el commit.
> - Lo que trae una versión va a `CHANGELOG.md`; aquí solo cambia el número.
> - Lo pendiente va a `docs/ROADMAP.md`. Cómo funciona cada pieza, a `docs/ARQUITECTURA.md`. Cómo llegó hasta aquí, a `docs/HISTORIA.md`.
> - Aquí solo entra lo que, si se borra, hace que un agente rompa algo en cualquier parte del repo.

---

## 1. Qué es

**loom** es una herramienta de terminal en Go (1.22+, binario estático sin CGO) que convierte **requerimientos declarados como código** en cambios verificados: orquesta varios agentes de IA en paralelo sobre un mismo repositorio y garantiza que a la rama principal solo llegue código probado y con historial legible.

La interfaz del producto es `loom.spec.yml`, no el agente.

```text
loom.spec.yml (QUÉ debe existir: requirements, rules, acceptance, constraints)
        ↓
Esqueleto (modelo grande: estructura, ARCHITECTURE.md, interfaces y stubs con Casos:)
        ↓
Contratos de tarea (.loom/tasks/*.md · una tarea de relleno por stub)
        ↓
Agentes en paralelo (cuartos aislados con git worktree)
        ↓
Verificación por tarea (listo_cuando + esclusas)
        ↓
Integración y aceptación del feature (comandos globales del spec)
        ↓
PR
```

### Principios rectores
1. **El historial del agente no es el historial del proyecto:** los `wip:` se aplanan en Conventional Commits con trailer `Agent: <modelo>`.
2. **Nadie toca lo que no reclamó:** lo editado fuera de `tocar_solo` o en pruebas vedadas se revierte antes de verificar.
3. **Verificar es trabajo de código, no del modelo:** el oráculo es `listo_cuando`, un comando que sale con 0 o no. Sin comando ejecutable, la tarea no existe.
4. **Lo que no se hizo, se declara:** handoff determinista desde el contrato y el diff.
5. **Paralelo para trabajar, en fila para entregar:** integración y `ship` son secuenciales.
6. **Sin debates entre agentes:** la coordinación se mide desde los artefactos (`attempts.jsonl`).

---

## 2. Pila

- **Go 1.22+**, `spf13/cobra`, `charmbracelet` (bubbletea, lipgloss, bubbles, huh). Estética sobria, acento único `#4FB3A2`, sin emojis. Toda salida soporta `--plain` y `--json`.
- **`git` nativo** y `gh` para PRs; sin remoto `origin`, entrega local en la rama `loom/_entrega`.
- **Ejecutores** (`internal/executor`): `claude`, `opencode` y `codex`, siempre en modo headless, con herramientas por rol (`executor.Rol`). Los comandos exactos están en el código de cada adaptador.
- **Dependencias:** cero frameworks pesados, cero servidores escuchando en red. Una sola de parseo (`yaml.v3`); el resto, stdlib.

---

## 3. Mapa del repo

```
cmd/loom/        # Comandos de la CLI (cobra)
  up.go              #   orquestador: prepara → plan → run → ship
  esqueleto.go       #   planearEsqueleto: el arquitecto y su verificación
  plan.go apply.go completar.go   # plan, spec → contratos, spec incremental
  run.go ship.go     #   despacho por oleadas y entrega
  reparar.go archive.go limpiar.go task.go board.go doctor.go init.go ...
internal/
  task/              # Contrato de tarea (frontmatter + notas)
  state/             # Estados: pendiente, en_curso, lista, detenida
  spec/              # Spec humano (yaml.v3), ValidatePlan, feature.json, intencion.json
  plan/              # Planificación, saneamiento de alcance y dependencias
  esqueleto/         # Verificación sin modelo del plano del arquitecto
  room/              # Cuartos aislados (git worktree) y dependencias
  loop/              # Bucle de intentos, latido, reversión, verificación
  gate/              # Esclusa de entrada
  ship/              # Esclusa de salida y entrega
  executor/          # Adaptadores claude / opencode / codex
  examiner/ sealed/  # Examinador ciego y suite oculta
  revisor/           # Modelo que juzga el diff contra el contrato
  capturas/          # Capturas para la revisión visual
  overlap/           # Colisiones: textual, semántico, funcional
  historial/         # loom archive
  constitution/ skills/   # Texto que se inyecta en el prompt
  budget/ ventanas/ metrics/ standup/   # Presupuesto, ledger, métricas, parte
  config/ kv/        # Configuración y parser del frontmatter
  recurse/           # Subtareas recursivas
  tui/ ui/           # Vistas Bubble Tea y formateadores
```

Casi todos los paquetes y `cmd/loom` tienen su propio `CLAUDE.md` con sus advertencias. Léelo antes de tocar el paquete.

---

## 4. Conceptos (detalle en `docs/ARQUITECTURA.md`)

- **Contrato de tarea** (`.loom/tasks/T-00N.md`): `listo_cuando`, `tocar_solo`, `no_tocar`, `depende_de`, `expone`, `usa`, `peso`, límites. Es el IR; el spec humano es el source.
- **Esqueleto:** el arquitecto escribe el plano en código (stubs con firma, `Idea:` y `Casos:`, cuerpo que lanza `loom: sin implementar`) y `esqueleto.Problemas` lo verifica sin modelo. Sobre código existente no hay stubs: se cambia lo que ya funciona.
- **`spec.ValidatePlan`:** análisis estático del grafo antes de gastar tokens. Errores abortan; advertencias solo se imprimen.
- **Esclusa de entrada** (`gate`): 6 chequeos; el clave es que `listo_cuando` sea ejecutable y **falle hoy**.
- **Esclusa de salida** (`ship`): base, historial, ruido, secretos, presupuesto, interfaces, dependencias, bisectable, handoff, pr. En `ship --todas` se suman `integradas` y `aceptación` sobre el conjunto.
- **Bucle** (`loop`): prompt → agente → reversión de alcance → commit `wip:` → `listo_cuando` → revisor. Latido cada 15 s; rancio a los 90 s.
- **Aceptación:** con `command` es compuerta determinista; textual solo entra al prompt del planificador.

---

## 5. Estado

Versión actual: **v1.11.5**. Historial por versión en `CHANGELOG.md`.

`go test ./...` y `go vet ./...` pasan. Eso cubre pruebas unitarias; no significa que cada flujo esté probado con agentes reales.

**Verificado de punta a punta con agentes reales (v1.0):** `loom up "<petición>" --agentes 3 --ship` sobre un repo Go vacío, más `init`, `doctor`, `check`, `plan --aprobar`, `run`, `ship T-00N`, `ship --todas`, `board`, `report`, `standup`. El esqueleto con pruebas del arquitecto corrió en un monorepo Python + frontend.

**Solo con pruebas unitarias:** el esqueleto plano (cada tarea escribe su prueba desde `Casos:`), la mezcla opencode free + claude, el spec incremental, la revisión visual, `Idea:` y el prompt de sistema propio, codex con una tarea real, `archive` en un flujo completo, `Obsoleto:`, el examen ciego de tareas del esqueleto y TypeScript, `reparar`, `pruebaComplaciente`.

---

## 6. Decisiones de alcance (no reabrir)

- **El examinador ciego cubre Go, Python y, en tareas del esqueleto, TypeScript/JavaScript.** Rust queda fuera (exige su toolchain). Una tarea Node con `expone` y sin esqueleto sigue sin examen.
- **Sin Homebrew:** `scripts/install.sh`, releases de GitHub (GoReleaser) y `go install`.
- **El campo del CLI es `cli`, no `ejecutor`.**
- **Hay dos parsers y cada uno tiene su territorio:** el spec humano con `yaml.v3` (`internal/spec/yaml.go`); el frontmatter de contratos y `config` con `internal/kv`. No migres uno al otro ni agregues una tercera librería. En `kv` las claves repetidas a distinta profundidad se pisan.
- **La aceptación textual no es compuerta:** fingir determinismo a partir de coincidencia de palabras sería peor que declarar el límite.
- **Sin debates ni auto-reportes de agentes:** el standup se calcula desde los artefactos.
- **Docker por defecto solo en proyectos generados desde cero;** se apaga con "sin docker" en el spec. Sobre un repo con código no se exige ni se verifica, salvo "con docker".

---

## 7. Reglas que aplican en todo el repo

- **Quien decida verde por código de salida pasa por `loop.SinPruebas`:** un comando que sale con 0 sin correr pruebas no es verde.
- **Quien ejecute `listo_cuando` o `pruebas` pasa el comando por `task.SinTerminal`:** corren sin stdin; una herramienta que pregunta (Django y su base de pruebas huérfana) muere con EOFError.
- **El agente nunca decide si terminó, y nada nuevo debe depender de que un prompt se obedezca:** lo que importa se fuerza por código (peso de tareas de interfaz, script de flujos, verificación del esqueleto).
- **Lo que no se puede verificar degrada en abierto y deja rastro:** se avisa y se sigue; nunca se omite en silencio.
- **Trabajo pagado no se tira:** las respuestas del planificador y del arquitecto se guardan y se reusan si falla algo después.
- **Chequeos e issues se buscan por nombre o `Code`, nunca por índice.**
- **Usa `room.Dir(root)`, nunca la ruta de un cuarto a mano.**
- **Mensajes de error:** minúscula, sin punto final, sin disculpas, qué ocurrió y qué hacer (`tarea rechazada · listo_cuando no ejecutable · edita T-001 y reintenta`).
