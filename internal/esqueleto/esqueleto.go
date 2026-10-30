// Package esqueleto es la fase en que el modelo grande deja el plano del
// proyecto en código: estructura, ARCHITECTURE.md, tipos e interfaces
// declarados y un stub por clase o función con su firma y su contrato
// (a quién llama, casos de ejemplo). No escribe lógica ni pruebas: eso
// lo hacen los agentes baratos, cada uno su archivo y su prueba.
//
// Reemplaza al plan en prosa: las firmas que antes viajaban como texto
// en expone/usa y había que comparar a mano (firmaCanonica, examinador
// ciego, nivel semántico) ahora las valida el compilador.
//
// En una corrida real, el arquitecto escribió además 4.3k líneas de pruebas, 1.2k
// de cableado real e implementaciones de referencia: 292k tokens y
// $6.90, el 70% de la corrida. El plano cuesta una fracción de eso.
package esqueleto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/capturas"
	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/task"
)

// Marca es el texto con que lanza todo stub. La verificación no depende
// de él, pero le dice al agente que rellena qué reemplazar.
const Marca = "loom: sin implementar"

// Documento es el archivo de arquitectura que el esqueleto deja en la
// raíz y que cada cambio siguiente evoluciona.
const Documento = "ARCHITECTURE.md"

// Resultado es lo que devuelve el arquitecto además de lo que escribió
// en disco.
type Resultado struct {
	// Verificar compila o hace typecheck del proyecto entero y hoy pasa.
	Verificar string `json:"verificar"`
	// Integracion corre la prueba de punta a punta; hoy falla por los
	// stubs y pasa cuando todo está rellenado. Es la aceptación global.
	Integracion string `json:"integracion"`
	// Pruebas corre la suite completa del proyecto. En un monorepo
	// loom no la detecta, y la esclusa de salida la necesita.
	Pruebas      string `json:"pruebas"`
	Arquitectura string `json:"arquitectura"`
	// Pantallas dice cómo levantar la interfaz web y qué capturar: lo
	// usan el revisor visual y la entrega. Vacío si no hay web.
	Pantallas capturas.Pantallas `json:"pantallas"`
	Tareas    []plan.Borrador
}

// Pedido es lo que el humano quiere, ya redactado.
type Pedido struct {
	Texto string // feature, requirements, reglas y aceptación
	// Previo es el ARCHITECTURE.md que ya existe: el esqueleto lo
	// evoluciona en vez de reescribir el proyecto.
	Previo string
	// PrimerID es el id de la primera tarea de relleno.
	PrimerID string
	// SinDocker apaga la exigencia de compose: el humano lo pidió en sus
	// reglas (una librería o un CLI no se despliegan).
	SinDocker bool
	// Capturas son fotos de cómo se ve hoy la interfaz, en tamaño
	// celular. Un plan visual hecho sin verlas planea contra el código,
	// no contra lo que ve el usuario.
	Capturas []string
	// SuiteRota es la cola de la suite del proyecto cuando ya falla en la
	// base, antes de cualquier cambio; "" si pasa o no se pudo saber.
	SuiteRota string
}

