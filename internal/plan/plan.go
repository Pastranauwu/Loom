// Package plan implementa al planificador: convierte una
// petición en lenguaje natural en contratos de tarea. El contrato lo
// redacta un modelo; loom solo parsea, muestra y aprueba. Nunca lo
// escribe a mano el usuario.
package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"sort"
	"strconv"
	"strings"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/skills"
	"github.com/Pastranauwu/loom/internal/task"
)

// Borrador es una tarea propuesta por el planificador, antes de asignarle
// id y version.
type Borrador struct {
	Titulo      string   `json:"titulo"`
	Porque      string   `json:"porque"`
	ListoCuando string   `json:"listo_cuando"`
	TocarSolo   []string `json:"tocar_solo"`
	NoTocar     []string `json:"no_tocar"`
	DependeDe   []string `json:"depende_de"`
	Expone      []string `json:"expone"`
	Usa         []string `json:"usa"`
	Riesgos     string   `json:"riesgos"`
	Peso        string   `json:"peso"`
	Agente      string   `json:"agente,omitempty"`
	// Skills son las del catálogo que la tarea necesita: nil si el modelo
	// no lo dijo (quedan las del rol), [] si dijo que ninguna.
	Skills []string `json:"skills"`
	// Cubre son los ids de requerimiento que implementa la tarea.
	Cubre []string `json:"cubre"`
	// LimiteLineas se lee por compatibilidad con planes anteriores.
	// Solo la configuración humana determina el tope efectivo.
	LimiteLineas int `json:"limite_lineas"`

	// Como es el enfoque que sugiere el orquestador: cómo encarar la
	// tarea, qué tocar primero, a qué no meterse. Es orientación para el
	// ejecutor, no contrato vinculante como listo_cuando — se inyecta en
	// el prompt como nota, y en una descomposición recursiva es lo único
	// que le dice al agente chico cómo cumplir su parte sin inventar.
	Como string `json:"como,omitempty"`
}

// UnmarshalJSON tolera que el modelo escriba las dependencias como
// números (`"depende_de": [1]`) o como ids completos (`["T-002"]`):
// `depende_de` es []string en el contrato, y un JSON numérico rompía
// toda la descomposición recursiva.

// ErrSinTareas es un plan vacío. Para el planificador es un error; para
// el arquitecto puede ser la respuesta correcta (el código ya cumple).
var ErrSinTareas = errors.New(idioma.T("el modelo no propuso ninguna tarea"))

func (b *Borrador) UnmarshalJSON(data []byte) error {
	type alias Borrador
	var raw struct {
		*alias
		DependeDe json.RawMessage `json:"depende_de"`
	}
	a := alias(*b)
	raw.alias = &a
	if err := json.Unmarshal(idioma.NormalizarClaves(data, clavesBorrador), &raw); err != nil {
		return err
	}
	a.Peso = task.NormalizarPeso(a.Peso)
	*b = Borrador(a)
	if len(raw.DependeDe) == 0 {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw.DependeDe, &items); err != nil {
		return err
	}
	deps := make([]string, 0, len(items))
	for _, it := range items {
		var s string
		if json.Unmarshal(it, &s) == nil {
			deps = append(deps, s)
			continue
		}
		var n float64
		if json.Unmarshal(it, &n) == nil {
			deps = append(deps, strconv.FormatFloat(n, 'f', -1, 64))
			continue
		}
		deps = append(deps, strings.Trim(string(it), `"`))
	}
	b.DependeDe = deps
	return nil
}

// Generador pide texto a un modelo. El comando lo adapta desde el
// ejecutor; aquí solo importa la interfaz mínima.
type Generador interface {
	Generar(ctx context.Context, prompt string) (string, error)
}

