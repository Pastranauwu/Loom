// Package skills trae e inyecta skills reales (SKILL.md, formato de
// skills.sh / Claude Code) en el prompt de cada agente, en vez del simple
// nombre-etiqueta que usaba config.Agente.Skills. El fetch corre una vez
// contra la raíz del repo (nunca dentro de un cuarto: los cuartos son
// worktrees separados y no ven archivos sin commitear de otro worktree),
// y el contenido se inyecta como texto — así cualquier ejecutor (claude,
// opencode) lo recibe igual, sin depender de que la CLI del agente
// descubra skills por su cuenta en modo headless.
package skills

import (
	"context"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Source es un paquete de skill instalable.
type Source struct {
	// Nombre es el slug con el que loom identifica la skill en todos
	// lados: config.yml, DefaultAgentes, y la carpeta instalada
	// (.agents/skills/<Nombre>/).
	Nombre string
	Repo   string
	// SkillArg es el valor exacto que exige `--skill` en el repo de
	// origen. Casi siempre es igual a Nombre; algunos repos (p. ej.
	// anthropics/claude-code) publican el título tal cual ("Agent
	// Development") y no su slug, aunque el CLI igual instala en la
	// carpeta con el slug. Vacío = usar Nombre.
	SkillArg string
}

// arg devuelve el valor a pasar a `--skill`.
func (s Source) arg() string {
	if s.SkillArg != "" {
		return s.SkillArg
	}
	return s.Nombre
}

// DefaultSources son los paquetes que loom trae por defecto: el
// catálogo del que el planificador elige por tarea (campo `skills` del
// contrato) y el que usan los roles cuando el contrato no lo declara.
func DefaultSources() []Source {
	return []Source{
		{Nombre: "clean-code", Repo: "https://github.com/sickn33/agentic-awesome-skills"},
		{Nombre: "create-a-backend", Repo: "https://github.com/vercel/vercel-plugin"},
		{Nombre: "test-driven-development", Repo: "https://github.com/obra/superpowers"},
		{Nombre: "impeccable", Repo: "https://github.com/pbakaus/impeccable"},
		{Nombre: "find-skills", Repo: "https://github.com/vercel-labs/skills"},
	}
}

// Asegurar trae las skills nombradas que falten y tengan fuente conocida.
// Corre antes de inyectarlas: así una tarea de interfaz recibe la skill de
// diseño aunque el repo se haya iniciado antes de que existiera, sin
// depender de que alguien corra `loom skills sync`. Serializado: las
// tareas paralelas comparten .agents/skills y npx no tolera dos installs
// a la vez. Devuelve los fallos para reportarlos; nunca bloquea.
func Asegurar(ctx context.Context, root string, nombres []string) []error {
	fetchMu.Lock()
	defer fetchMu.Unlock()
	var errs []error
	for _, n := range nombres {
		if Instalado(root, n) {
			continue
		}
		for _, src := range DefaultSources() {
			if src.Nombre != n {
				continue
			}
			if err := Fetch(ctx, root, src); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

var fetchMu sync.Mutex

// Base es lo que todo implementador recibe, sea cual sea su tarea. Va en
// la parte común del prompt, así que es idéntica en todos los agentes.
//
// Reemplaza a tres paquetes que se inyectaban enteros en cada intento:
// implement (pedía /tdd y /code-review: el agente invocaba la skill
// code-review de Claude Code, ~5% del costo de una corrida, sobre un
// trabajo que el revisor y la esclusa ya juzgan), clean-code (4,8 KB;
// queda completa en el catálogo) y caveman (estilo de prosa; aquí basta
// una línea).
const Base = `How to work:
- Implement what the contract asks, nothing outside your scope.
- Run the tests of what you touch often, and done_when at the end.
- Commit your work on the current branch.
- Do not narrate what you do: act. Keep the final reply short.

Code is machine IR: the human audits the contracts (spec, DNA, exposes), not function bodies; the compiler and the tests are the readers.
- Type-driven design: make invalid states unrepresentable. Newtypes for domain values, exhaustive enums or discriminated unions for states, strict signatures. No stringly-typed data, no boolean flags that encode a state, no null where absence is not a real state.
- In code, machine efficiency over surface aesthetics: performance, determinism and memory safety first; no unnecessary allocations or copies; no syntactic sugar that costs either. The look of an interface is not surface: it is the deliverable.
- Reuse what already exists before writing something new; nothing duplicated.
- Errors are typed values: do not swallow them or return magic values.
- Zero explanatory comments: only tool directives (//go:build, //nolint, #[allow(...)]).
- Zero dead or commented-out code, zero debug prints.
- Zero redundant wrappers: no 2 or 3 line functions or layers just to name a step.
- Only what exposes declares gets a descriptive public name and signature: other agents call it without reading the body.
- If you implement a skeleton stub, delete its Idea:/Cases: block when done.
- Missing know-how the contract assumes (a domain, a library, a design practice)? ` + "`npx skills find <topic>`" + ` lists skills and ` + "`npx skills add <owner/repo@skill> -g -y`" + ` installs one in ~/.agents/skills/<name>/SKILL.md: read it and follow it. Never install into the repo.`

// UI es lo que recibe toda tarea que toca archivos de interfaz (TocaUI),
// venga del esqueleto o del plan, en cualquier stack: las reglas de
// estructura. El criterio de diseño lo pone UISkillName, que entra junto.
// Sin él, en una corrida real 5 tareas de rediseño salieron verdes y la
// interfaz quedó con cara de plantilla.
const UI = `Interface:
- Visual quality is part of the contract: a screen that looks like an unstyled template or a framework default is not done. Give each screen a clear typographic scale, deliberate spacing and composition, and one focal element.
- Use the project's design system (ARCHITECTURE.md describes it or it shows in the code): its components, theme tokens and styling approach. Do not add another UI library or mix styles: no loose CSS, inline styles or hard-coded colors if the project has tokens.
- Reuse existing components before writing one. If the project's library ships a CLI to add components, use it instead of writing them by hand.
- Each screen follows the project's template (layout, header, content) and covers its states: loading, empty (message and action) and error (message and retry).
- Mobile-first: one column on small screens that opens up on large ones; touch targets of 44px or more.
- Forms: a visible label on every field, the error next to the field, the main button disabled while submitting.
- Accessible: semantic HTML (button, nav, main, headings in order), alt text on images, visible focus, nothing understood by color alone.
- One main action per screen; hierarchy through size, weight and space, not extra colors.
- If the contract asks for a style, it must be visible at a glance on screen: a reviewer looks at screenshots of the result. An effect that is barely visible counts as not done.
- UI tests verify what the user sees and does (texts, roles, states), not CSS classes.`

// extensionesUI son los archivos que dibujan interfaz.
var extensionesUI = []string{".tsx", ".jsx", ".vue", ".svelte", ".astro", ".html", ".css", ".scss", ".erb", ".blade.php", ".twig", ".jinja", ".j2", ".hbs", ".razor", ".cshtml"}

// TocaUI reporta si alguna ruta o glob de tocar_solo es de interfaz.
func TocaUI(tocarSolo []string) bool {
	for _, p := range tocarSolo {
		for _, ext := range extensionesUI {
			if strings.HasSuffix(p, ext) {
				return true
			}
		}
	}
	return false
}

// BackendSkillName y PMSkillName son el paquete de cada rol cuando el
// contrato no declara skills.
func BackendSkillName() string { return "create-a-backend" }
func PMSkillName() string      { return "test-driven-development" }

// UISkillName es la skill de diseño que recibe toda tarea que toca
// interfaz (TocaUI), la declare el contrato o no.
func UISkillName() string { return "impeccable" }

// Dir es el directorio canónico donde el CLI `skills` instala el
// contenido (`.agents/skills/<nombre>/SKILL.md`), siempre a raíz del
// repo — no del cuarto.
func Dir(root string) string { return filepath.Join(root, ".agents", "skills") }

// Instalado reporta si un paquete ya está descargado.
func Instalado(root, nombre string) bool {
	_, err := os.Stat(filepath.Join(Dir(root), nombre, "SKILL.md"))
	return err == nil
}

// Fetch trae un paquete con `npx skills add`. Idempotente: si ya está
// instalado, no hace nada. Degrada con error legible si falta npx o si
// el fetch falla (sin red, repo sin ese skill); el llamador decide si
// bloquea o sigue sin esa skill.
func Fetch(ctx context.Context, root string, src Source) error {
	if Instalado(root, src.Nombre) {
		return nil
	}
	if _, err := exec.LookPath("npx"); err != nil {
		return fmt.Errorf(idioma.T("npx no está instalado · instala Node.js para traer skills"))
	}
	cmd := exec.CommandContext(ctx, "npx", "--yes", "skills", "add", src.Repo,
		"--skill", src.arg(), "-y", "--agent", "universal")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(idioma.T("no se pudo traer skill %s · %s"), src.Nombre, tail(string(out)))
	}
	if !Instalado(root, src.Nombre) {
		return fmt.Errorf(idioma.T("skill %s no apareció en el repo tras el fetch · revisa que el paquete la incluya"), src.Nombre)
	}
	return nil
}

// Skill es el contenido parseado de un SKILL.md.
type Skill struct {
	Nombre      string
	Descripcion string
	Cuerpo      string
}

// Read lee y parsea el SKILL.md de un paquete ya instalado.
func Read(root, nombre string) (Skill, error) {
	path := filepath.Join(Dir(root), nombre, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	return parseSkillMD(nombre, string(data)), nil
}

// parseSkillMD separa el frontmatter YAML mínimo (name/description) del
// cuerpo. No usa un parser YAML completo: el frontmatter de SKILL.md es
// siempre `clave: valor` plano entre dos líneas `---`.
func parseSkillMD(nombre, data string) Skill {
	s := Skill{Nombre: nombre}
	lines := strings.Split(data, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		s.Cuerpo = strings.TrimSpace(data)
		return s
	}
	i := 1
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			break
		}
		key, val, ok := strings.Cut(lines[i], ":")
		if !ok || strings.TrimSpace(key) != "description" {
			continue
		}
		val = strings.TrimSpace(val)
		if val != ">" && val != "|" {
			s.Descripcion = strings.Trim(val, "> ")
			continue
		}
		// folded scalar (`description: >`): las líneas indentadas que
		// siguen son el valor, hasta la primera sin indentar.
		var partes []string
		for i++; i < len(lines) && (lines[i] == "" || strings.HasPrefix(lines[i], " ") || strings.HasPrefix(lines[i], "\t")); i++ {
			if t := strings.TrimSpace(lines[i]); t != "" {
				partes = append(partes, t)
			}
		}
		i-- // el for externo vuelve a avanzar
		s.Descripcion = strings.Join(partes, " ")
	}
	if i+1 < len(lines) {
		s.Cuerpo = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
	}
	return s
}

// Content junta el cuerpo de cada skill nombrada que ya está instalada,
// lista para inyectar en el prompt. Una skill que falta se salta en
// silencio — nunca bloquea al agente por una skill no instalada, mismo
// criterio de degradación que el examinador y la constitución.
func Content(root string, nombres []string) string {
	var b strings.Builder
	for _, n := range nombres {
		sk, err := Read(root, n)
		if err != nil {
			continue
		}
		// la ruta deja al agente abrir las referencias que el cuerpo cita
		// (impeccable remite a reference/*.md): el cuarto no las ve.
		fmt.Fprintf(&b, "### Skill: %s (files: %s)\n%s\n\n", n, filepath.Join(Dir(root), n), sk.Cuerpo)
	}
	return strings.TrimSpace(b.String())
}

// Catalogo lista las skills instaladas en .agents/skills, ordenadas por
// nombre: de ahí elige el planificador las de cada tarea. implement y
// caveman quedan fuera aunque sigan instaladas en repos viejos: la Base
// las reemplaza.
func Catalogo(root string) []Skill {
	entradas, err := os.ReadDir(Dir(root))
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range entradas {
		if !e.IsDir() || e.Name() == "implement" || e.Name() == "caveman" {
			continue
		}
		if sk, err := Read(root, e.Name()); err == nil {
			out = append(out, sk)
		}
	}
	return out
}

func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " · ")
}