// Prompt builds the architect's instruction. The fixed part goes first so a
// correction (PromptCorregir) shares the cached prefix.
func Prompt(p Pedido, c plan.Contexto) string {
	var b strings.Builder
	b.WriteString("You are loom's ARCHITECT. You do not implement the logic: you leave the project ready so many cheap agents fill it in parallel, one file each, without seeing each other. What they cannot decide, you decide now, in code.\n\n")
	b.WriteString("REQUEST\n")
	b.WriteString(p.Texto)
	b.WriteString("\n")
	if c.Constitucion != "" {
		b.WriteString("\nPROJECT CONSTITUTION (respect it)\n" + c.Constitucion + "\n")
	}
	b.WriteString("\nREPOSITORY\n")
	b.WriteString(contexto(c))
	if strings.TrimSpace(p.Previo) != "" {
		b.WriteString("\n" + Documento + " already exists. Evolve it: add what is new, adjust what changes and do not rewrite what already works. Tasks are only for what is new or changes.\n")
	}
	if len(p.Capturas) > 0 {
		b.WriteString("\nHOW IT LOOKS TODAY (phone-size screenshots taken moments ago, of this same repository with the current code: they are not stale. Open them with your file-reading tool before planning)\n")
		for _, c := range p.Capturas {
			b.WriteString("- " + c + "\n")
		}
		b.WriteString("If the request touches how it looks, your plan must change what you see there in a way that shows.\n")
	}
	b.WriteString(`
IF THE REPOSITORY ALREADY HAS CODE
- Screenshots outrank the code: if the code says something exists but it is not visible in the screenshot, or it looks broken or unstyled, it is wrong and must be planned.
- If there is truly nothing to change, answer with "tasks": [] and do not invent work.
- Templates, stylesheets, markup and data files (.html, .css, .svg, .json, .md) are never stubs and you do not create them: the task creates them, and its "how" carries the "Cases:" of what must show.
- What already works does NOT become a stub. Respect the stack, libraries, design system and conventions already there; do not migrate anything the request does not ask for.
- A change to existing code is a task without a stub: "how" says what to change, where, and "Cases:" of the new behavior (input → output or what shows on screen). If the change adds new functions, methods or components, those do go as stubs with their contract.
- The "done_when" of a change runs a NEW test of the new behavior (a file that does not exist today): existing tests pass today and are no oracle. Those tests must keep passing.
- An existing test is frozen: the agent cannot touch it. If the change you ask for makes something an existing test checks FALSE (changes a text, a flow, a result), that is your decision, not the agent's: put that test file in the task's "only_touch" with its exact path (a glob does not unfreeze it), and write in its "how" an "Obsolete:" line saying which cases of that test stop being valid and what they must check now. The agent may only change that. If no existing test becomes false, do not put existing tests in "only_touch".
- If the request is visual (redesign, style, usability), the result has to LOOK different, not just change classes. Existing tests that pin CSS classes or styles protect nothing for the user: rewrite or delete them yourself in this skeleton so they do not block the redesign. Do not make "additive" changes to dodge them.
- UI tests you ask for verify what the user sees and does (texts, roles, states, navigation), not CSS classes: how it looks is judged by a screenshot review.
`)
	b.WriteString(`
YOUR DELIVERABLE IS A BLUEPRINT IN CODE, NOT AN IMPLEMENTATION
You write no logic and no tests: every line you write is paid by the expensive model, and that is the part the cheap agents do. Your job is that they fit together without seeing each other: where each thing goes, which classes and methods exist, what each receives and returns, who calls whom.

1. Structure and stack: folders, manifests (package.json, pyproject.toml, go.mod, ...), build and test-runner configuration, and .gitignore. Install the dependencies.
   TypeScript: tsconfig with "moduleResolution": "bundler" (apps, bundlers) or "node16"/"nodenext" (Node libraries), never "node", "node10" or "classic", and none of the options TypeScript 5 deprecated (importsNotUsedAsValues, preserveValueImports, suppressImplicitAnyIndexErrors, target ES3...).
   Web interface: use the design system and components the project already has. Only if there are none and the rules ask for nothing else: Tailwind CSS and the standard component library of its framework (e.g. shadcn/ui on React), configured with its CLI without questions (--yes/--defaults), and the base components the app will use added with that CLI: they are generated code, do not write them yourself. The theme (colors, radii, typography) lives in one place.
2. ` + Documento + ` at the root: style (modular, hexagonal or layered according to the problem, not by fashion), each module with its file and responsibility, which module may depend on which, data flow of the main case, and how each module is tested. If there is a web interface, a UI section: the design system (where the theme and components live), the page template (layout, header, loading/empty/error states) and which component is used for what. It is the source of truth every agent reads and the next change evolves.
3. Shared types, entities, interfaces and ports: DECLARATIONS only (fields, method signatures). No bodies with logic.
4. Everything NEW is a STUB, including new wiring (main, routes, container, App): every public class, function and method with its exact signature (input and output types) and a contract written as line comments right above it (// in Go, TypeScript and Rust, # in Python; never a /** ... */ block of narrative prose) with
   - what it does, inputs, output and errors;
   - whom it calls (module and method) and who uses it;
   - "Idea:" how it is solved, already thought through by you: algorithm, data structures, order of the steps and the traps (edge cases, what looks obvious and is wrong). Telegraphic, 1 to 8 lines, no code or full sentences: you already solved the problem while planning and the agent only writes it. Do not repeat what the signature already says.
   - "Cases:" 2 to 5 concrete examples of input → output or error, which the agent will turn into its test.
   The labels "Idea:" and "Cases:" are LITERAL: the word and the colon attached, nothing in between, also in the "how" of a change to existing code. "Cases (with mocks):" or "Examples:" do not count: loom looks for the exact text and sends the blueprint back to you.
   The body only throws an error with the exact text "` + Marca + `".
5. Nothing else. Write no tests, reference implementations, sample data or code outside the repository. Run nothing except what is needed for "verify" to pass. No debugging commands left in the project: no "node -e", "sh -c", interactive shells or temporary scripts in package.json, manifests or tool configs.
`)
	if !p.SinDocker {
		b.WriteString(`6. Docker deployment (it is configuration: you write it, complete). One Dockerfile per service (multi-stage, final image without development tools), a compose.yaml at the root that brings everything up with "docker compose up --build" (services, database if any, volumes for data, ports, variables from .env) and a .env.example, and a .dockerignore that leaves out dependencies, local builds, .git and .env. The README starts with that command. If they already exist, evolve them. "docker compose config" must pass.
`)
	}
	b.WriteString(`

HOW TO SPLIT
- One task per module: a stub file, or several that share state or make no sense apart, or a change to an existing file. With "Idea:" written, a cheap agent can handle a whole complex module: do not split into minimal tasks what is understood together, and do not raise a task's "weight" because it is hard to think through; you already thought it through. Weight rises only for size or interface.
- Exception: a VISUAL request (design, style, usability) is not split into small tasks. It is ONE design task with "weight": "heavy", owner of all the interface files that change (theme, components and screens in its "only_touch"), because a coherent design does not come from patches by agents that do not see the whole screen. In "how" describe the concrete design: palette, typography, spacing, how each screen and each state looks. Functional tasks that touch those files depend on it. The smaller and more independent, the more agents in parallel and the cheaper.
- Each task with a stub declares in "done_when" a new test file and lists it in "only_touch" next to the stub. loom removes it from the implementer's scope and an independent examiner writes that test from the signatures and "Cases:". Changes without a stub keep the test writing by the implementer.
- The agent is a cheap model: it does not redesign or choose libraries. What it needs to decide must be in the contract.
- Dependencies between modules enter by parameter or constructor (ports), so each module is tested with fakes without waiting for the others. "depends_on" only when a module truly needs another's real implementation.
- A final task writes the end-to-end test of the main flow: it depends on all and its "done_when" is the "integration" command. If an end-to-end test already exists, it passes today: if what changes is behavior, the final task writes a NEW one and "integration" runs the new one; if the change does not alter the flow (styles, texts), there is no final task and "integration" is the existing test, as a regression.

BEFORE ANSWERING: the "verify" command passes with the stubs (it compiles, typechecks or imports everything). Nothing more.

ANSWER AT THE END WITH ONLY THIS JSON
{
  "verify": "command that compiles or typechecks the whole project and passes today in the strict form of the language: Go \"go vet ./...\", Rust \"cargo check && cargo clippy -- -D warnings\", TypeScript \"npx tsc --noEmit --strict\", Python \"mypy --strict .\" (and \"ruff check .\")",
  "integration": "command that runs the end-to-end test (the final task writes it)",
  "tests": "command that runs the WHOLE project suite (all folders and languages)",
  "architecture": "summary of 5 to 15 lines; the full text lives in ` + Documento + `",
  "screens": {"start": "command that serves the whole app on port $PORT, with a build if needed (omit the whole field if there is no web interface)", "url": "http://localhost:$PORT", "routes": ["/", "each main screen, with its route as opened in the browser"], "seed": "command that loads sample data with the app running on $PORT, so screenshots do not show only empty states (optional)", "script": "command that drives a browser through flows that have no route (picking a photo, filling a form, generating a result) and saves one PNG per step in $CAPTURAS; receives $BASE_URL. Write it yourself (e.g. Playwright): it is MANDATORY if any task changes the interface", "sample": "route of ONE page you write in the skeleton if there is a web interface: the component sheet with the visual identity (palette, typography, buttons, fields, a card, a table, alerts, an empty state) and below it a basic layout (header, navigation, content) using all of it. The visual review looks at it first instead of every screen"},
  "tasks": [
    {
      "title": "short lowercase phrase",
      "why": "which requirement it covers",
      "covers": ["the requirement ids it implements, as they come in the request: \"R-a1b2c3\""],
      "done_when": "command that runs only this module's test",
      "only_touch": ["the stub or the file that changes", "its test file"],
      "depends_on": [],
      "weight": "light | medium | heavy",
      "how": "which file to fill in or what to change; for a change to existing code, its Idea: and its Cases:"
    }
  ]
}
`)
	if p.SuiteRota != "" {
		b.WriteString("\nSTATE OF THE BASE\nThe project suite already fails today, before your change. Its output ends like this:\n" + p.SuiteRota + "\nIf that failure prevents your tasks' tests from running (the test database does not build, a broken import, a missing dependency), fix it yourself in the skeleton: it is base state, not new logic, and without it no task can go green. If it does not affect them, do not touch it and do not use it in any \"done_when\".\n")
	}
	if p.PrimerID != "" {
		fmt.Fprintf(&b, "Your tasks get consecutive ids starting at %s in array order: in \"depends_on\" write those full ids (\"%s\"), not bare numbers.\n", p.PrimerID, p.PrimerID)
	}
	b.WriteString(idioma.Instruccion())
	return b.String()
}