// Contexto es lo que loom sabe del repositorio al momento de
// planear. Se lo pasa al modelo para que no adivine el stack ni
// invente comandos que no existen en el proyecto.
type Contexto struct {
	Lenguaje     string   // go, node, python, rust, "" si no se detecta
	EsVacio      bool     // repo sin código fuente todavía
	Pruebas      string   // comando de pruebas detectado ("" si no hay)
	Stack        string   // stack elegido por el humano ("" si lo decide el modelo)
	Requisitos   string   // requisitos extra que dijo el humano, en texto libre
	Constitucion string   // contenido de .loom/constitution.md, "" si no existe
	Vedadas      []string // globs que tocar_solo nunca puede incluir (zonas prohibidas + rutas de prueba)
	// Ocupados son los alcances que ya tienen dueño: tocar_solo de las
	// tareas activas, por id. Sin esto el planificador propone tareas
	// que se cruzan con las que ya corren, la esclusa de entrada las
	// rechaza por solapamiento y los tokens del plan se gastaron para nada.
	Ocupados map[string][]string
	// Expuestas son las firmas que ya prometen las tareas del repo, por
	// id. ValidatePlan compara el plan contra ellas: sin verlas, el
	// modelo reescribe "get_db() -> Iterator[Session]" como
	// "def get_db() -> Generator[Session]" y el plan entero se rechaza.
	Expuestas map[string][]string
	// PrimerID es el id que recibirá la primera tarea del plan. Sin
	// saberlo, el modelo que ve tareas previas numera por su cuenta
	// (T-002 después de T-001) y leer eso por posición armaba ciclos.
	PrimerID string
	Agentes  map[string]config.Agente // agentes disponibles en config.yml
	// PruebasPropias marca que este stack no tiene examinador ciego, así
	// que las pruebas las escribe la propia tarea y su archivo tiene que
	// entrar en tocar_solo. Sin decirlo, el planificador apunta el
	// listo_cuando a un archivo de prueba que deja fuera de alcance, y
	// nadie puede crearlo: la tarea queda roja para siempre.
	PruebasPropias bool
	// Skills es el catálogo de .agents/skills del que el planificador
	// elige las de cada tarea. Vacío: el campo no se pide.
	Skills []skills.Skill
	// Grafo es el resumen de .loom/repo-graph.json relevante a la
	// petición; vacío no agrega nada al prompt.
	Grafo string
}

// Prompt asks for architecture decisions and contracts in a single call.
// The repo context is injected so the model uses the real language and test command.
func Prompt(frase string, c Contexto) string {
	return prompt("You are loom's planner. Design the solution and split it into complete, verifiable units of work:\n\n", "\""+frase+"\"\n\n", c)
}

// PromptCompletar asks for the contracts of tasks the human already decided in
// a quick spec. The planner does not split: it writes done_when, scope,
// dependencies and signatures for exactly those tasks, in that order.
func PromptCompletar(feature string, reglas []string, tareas []task.Task, c Contexto) string {
	n := len(tareas)
	intro := fmt.Sprintf("You are loom's planner. The human already decided these %d tasks; your job is to write their contract. "+
		"Do NOT split, merge, remove or add tasks: return exactly %d, in this same order and with the same \"title\". "+
		"If a task already has a field, copy it as is.\n\n", n, n)

	var p strings.Builder
	if feature != "" {
		fmt.Fprintf(&p, "Feature: %s\n", feature)
	}
	for _, r := range reglas {
		fmt.Fprintf(&p, "Rule for all: %s\n", r)
	}
	p.WriteString("Tasks:\n")
	for i, t := range tareas {
		fmt.Fprintf(&p, "%d. %s · %s", i+1, t.ID, t.Titulo)
		if t.ListoCuando != "" {
			fmt.Fprintf(&p, " · done_when: %s", t.ListoCuando)
		}
		if len(t.TocarSolo) > 0 {
			fmt.Fprintf(&p, " · only_touch: %s", strings.Join(t.TocarSolo, ", "))
		}
		if len(t.DependeDe) > 0 {
			fmt.Fprintf(&p, " · depends_on: %s", strings.Join(t.DependeDe, ", "))
		}
		if t.Notas != "" {
			fmt.Fprintf(&p, " · notes: %s", t.Notas)
		}
		p.WriteString("\n")
	}
	if n > 0 {
		fmt.Fprintf(&p, "In \"depends_on\" use the ids from this list (e.g. \"%s\").\n\n", tareas[0].ID)
	}
	return prompt(intro, p.String(), c)
}

