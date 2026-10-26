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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	}
}

// Base es lo que todo implementador recibe, sea cual sea su tarea. Va en
// la parte común del prompt, así que es idéntica en todos los agentes.
//
// Reemplaza a tres paquetes que se inyectaban enteros en cada intento:
// implement (pedía /tdd y /code-review: el agente invocaba la skill
// code-review de Claude Code, ~5% del costo de una corrida, sobre un
// trabajo que el revisor y la esclusa ya juzgan), clean-code (4,8 KB;
// queda completa en el catálogo) y caveman (estilo de prosa; aquí basta
// una línea).
const Base = `Cómo trabajar:
- Implementa lo que pide el contrato, nada fuera de tu alcance.
- Corre seguido las pruebas de lo que tocas y el listo_cuando al final.
- Haz commit de tu trabajo en la rama actual.
- No narres lo que haces: actúa. Respuesta final breve.

Código limpio:
- Nombres que dicen qué son, sin abreviaturas crípticas.
- Funciones cortas que hacen una sola cosa, con pocos parámetros y sin efectos ocultos.
- Reusa lo que ya existe antes de escribir algo nuevo; nada duplicado.
- Errores explícitos: no los tragues ni devuelvas valores mágicos.
- Comentarios solo para el porqué; ni código comentado ni prints de depuración.`

// UI es lo que recibe toda tarea que toca archivos de interfaz (TocaUI),
// venga del esqueleto o del plan, en cualquier stack. Reemplaza a
// frontend-design: 9.4 KB en cada intento que pedían un diseño
// "distintivo" y huir de lo estándar, lo contrario de una interfaz clara
// hecha con las piezas que el proyecto ya tiene.
const UI = `Interfaz:
- Usa el sistema de diseño del proyecto (lo describe ARCHITECTURE.md o se ve en el código): sus componentes, sus tokens de tema y su forma de estilar. No agregues otra librería de UI ni mezcles estilos: nada de CSS suelto, estilos en línea o colores fijos si el proyecto tiene tokens.
- Reusa los componentes que ya existen antes de escribir uno. Si la librería del proyecto trae un CLI para agregar componentes, úsalo en vez de escribirlos a mano.
- Cada pantalla sigue la plantilla del proyecto (layout, encabezado, contenido) y cubre sus estados: cargando, vacío (mensaje y acción) y error (mensaje y reintentar).
- Mobile-first: una columna en pantallas chicas que se abre en las grandes; objetivos táctiles de 44px o más.
- Formularios: etiqueta visible en cada campo, el error junto al campo, el botón principal deshabilitado mientras envía.
- Accesible: HTML semántico (button, nav, main, títulos en orden), texto alternativo en imágenes, foco visible, nada que se entienda solo por color.
- Una acción principal por pantalla; la jerarquía con tamaño, peso y espacio, no con colores extra.
- Si el contrato pide un estilo, que se note a simple vista en la pantalla: un revisor mira capturas del resultado. Un efecto que casi no se ve cuenta como no hecho.
- Las pruebas de interfaz verifican lo que el usuario ve y hace (textos, roles, estados), no clases CSS.`

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
		return fmt.Errorf("npx no está instalado · instala Node.js para traer skills")
	}
	cmd := exec.CommandContext(ctx, "npx", "--yes", "skills", "add", src.Repo,
		"--skill", src.arg(), "-y", "--agent", "universal")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("no se pudo traer skill %s · %s", src.Nombre, tail(string(out)))
	}
	if !Instalado(root, src.Nombre) {
		return fmt.Errorf("skill %s no apareció en el repo tras el fetch · revisa que el paquete la incluya", src.Nombre)
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
		fmt.Fprintf(&b, "### Skill: %s\n%s\n\n", n, sk.Cuerpo)
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