// PromptCorregir repeats the original request (same prefix, same cache)
// and adds what verification found. The repo keeps the earlier work: it is
// corrected, not started over.
func PromptCorregir(original string, problemas []string) string {
	var b strings.Builder
	b.WriteString(original)
	b.WriteString("\nYOU ALREADY WORKED ON THIS REPOSITORY. What you wrote is still on disk, but loom's verification found this:\n")
	for _, p := range problemas {
		b.WriteString("- " + p + "\n")
	}
	b.WriteString("Fix it in the repository and answer again with the complete JSON.\n")
	return b.String()
}

// PromptCorregirSesion is the correction for an architect continuing its
// own conversation: it already has the request and what it read from the
// repo, so it only receives what verification found.
func PromptCorregirSesion(problemas []string) string {
	var b strings.Builder
	b.WriteString("loom's verification found this in what you left:\n")
	for _, p := range problemas {
		b.WriteString("- " + p + "\n")
	}
	b.WriteString("Fix it in the repository without re-exploring what you already know. If the fix was only on disk and your JSON stays the same, answer only {\"unchanged\": true}; otherwise answer again with the complete JSON.\n")
	return b.String()
}

// SinCambios dice si la corrección contestó {"unchanged": true}: arregló
// el disco y el JSON es el de antes. Reescribirlo entero costaba hasta
// 126k tokens de salida por vuelta, lo más caro y lento del arquitecto.
func SinCambios(texto string) bool {
	t := plan.CuerpoJSON(texto)
	var v struct {
		Unchanged bool            `json:"unchanged"`
		Tasks     json.RawMessage `json:"tasks"`
	}
	// CuerpoJSON devuelve desde la llave hasta el final del texto: Decode lee solo el objeto
	return strings.HasPrefix(t, "{") && json.NewDecoder(strings.NewReader(t)).Decode(&v) == nil && v.Unchanged && v.Tasks == nil
}