// prompt builds the instruction shared by planning and completing: what is
// asked, the repo context and the format of each contract.
func prompt(intro, peticion string, c Contexto) string {
	var b strings.Builder
	b.WriteString(intro)
	if c.Constitucion != "" {
		b.WriteString("Project constitution (established conventions the plan must respect):\n")
		b.WriteString(c.Constitucion)
		b.WriteString("\n\n")
	}
	b.WriteString(peticion)
	b.WriteString(contextoPrompt(c))
	if c.Grafo != "" {
		b.WriteString("\n\n" + c.Grafo)
	}
	b.WriteString("\n\nYou are the ARCHITECT of the solution, not an implementer: you write no code. Your job is to leave the architecture defined (style, file tree, responsibility of each file, data flow, shared types and the exact signature of each public function with parameters and return value) so each executor only writes the files assigned to it without redesigning anything. Apply SOLID where it helps; use hexagonal architecture or microservices only when the problem justifies it. Agents, not people, will maintain this code: ask for compact internal bodies, no comments and no intermediate functions added only for readability, and descriptive names and signatures only for what is declared in \"exposes\".\n")
	b.WriteString("Return ONLY a JSON object with \"architecture\" (text with the file tree, responsibilities, shared types and exact signatures) and \"tasks\" (array of contracts). The architecture is stored in the notes of every contract.\n")
	b.WriteString("Organize \"architecture\" in paragraphs separated by a blank line, each with an uppercase heading (STACK:, STYLE:, SHARED TYPES:, ...). Two are mandatory and have a fixed format, because each executor receives only the part that concerns it: \"FILE TREE:\" with one line per file starting with its path (\" src/a.js (T-001) responsibility\"), and \"PUBLIC SIGNATURES:\" with one block per file starting with \"[T-00N path]\" followed by its signatures.\n")
	b.WriteString("Divide and conquer: split into tasks per FILE or MODULE, each with an \"only_touch\" limited to the files it writes, its \"exposes\"/\"uses\" with exact signatures (parameters and return) and a \"how\" saying exactly what to write. The more small independent tasks, the more models run in parallel and the faster the result: prefer several light tasks to one big task. If a shared base is needed (types, contracts), give it to a heavy task with the architect agent and make its consumers depend on it. Reserve ONE final task for composition and its integration tests.\n")
	b.WriteString("Each task contains these fields:\n")
	b.WriteString("- \"title\": short lowercase phrase\n")
	b.WriteString("- \"why\": why it matters (one sentence)\n")
	b.WriteString("- \"done_when\": an executable command that FAILS TODAY and passes when the task is done (mandatory).\n")
	b.WriteString("  · loom runs it BEFORE spending a single token and rejects the task if it already passes: a command that is green today verifies nothing.\n")
	b.WriteString("  · So the project's test command as is does not work if the suite is already green (`npm test`, `go test ./...`, `pytest`): narrow it to what this task will create — the test file, the module or the package that does not exist yet.\n")
	b.WriteString("  · Examples that fail today because the target does not exist: \"go test ./internal/wol/...\", \"node --test test/validator.test.js\", \"pytest tests/test_wol.py\", \"npm test -- validator\".\n")
	b.WriteString("- \"only_touch\": array of globs of files the task may touch\n")
	b.WriteString("  · two tasks NEVER share a file in \"only_touch\": they run in parallel and collide on integration. If several need the same one (go.mod, package.json, a router), only one touches it and the others list that task in \"depends_on\".\n")
	if c.PruebasPropias {
		b.WriteString("  · in this project the task writes its own tests: if \"done_when\" points to a test file, THAT file must also be in \"only_touch\", or nobody can create it.\n")
	}
	if len(c.Vedadas) > 0 {
		b.WriteString("  · \"only_touch\" can NEVER include these paths (the project or the blind examiner owns them, not the task): " + strings.Join(c.Vedadas, ", ") + "\n")
	}
	if len(c.Ocupados) > 0 {
		b.WriteString("  · these paths are ALREADY being touched by another running task; \"only_touch\" must not overlap them or the task is rejected before it runs:\n")
		for _, id := range idsOrdenados(c.Ocupados) {
			b.WriteString("      " + id + ": " + strings.Join(c.Ocupados[id], ", ") + "\n")
		}
	}
	b.WriteString("- \"depends_on\": array of ids (e.g. \"T-001\") of tasks that must be green before this one; empty if it depends on none\n")
	if c.PrimerID != "" && c.PrimerID != "T-001" {
		b.WriteString("  · the repo already has tasks: yours get consecutive ids starting at " + c.PrimerID + " in array order. Use those ids in \"depends_on\", in the FILE TREE and in the PUBLIC SIGNATURES; to depend on an earlier task use its id.\n")
	}
	b.WriteString("- \"exposes\": array of EXACT public signatures this task produces and another consumes, with input and return types (e.g. \"wol.Send(mac string, addr string) error\", \"POST /wake\"); empty if it produces none\n")
	if len(c.Expuestas) > 0 {
		b.WriteString("  · signatures earlier tasks already expose; if you consume them, copy them word for word into \"uses\" and put their id in \"depends_on\" (never expose them again):\n")
		for _, id := range idsOrdenados(c.Expuestas) {
			b.WriteString("      " + id + ": " + strings.Join(c.Expuestas[id], " · ") + "\n")
		}
	}
	b.WriteString("- \"uses\": array of signatures from OTHER tasks this one consumes, copied word for word from that task's \"exposes\" (same name, same types); empty if it consumes none. If you cannot copy the exact signature, the task is badly split: resolve the dependency in the plan, not in the executor\n")
	b.WriteString("- \"weight\": \"light\", \"medium\" or \"heavy\" by complexity: light for implementation with decisions already made, medium for complex logic and heavy for architectural foundations or high uncertainty\n")
	b.WriteString("- \"max_lines\": 0. Do not impose a maximum of lines or split by size: split by responsibility and dependencies. Positive limits are the human's call.\n")
	if len(c.Agentes) > 0 {
		var ags []string
		for nombre, a := range c.Agentes {
			desc := nombre
			if len(a.Skills) > 0 {
				desc += fmt.Sprintf(" (skills: %s)", strings.Join(a.Skills, ", "))
			}
			ags = append(ags, desc)
		}
		sort.Strings(ags)
		fmt.Fprintf(&b, "- \"agent\": name of the agent assigned to this task (available: %s); or \"\" for the default executor\n", strings.Join(ags, "; "))
	}
	if len(c.Skills) > 0 {
		b.WriteString("- \"skills\": array with the skills THIS task needs, from this catalog; their whole text enters the executor prompt, so each one costs tokens on every attempt. List only those that change how this task is written (e.g. visual design only in tasks that draw interface) and [] if none is needed:\n")
		for _, sk := range c.Skills {
			fmt.Fprintf(&b, "  - %s: %s\n", sk.Nombre, recortar(sk.Descripcion, 200))
		}
	}
	b.WriteString("- \"risks\": risks or limitations, or \"\" if none\n")
	b.WriteString("- \"how\": instructions sufficient to execute without redesigning: concrete files, steps, inputs and outputs, errors, edge cases, dependencies and tests covering the requirements. Do not limit it to one line.\n\n")
	b.WriteString("Tasks run in parallel and isolated: they cannot read each other's code. If one produces something another needs, the signature MUST appear identically in the \"exposes\" of the producer and in the \"uses\" of the consumer; otherwise each will invent its own and they will not fit.\n\n")
	b.WriteString("And do not promise more than needed: what a task supports and its consumer never asks for is tested by nobody, even if both end up green. If a piece has to accept an operator, a format or an edge case, the \"done_when\" of the task that consumes it must cover it, or do not declare it in the \"exposes\" of the one that produces it.\n\n")
	b.WriteString("Example:\n{\n  \"architecture\": \"Existing Go module. internal/wol/send.go builds and sends the UDP packet; internal/api/wake.go adapts HTTP to wol.Send. The handler composes the call without duplicating UDP logic. Each module keeps its tests next to the code.\",\n  \"tasks\": [\n    {\n      \"title\": \"send magic packet\",\n      \"why\": \"it is the core action\",\n      \"done_when\": \"go test ./internal/wol/...\",\n      \"only_touch\": [\n        \"internal/wol/**\"\n      ],\n      \"exposes\": [\n        \"wol.Send(mac, addr string) error\"\n      ],\n      \"uses\": [],\n      \"depends_on\": [],\n      \"weight\": \"light\",\n      \"max_lines\": 0,\n      \"risks\": \"\",\n      \"how\": \"Implement Send in internal/wol/send.go: validate the MAC, build 6 bytes of FF plus 16 repetitions of the MAC, send UDP to addr and propagate errors. Verify the exact packet, an invalid MAC and a send failure.\"\n    },\n    {\n      \"title\": \"http endpoint that triggers wol\",\n      \"why\": \"automation invokes it\",\n      \"done_when\": \"go test ./internal/api/...\",\n      \"only_touch\": [\n        \"internal/api/**\"\n      ],\n      \"exposes\": [\n        \"POST /wake\"\n      ],\n      \"uses\": [\n        \"wol.Send(mac, addr string) error\"\n      ],\n      \"depends_on\": [\n        \"T-001\"\n      ],\n      \"weight\": \"light\",\n      \"max_lines\": 0,\n      \"risks\": \"\",\n      \"how\": \"Create and register POST /wake in internal/api/wake.go using the existing router in that scope. Read JSON mac and addr, call wol.Send, answer 204 on success, 400 for invalid input and 502 if UDP fails. Verify the responses and that a valid request invokes the send.\"\n    }\n  ]\n}")
	b.WriteString(idioma.Instruccion())
	return b.String()
}

