// Package tutorial holds the guided walkthrough of loom for new developers,
// in English and Spanish.
package tutorial

import "strings"

type Pagina struct {
	Titulo string
	Cuerpo string
}

// Paginas returns the walkthrough in the given language ("es" or English).
func Paginas(es bool) []Pagina {
	if es {
		return paginasEs
	}
	return paginasEn
}

// Texto prints every page, for terminals without a TUI and for pipes.
func Texto(ps []Pagina) string {
	var b strings.Builder
	for i, p := range ps {
		b.WriteString(strings.Repeat("─", 3) + " " + p.Titulo + "\n\n" + p.Cuerpo)
		if i < len(ps)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

const flujo = `  you write                loom does                       you get
  ─────────                ─────────                       ───────
  loom.spec.yml    ──▶     plan ──▶ tasks (contracts)
  "what must exist"                  │
                                     ▼
                           agents work in parallel,
                           each in its own git worktree
                                     │
                                     ▼
                           gates verify each task
                           (tests, scope, noise, secrets…)  ──▶  one clean PR`

const flujoEs = `  tú escribes              loom hace                       tú recibes
  ───────────              ─────────                       ──────────
  loom.spec.yml    ──▶     plan ──▶ tareas (contratos)
  "qué debe existir"                 │
                                     ▼
                           los agentes trabajan en paralelo,
                           cada uno en su propio git worktree
                                     │
                                     ▼
                           las compuertas verifican cada tarea
                           (pruebas, alcance, ruido, secretos…) ──▶  un PR limpio`

var paginasEn = []Pagina{
	{"What loom is", `A loom weaves thousands of threads from a pattern someone designed on
punched cards. loom does the same with code.

  YOU design the pattern: a spec that says what must exist.
  loom weaves it: it splits the work, runs AI agents in parallel, and only
  lets code through when deterministic checks say it is right.

You never need to open an editor to start. You describe, loom builds, and
nothing reaches your main branch unless it passed every gate.

Why gates? AI agents hallucinate, touch files they should not, and say
"done!" while breaking things. loom never takes their word: a command that
exits with 0 is the only proof of done.`},
	{"The flow", flujo + `

The agents are workers, not the product. The product is your spec.`},
	{"Step 1 · say what you want", `Two ways to start:

  loom sketch "a job queue with retries"
      A model interviews you with four sharp questions about edge cases,
      then writes the spec for you.

  or write loom.spec.yml by hand:

      feature: job queue
      requirements:
        - enqueue a job and get an id
        - failed jobs retry 3 times, then go to a dead-letter list
      rules:
        - no third-party dependencies
      acceptance:
        - criterion: full flow
          command: go test ./... -run TestQueueFlow

requirements say WHAT. rules apply to everything. acceptance is how loom
checks the whole feature at the end (a command is a real gate).`},
	{"Step 2 · turn it into tasks", `  loom apply --dry-run     see what would be planned, spending nothing
  loom apply               plan it: the spec becomes task contracts

A task contract (.loom/tasks/T-001.md) is the unit of work:

  title        what it is
  done_when    a command that FAILS today and passes when the task is done
  only_touch   the only files the agent may change (the rest is reverted)
  exposes      the public signatures it must provide to other tasks
  depends_on   tasks that must finish first

If you edit the spec later, loom shows you the semantic diff and plans only
what changed. Your spec is living state, not a one-shot ticket.`},
	{"Step 3 · let the agents work", `  loom run                 run pending tasks in parallel
  loom board               live board: ready, running, stopped, pending

Each task runs in its own room: a separate git worktree. Agents cannot see
or break each other's work, and your working tree is never touched.

If a task fails, loom feeds the error back and retries. Past 70% of the
context window it starts a fresh session instead of dragging a full one.

  loom run --retry         bring stopped tasks back to life`},
	{"Step 4 · the gates and the PR", `Before anything is delivered each task passes the exit gate, in order:

  base · history · noise · secrets · budget · interfaces · dependencies ·
  dna · bisectable · hidden suite · handoff · pr

  loom ship T-001          deliver one task
  loom ship --all          integrate all ready tasks into ONE clean PR,
                           run your test suite on the whole set, and run
                           your acceptance commands

Green task != working feature. Three tasks can each pass and still not fit
together, so loom also tests the integrated result and adds a seam test.`},
	{"Understand your system", `  loom map                 modules, the contracts each one offers, who uses
                           whom, active DNA and gate state. No code opened.
  loom map --mermaid       the same as a diagram for Markdown or a PR

  loom dna                 pick the design style your agents must follow:
                           hexagonal, zero-deps, crash-early, functional-core
                           (some are enforced by the gate, not just asked)

  loom heal --from crash.log
                           paste a failure: loom finds the rule your spec was
                           missing, adds it, and fixes the code`},
	{"Cheat sheet", `  loom doctor              check your setup
  loom init                set up a repo (language, agent CLI, models)
  loom sketch "idea"       interview → spec
  loom up                  spec → tasks → run   (add --ship to deliver)
  loom board / map         see what is happening / the whole system
  loom ship --all          one clean PR
  loom heal --from log     fix from a failure
  loom language es|en      change the language you read (prompts stay English)

Fastest first run:   loom init  →  loom sketch "your idea"  →  loom up --ship

Anytime:  loom <command> --help    ·    loom tutorial`},
}

var paginasEs = []Pagina{
	{"Qué es loom", `Un telar teje miles de hilos siguiendo un patrón que alguien diseñó en
tarjetas perforadas. loom hace lo mismo con el código.

  TÚ diseñas el patrón: un spec que dice qué debe existir.
  loom lo teje: reparte el trabajo, corre agentes de IA en paralelo y solo
  deja pasar código cuando verificaciones deterministas dicen que está bien.

No necesitas abrir un editor para empezar. Describes, loom construye, y
nada llega a tu rama principal sin pasar todas las compuertas.

¿Por qué compuertas? Los agentes de IA alucinan, tocan archivos que no
deben y dicen "¡listo!" mientras rompen cosas. loom nunca les cree: un
comando que sale con 0 es la única prueba de que algo está hecho.`},
	{"El flujo", flujoEs + `

Los agentes son obreros, no el producto. El producto es tu spec.`},
	{"Paso 1 · di qué quieres", `Dos formas de empezar:

  loom sketch "una cola de trabajos con reintentos"
      Un modelo te entrevista con cuatro preguntas filosas sobre casos
      borde y luego escribe el spec por ti.

  o escribe loom.spec.yml a mano:

      feature: cola de trabajos
      requirements:
        - encolar un trabajo y recibir un id
        - los fallidos reintentan 3 veces y luego van a una lista de muertos
      rules:
        - sin dependencias de terceros
      acceptance:
        - criterion: flujo completo
          command: go test ./... -run TestFlujoCola

requirements dicen QUÉ. rules aplican a todo. acceptance es cómo loom
verifica la feature entera al final (un comando es una compuerta real).`},
	{"Paso 2 · conviértelo en tareas", `  loom apply --dry-run     ve qué se planearía, sin gastar nada
  loom apply               plánealo: el spec se vuelve contratos de tarea

Un contrato de tarea (.loom/tasks/T-001.md) es la unidad de trabajo:

  title        qué es
  done_when    un comando que HOY FALLA y pasa cuando la tarea está hecha
  only_touch   los únicos archivos que el agente puede cambiar (el resto se revierte)
  exposes      las firmas públicas que debe ofrecer a otras tareas
  depends_on   tareas que deben terminar antes

Si luego editas el spec, loom te muestra el diff semántico y planea solo lo
que cambió. Tu spec es estado vivo, no un ticket desechable.`},
	{"Paso 3 · deja trabajar a los agentes", `  loom run                 corre las tareas pendientes en paralelo
  loom board               tablero en vivo: listas, en curso, detenidas, pendientes

Cada tarea corre en su propio cuarto: un git worktree aparte. Los agentes no
pueden ver ni romper el trabajo de los demás, y tu árbol de trabajo nunca se toca.

Si una tarea falla, loom devuelve el error y reintenta. Pasado el 70% de la
ventana de contexto abre una sesión nueva en vez de arrastrar una llena.

  loom run --retry         revive las tareas detenidas`},
	{"Paso 4 · las compuertas y el PR", `Antes de entregar, cada tarea pasa la esclusa de salida, en orden:

  base · historial · ruido · secretos · presupuesto · interfaces ·
  dependencias · dna · bisectable · suite oculta · handoff · pr

  loom ship T-001          entrega una tarea
  loom ship --all          integra todas las tareas listas en UN PR limpio,
                           corre tu suite sobre el conjunto y tus comandos
                           de aceptación

Tarea verde != feature que funciona. Tres tareas pueden pasar cada una y aun
así no encajar, por eso loom prueba también el resultado integrado y agrega
una prueba de costura.`},
	{"Entiende tu sistema", `  loom map                 módulos, los contratos que ofrece cada uno, quién usa
                           a quién, ADN activo y estado de las compuertas. Sin
                           abrir código.
  loom map --mermaid       lo mismo como diagrama para Markdown o un PR

  loom dna                 elige el estilo de diseño que deben seguir tus agentes:
                           hexagonal, zero-deps, crash-early, functional-core
                           (algunos los exige la compuerta, no solo se piden)

  loom heal --from crash.log
                           pega un fallo: loom encuentra la regla que le
                           faltaba a tu spec, la agrega y arregla el código`},
	{"Chuleta", `  loom doctor              revisa tu entorno
  loom init                configura un repo (idioma, CLI de agente, modelos)
  loom sketch "idea"       entrevista → spec
  loom up                  spec → tareas → corrida   (agrega --ship para entregar)
  loom board / map         ver qué pasa / el sistema completo
  loom ship --all          un PR limpio
  loom heal --from log     arreglar a partir de un fallo
  loom language es|en      cambia el idioma que lees (los prompts siguen en inglés)

Primera corrida más rápida:   loom init  →  loom sketch "tu idea"  →  loom up --ship

Siempre:  loom <comando> --help    ·    loom tutorial`},
}