func contexto(c plan.Contexto) string {
	return contextoBase(c) + grafoDe(c)
}

func grafoDe(c plan.Contexto) string {
	if c.Grafo == "" {
		return ""
	}
	return "\n" + c.Grafo + "\n"
}

func contextoBase(c plan.Contexto) string {
	switch {
	case c.EsVacio && c.Stack != "":
		return "Vacío. El humano eligió el stack: " + c.Stack + ".\n"
	case c.EsVacio:
		return "Vacío. Elige el stack que mejor sirva al pedido y respeta las reglas.\n"
	case c.Lenguaje == "":
		return "Ya tiene código. Inspecciónalo antes de decidir: respeta su stack, su estructura y sus corredores de pruebas.\n"
	default:
		s := "Ya tiene código en " + c.Lenguaje + ". Respeta su stack y su estructura."
		if c.Pruebas != "" {
			s += " Pruebas del proyecto: " + c.Pruebas + "."
		}
		return s + "\n"
	}
}

// Parse lee el JSON final del arquitecto.
func Parse(texto string) (Resultado, error) {
	bs, err := plan.Parse(texto)
	// sin tareas es una respuesta válida del arquitecto: lo pedido ya
	// está. Exigirle tareas lo hizo copiar el plan anterior
	if err != nil && !errors.Is(err, plan.ErrSinTareas) {
		return Resultado{}, err
	}
	t := plan.CuerpoJSON(texto)
	var r Resultado
	if !strings.HasPrefix(t, "{") {
		return Resultado{}, errors.New(idioma.T("el arquitecto no devolvió un objeto JSON"))
	}
	var crudo json.RawMessage
	if err := json.NewDecoder(strings.NewReader(plan.EscaparControles(t))).Decode(&crudo); err != nil {
		return Resultado{}, fmt.Errorf(idioma.T("el arquitecto devolvió JSON inválido · %s"), err)
	}
	if err := json.Unmarshal(normalizarClaves(crudo), &r); err != nil {
		return Resultado{}, fmt.Errorf(idioma.T("el arquitecto devolvió JSON inválido · %s"), err)
	}
	r.Tareas = bs
	return r, nil
}