// contextoPrompt writes the context section from what was detected in the
// repo. In an empty repo it enables greenfield mode: the first task
// initializes the stack and sets its real verification command.
func contextoPrompt(c Contexto) string {
	var b strings.Builder
	if c.EsVacio {
		b.WriteString("The repository is empty (no code yet). You must start from scratch:\n")
		if c.Stack != "" {
			fmt.Fprintf(&b, "- The human chose the stack: %s. Use it for all tasks.\n", c.Stack)
		} else {
			b.WriteString("- Choose ONE stack (Go, Node, Python, ...) and fix its structure. The FIRST task initializes it following those decisions and leaves the project compiling.\n")
		}
		b.WriteString("- The first task leaves the project compiling; its \"done_when\" is the real build or test command of the stack (e.g. \"go build ./...\" for Go, \"npm test\" for Node).\n")
		b.WriteString("- The other tasks build on that base, with real \"done_when\" commands of the same stack that fail today, and list in \"depends_on\" the task that created the base (and any other they depend on).\n")
		b.WriteString("- Keep ONE language across all tasks.\n")
		if c.Requisitos != "" {
			fmt.Fprintf(&b, "- Besides the request, the human asked for: %s\n", c.Requisitos)
		}
		return b.String()
	}
	if c.Lenguaje == "" {
		return "The repository already exists but no specific language was detected. Inspect it before choosing commands and use the test command that already exists, if any."
	}
	pruebas := c.Pruebas
	if strings.TrimSpace(pruebas) == "" {
		pruebas = "none detected · propose one that exists or the language standard"
	}
	return fmt.Sprintf("The repository is already under development. Detected language: %s. Project test command: %s.\n"+
		"- Keep that language and that test runner; do not change the stack or propose packages or paths that do not exist.\n"+
		"- But do NOT copy that command as is into \"done_when\" if the project suite is already green: it would pass today and loom would reject the task. Point the same runner at what the task will create and does not exist yet.",
		c.Lenguaje, pruebas)
}