// Verificacion es dónde y cómo revisar lo que dejó el arquitecto.
type Verificacion struct {
	Dir string
	// Base es el commit con que arrancó el cuarto. Lo que ya existía ahí
	// es código que funciona: se cambia, no se convierte en stub. Vacío
	// = todo es nuevo.
	Base    string
	Docker  bool // exigir compose.yaml y .dockerignore
	Timeout time.Duration
	Env     []string
}

// Problemas verifica el plano sin ningún modelo. Vacío es listo para
// repartir. No exige pruebas: las escribe cada tarea a partir de los
// casos de su contrato.
func Problemas(ctx context.Context, v Verificacion, r Resultado) []string {
	dir := v.Dir
	var out []string
	if _, err := os.Stat(filepath.Join(dir, Documento)); err != nil {
		out = append(out, "missing "+Documento+" at the root")
	}
	if strings.TrimSpace(r.Verificar) == "" {
		out = append(out, "missing \"verify\": the command that compiles or typechecks the project")
	} else if salida, code := correr(ctx, dir, r.Verificar, v.Timeout, v.Env); code == nil || *code != 0 {
		out = append(out, fmt.Sprintf("\"verify\" (%s) does not pass with the stubs: %s", r.Verificar, cola(salida)))
	}
	if v.Docker {
		out = append(out, docker(ctx, dir, v.Timeout, v.Env)...)
	}
	for _, p := range config.TsconfigObsoleto(dir) {
		out = append(out, "deprecated TypeScript config: "+p)
	}
	archivos := versionables(ctx, dir)
	previos := enCommit(ctx, dir, v.Base)
	integracion := false
	for i, t := range r.Tareas {
		nombre := fmt.Sprintf("task %d (%s)", i+1, t.Titulo)
		if strings.TrimSpace(t.ListoCuando) == "" {
			out = append(out, nombre+": no done_when")
		}
		if strings.TrimSpace(t.ListoCuando) == strings.TrimSpace(r.Integracion) {
			integracion = true
		}
		if len(t.TocarSolo) == 0 {
			out = append(out, nombre+": empty only_touch")
			continue
		}
		for _, f := range t.TocarSolo {
			if !strings.ContainsAny(f, "*?[") && !task.EsArchivoDePrueba(f) && EsCodigo(f) && !algunoCoincide([]string{f}, archivos) {
				out = append(out, fmt.Sprintf("%s: %s does not exist; you create the stub", nombre, f))
			}
		}
		out = append(out, contrato(dir, nombre, t, archivos, previos)...)
	}
	// "integracion" es la aceptación global. Si falla hoy (proyecto nuevo,
	// o una prueba nueva que todavía no existe), una tarea final la hace
	// pasar. Si ya pasa, es la regresión de un proyecto existente: vale
	// como aceptación pero ninguna tarea puede usarla como listo_cuando,
	// porque la esclusa rechaza lo que ya pasa (la tarea final de
	// la UI renovada murió así). Un cambio solo visual no necesita una
	// prueba de punta a punta nueva.
	if len(r.Tareas) > 0 && strings.TrimSpace(r.Integracion) != "" {
		_, code := correr(ctx, dir, r.Integracion, v.Timeout, v.Env)
		pasa := code != nil && *code == 0
		switch {
		case pasa && integracion:
			out = append(out, fmt.Sprintf("\"integration\" (%s) already passes today: a task with that done_when proves nothing. If the new work needs an end-to-end test, the final task writes a new one and \"integration\" runs it; otherwise remove that task and keep \"integration\" as a regression", r.Integracion))
		case !pasa && !integracion:
			out = append(out, "no task has the \"integration\" command as its done_when, which fails today: the final task that writes the end-to-end test is missing")
		}
	}
	return out
}