// Parse extrae la lista de borradores de la respuesta del modelo, tolerando
// vallas markdown y texto alrededor. Un borrador sin titulo ni listo_cuando
// se rechaza: un plan sin criterio de "listo" no es un plan.
func Parse(texto string) ([]Borrador, error) {
	// Conserva compatibilidad con el array de contratos de versiones anteriores.
	t := CuerpoJSON(texto)
	if t == "" {
		return nil, errors.New(idioma.T("el modelo no devolvió un plan JSON · vuelve a intentarlo"))
	}
	ini := 0
	var bs []Borrador
	var arquitectura string
	dec := json.NewDecoder(strings.NewReader(EscaparControles(t)))
	if t[ini] == '{' {
		var documento struct {
			Arquitectura string     `json:"arquitectura"`
			Tareas       []Borrador `json:"tareas"`
		}
		var crudo json.RawMessage
		if err := dec.Decode(&crudo); err != nil {
			return nil, fmt.Errorf(idioma.T("el modelo devolvió JSON inválido · %s"), err)
		}
		if err := json.Unmarshal(idioma.NormalizarClaves(crudo, map[string]string{"architecture": "arquitectura", "tasks": "tareas"}), &documento); err != nil {
			return nil, fmt.Errorf(idioma.T("el modelo devolvió JSON inválido · %s"), err)
		}
		if strings.TrimSpace(documento.Arquitectura) == "" {
			return nil, errors.New(idioma.T("el plan no define arquitectura · incluye decisiones y árbol de archivos"))
		}
		bs, arquitectura = documento.Tareas, documento.Arquitectura
	} else if err := dec.Decode(&bs); err != nil {
		return nil, fmt.Errorf(idioma.T("el modelo devolvió JSON inválido · %s"), err)
	}
	for i := range bs {
		if arquitectura != "" {
			if strings.TrimSpace(bs[i].Como) == "" {
				return nil, fmt.Errorf(idioma.T("la tarea %d no trae instrucciones de implementación en como"), i+1)
			}
			bs[i].Como = MarcaArquitectura + arquitectura + "\n\n" + MarcaImplementacion + bs[i].Como
		}
	}

	if len(bs) == 0 {
		return nil, ErrSinTareas
	}
	for i, b := range bs {
		if strings.TrimSpace(b.Titulo) == "" || strings.TrimSpace(b.ListoCuando) == "" {
			return nil, fmt.Errorf(idioma.T("la tarea %d del plan no trae titulo ni listo_cuando · el modelo se desvió"), i+1)
		}
	}
	return bs, nil
}