// contrato exige que cada tarea diga qué hacer y cómo probarlo. En un
// archivo nuevo, un stub con la marca (prueba que el arquitecto no
// escribió la lógica) y "Casos:" en su comentario. En un archivo que ya
// existía, el código funciona y no se convierte en stub: el contrato del
// cambio va en "como", con sus casos. Antes se exigía la marca siempre y
// ningún cambio a código existente pasaba la verificación. La tarea
// final de integración solo escribe pruebas y no necesita nada.
func contrato(dir, nombre string, t plan.Borrador, archivos []string, previos map[string]bool) []string {
	var nuevos, existentes, pruebasViejas []string
	for _, f := range archivos {
		if previos[f] && task.EsArchivoDePrueba(f) && slices.Contains(t.TocarSolo, f) {
			pruebasViejas = append(pruebasViejas, f)
		}
		if !config.MatchesAny(t.TocarSolo, f) || task.EsArchivoDePrueba(f) {
			continue
		}
		// plantillas, estilos y datos no pueden lanzar "sin implementar":
		// exigirles stub hacía rebotar al arquitecto en cada rediseño
		if previos[f] || !EsCodigo(f) {
			existentes = append(existentes, f)
		} else {
			nuevos = append(nuevos, f)
		}
	}
	var out []string
	if len(nuevos) > 0 {
		marca, casos, idea := false, false, false
		for _, f := range nuevos {
			b, err := os.ReadFile(filepath.Join(dir, f))
			if err != nil {
				continue
			}
			marca = marca || TieneMarca(string(b))
			if TieneMarca(string(b)) && strings.Contains(string(b), "/**") {
				out = append(out, fmt.Sprintf(idioma.T("%s: %s documents its stubs with a /** ... */ block; write the contract as line comments (//) right above each stub"), nombre, f))
			}
			casos = casos || idioma.Tiene(string(b), idioma.Casos)
			idea = idea || idioma.Tiene(string(b), idioma.Idea)
		}
		if !marca {
			out = append(out, fmt.Sprintf(idioma.T("%s: %s is new and has no stub with \"%s\"; the agent implements what is new, not you"), nombre, strings.Join(nuevos, ", "), Marca))
		}
		if !casos {
			out = append(out, fmt.Sprintf(idioma.T("%s: the contract of %s has no \"Cases:\"; without examples the agent does not know what to test"), nombre, strings.Join(nuevos, ", ")))
		}
		// lo pensado por el modelo caro viaja en el contrato: sin eso el
		// barato rediseña, y solo se le podían dar tareas triviales
		if !idea {
			out = append(out, fmt.Sprintf(idioma.T("%s: the contract of %s has no \"Idea:\"; write in a few lines how it is solved (algorithm, structures, traps) so the agent only implements it"), nombre, strings.Join(nuevos, ", ")))
		}
	}
	// una prueba existente en tocar_solo deja de estar congelada: sin
	// decir qué parte quedó obsoleta, el agente podría reescribirla entera
	if len(pruebasViejas) > 0 && !idioma.Tiene(t.Como, idioma.Obsoleto) {
		out = append(out, fmt.Sprintf(idioma.T("%s: lets %s, a test that already exists, be modified, and its \"how\" has no \"Obsolete:\" with the cases that stop being valid; if none becomes false, remove it from only_touch"), nombre, strings.Join(pruebasViejas, ", ")))
	}
	if len(nuevos) > 0 {
		return out
	}
	if len(existentes) > 0 && !idioma.Tiene(t.Como, idioma.Casos) {
		out = append(out, fmt.Sprintf(idioma.T("%s: changes %s, which already exists, and its \"how\" has no \"Cases:\" of the new behavior"), nombre, strings.Join(existentes, ", ")))
	}
	return out
}

// EsCodigo dice si el archivo admite un stub que lance la marca. Una
// plantilla, una hoja de estilos o un JSON no: su contrato va en "como".
func EsCodigo(f string) bool {
	switch strings.ToLower(filepath.Ext(f)) {
	case ".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".rs", ".java", ".kt", ".rb", ".php", ".cs", ".swift", ".c", ".cc", ".cpp", ".h", ".vue", ".svelte":
		return true
	}
	return false
}

// Composes son los nombres que acepta docker compose.
var Composes = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}

// docker exige lo necesario para levantar el proyecto con un comando:
// compose y .dockerignore (sin él, node_modules y .venv viajan al
// contexto de build). Si docker está instalado, además valida el compose.
func docker(ctx context.Context, dir string, timeout time.Duration, env []string) []string {
	compose := ""
	for _, n := range Composes {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			compose = n
			break
		}
	}
	var out []string
	if compose == "" {
		out = append(out, "missing compose.yaml at the root: the project starts with \"docker compose up --build\"")
	}
	if _, err := os.Stat(filepath.Join(dir, ".dockerignore")); err != nil {
		out = append(out, "missing .dockerignore: without it dependencies and local builds enter the docker context")
	}
	if compose != "" {
		if _, err := exec.LookPath("docker"); err == nil {
			if salida, code := correr(ctx, dir, "docker compose config -q", timeout, env); code == nil || *code != 0 {
				out = append(out, "\"docker compose config\" does not pass: "+cola(salida))
			}
		}
	}
	return out
}