// CuerpoJSON devuelve el valor JSON más largo que hay en la respuesta de
// un modelo, desde su primer carácter. Tomar el primer "{" del texto tiraba
// la respuesta cuando el modelo narraba antes de responder y la prosa traía
// llaves ("{campo}" en un párrafo): una ronda de corrección pagada por un
// plan que estaba bien. Si nada decodifica, devuelve desde el
// primer "{" o "[" para que el error del decodificador sea el del plan.
func CuerpoJSON(texto string) string {
	mejor, primero, largo := "", "", 0
	for i := 0; i < len(texto); i++ {
		if texto[i] != '{' && texto[i] != '[' {
			continue
		}
		if primero == "" {
			primero = texto[i:]
		}
		var crudo json.RawMessage
		dec := json.NewDecoder(strings.NewReader(EscaparControles(texto[i:])))
		if dec.Decode(&crudo) != nil {
			continue
		}
		if len(crudo) > largo {
			mejor, largo = texto[i:], len(crudo)
		}
		// lo de adentro de un valor que ya decodificó es más corto
		i += int(dec.InputOffset()) - 1
	}
	if mejor == "" {
		return primero
	}
	return mejor
}

// EscaparControles escapa saltos de línea y tabuladores crudos dentro de
// strings JSON. Los modelos escriben "arquitectura" con párrafos reales
// en vez de \n, y el decoder rechazaba un plan de 49k tokens entero.
func EscaparControles(s string) string {
	var b strings.Builder
	enString, escape := false, false
	for _, r := range s {
		switch {
		case escape:
			escape = false
		case enString && r == '\\':
			escape = true
		case r == '"':
			enString = !enString
		case enString && r == '\n':
			b.WriteString(`\n`)
			continue
		case enString && r == '\r':
			b.WriteString(`\r`)
			continue
		case enString && r == '\t':
			b.WriteString(`\t`)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Generar pide el plan y lo parsea.
func Generar(ctx context.Context, g Generador, c Contexto, frase string) ([]Borrador, error) {
	texto, err := g.Generar(ctx, Prompt(frase, c))
	if err != nil {
		return nil, err
	}
	return Parse(texto)
}

// Completar pide los contratos de las tareas de un spec rápido y exige
// uno por tarea: un modelo que reparte o se salta una desalinea todo lo
// que viene después.
func Completar(ctx context.Context, g Generador, c Contexto, feature string, reglas []string, tareas []task.Task) ([]Borrador, error) {
	texto, err := g.Generar(ctx, PromptCompletar(feature, reglas, tareas, c))
	if err != nil {
		return nil, err
	}
	bs, err := Parse(texto)
	if err != nil {
		return nil, err
	}
	if len(bs) != len(tareas) {
		return nil, fmt.Errorf(idioma.T("el modelo devolvió %d contratos para %d tareas · vuelve a intentarlo"), len(bs), len(tareas))
	}
	return bs, nil
}

// AcotarLimiteLineas conserva el límite elegido por el humano. La estimación
// del modelo no crea ni modifica una restricción de entrega; 0 es sin tope.
func AcotarLimiteLineas(propuesto, porDefecto int) int {
	return porDefecto
}

// idsOrdenados devuelve las claves en orden estable: el prompt tiene que
// ser reproducible para que la caché del proveedor sirva de algo.
func idsOrdenados(m map[string][]string) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// recortar acota s a max runas, para que una descripción larga de skill
// no infle el prompt del planificador.
func recortar(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}

var clavesBorrador = map[string]string{
	"title": "titulo", "why": "porque", "done_when": "listo_cuando", "only_touch": "tocar_solo",
	"dont_touch": "no_tocar", "depends_on": "depende_de", "exposes": "expone", "uses": "usa",
	"risks": "riesgos", "weight": "peso", "agent": "agente", "covers": "cubre",
	"max_lines": "limite_lineas", "how": "como",
}