// enCommit lista los archivos versionados en base; nada si base es vacío.
func enCommit(ctx context.Context, dir, base string) map[string]bool {
	out := map[string]bool{}
	if base == "" {
		return out
	}
	cmd := exec.CommandContext(ctx, "git", "ls-tree", "-r", "--name-only", base)
	cmd.Dir = dir
	b, _ := cmd.Output()
	for _, f := range strings.Fields(string(b)) {
		out[f] = true
	}
	return out
}

// ConStub reporta si alguno de los archivos de tocar (no de prueba)
// tiene la marca: decide si la tarea rellena un stub o cambia código.
func ConStub(ctx context.Context, dir string, tocar []string) bool {
	for _, f := range versionables(ctx, dir) {
		if !config.MatchesAny(tocar, f) || task.EsArchivoDePrueba(f) {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil && TieneMarca(string(b)) {
			return true
		}
	}
	return false
}

// SuiteBase corre la suite del proyecto tal como está y devuelve la cola
// de su salida si falla; "" si pasa, si no hay comando o si se pasó del
// tiempo (no concluyente). Es para saberlo antes de pagar un plan: con
// la suite rota en la base, las tareas que la necesitan no salen verdes
// hagan lo que hagan.
func SuiteBase(ctx context.Context, dir, comando string, timeout time.Duration, env []string) string {
	if strings.TrimSpace(comando) == "" {
		return ""
	}
	salida, code := correr(ctx, dir, comando, timeout, env)
	if code == nil || *code == 0 || *code == 124 {
		return ""
	}
	return cola(salida)
}

func correr(ctx context.Context, dir, cmdStr string, timeout time.Duration, env []string) (string, *int) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// verify instala y compila el proyecto entero: FS aislado, red abierta
	cmd := task.Shell(ctx, dir, cmdStr, true)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		c := 124
		return string(out) + "\nse pasó del tiempo", &c
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		c := exitErr.ExitCode()
		return string(out), &c
	}
	if err != nil {
		return string(out) + err.Error(), nil
	}
	c := 0
	return string(out), &c
}

// versionables lista los archivos del árbol que git vería, sin los
// ignorados ni los excluidos (node_modules y compañía).
func versionables(ctx context.Context, dir string) []string {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-co", "--exclude-standard")
	cmd.Dir = dir
	out, _ := cmd.Output()
	return strings.Fields(string(out))
}

func algunoCoincide(patrones, archivos []string) bool {
	for _, f := range archivos {
		if config.MatchesAny(patrones, f) {
			return true
		}
	}
	return false
}

// cola deja las últimas líneas de una salida para el mensaje.
func cola(s string) string {
	lineas := strings.Split(strings.TrimSpace(s), "\n")
	if len(lineas) > 6 {
		lineas = lineas[len(lineas)-6:]
	}
	return strings.Join(lineas, " ⏎ ")
}

// normalizarClaves lleva las llaves en inglés de la respuesta a las canónicas.
func normalizarClaves(data []byte) []byte {
	data = idioma.NormalizarClaves(data, map[string]string{
		"verify": "verificar", "integration": "integracion", "tests": "pruebas",
		"architecture": "arquitectura", "screens": "pantallas", "tasks": "tareas",
	})
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return data
	}
	if p, ok := m["pantallas"]; ok {
		m["pantallas"] = idioma.NormalizarClaves(p, map[string]string{"start": "levantar", "routes": "rutas", "seed": "semilla", "sample": "muestra"})
		if out, err := json.Marshal(m); err == nil {
			return out
		}
	}
	return data
}

// marcaAnterior is the stub body text written before the rename to loom.
const marcaAnterior = "devclean: sin implementar"

// TieneMarca reports whether src has a stub body, under either name.
func TieneMarca(src string) bool {
	return strings.Contains(src, Marca) || strings.Contains(src, marcaAnterior)
}
