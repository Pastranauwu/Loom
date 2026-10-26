// Package spec implementa el modelo declarativo "Requerimientos como Código":
// un archivo YAML (ej. loom.spec.yml) define la feature, reglas y la lista
// de tareas con sus criterios de aceptación y dependencias, permitiendo sincronizarlas
// y ejecutarlas en paralelo al estilo Docker Compose.
package spec

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Pastranauwu/loom/internal/kv"
	"github.com/Pastranauwu/loom/internal/task"
)

// DefaultSpecNames son los nombres por defecto que loom busca en la raíz del repo.
var DefaultSpecNames = []string{
	"loom.spec.yml",
	"loom.spec.yaml",
	"loom.specs.yml",
	"loom.specs.yaml",
	"spec.yml",
	"spec.yaml",
	".loom/spec.yml",
	".loom/spec.yaml",
	// nombres anteriores al cambio de loom
	"devclean.spec.yml",
	"devclean.spec.yaml",
	"devclean.specs.yml",
	"devclean.specs.yaml",
	".devclean/spec.yml",
	".devclean/spec.yaml",
}

// Limites define límites por defecto para todas las tareas de una especificación.
type Limites struct {
	Intentos int `json:"intentos,omitempty"`
	Lineas   int `json:"lineas,omitempty"`
}

// Spec es la especificación declarativa de una feature o conjunto de tareas.
type Spec struct {
	Version int    `json:"version"`
	Feature string `json:"feature"`
	// Motivo es por qué se pide el feature, en una o dos líneas. No entra
	// al plan: va al índice del historial al archivar.
	Motivo string `json:"motivo,omitempty"`
	Agente string `json:"agente,omitempty"`
	// Agentes es cuántas tareas corren en paralelo (el --agentes de run/up).
	// El flag de la línea de comandos gana sobre el spec.
	Agentes int `json:"agentes,omitempty"`
	// Ship pide entregar todo en un PR al terminar la corrida (el --ship de
	// up). Es la manera de que "levantar el spec" sea también "entregarlo".
	Ship    bool     `json:"ship,omitempty"`
	Limites Limites  `json:"limites,omitempty"`
	Reglas  []string `json:"reglas,omitempty"`
	// Requirements y Acceptance son la interfaz humana. Tasks es el IR:
	// puede venir escrito por un usuario avanzado o ser generado.
	Requirements []string `json:"requirements,omitempty"`
	// RequirementIDs va alineado con Requirements: el id explícito del
	// spec o vacío. Usa IDs(), que completa los vacíos.
	RequirementIDs []string     `json:"requirement_ids,omitempty"`
	Acceptance     []Acceptance `json:"acceptance,omitempty"`
	Constraints    Constraints  `json:"constraints,omitempty"`
	Tasks          []task.Task  `json:"tasks"`
	// Previos son los requirements ya planeados en una corrida anterior
	// del mismo spec (IntencionFile). Los llena runApply; nunca vienen
	// del YAML.
	Previos    []string `json:"-" yaml:"-"`
	Delta      Delta    `json:"-" yaml:"-"`
	PreviosIDs []string `json:"-" yaml:"-"`
}

type Acceptance struct {
	Criterion string `json:"criterion" yaml:"criterion"`
	Command   string `json:"command,omitempty" yaml:"command,omitempty"`
}

type Constraints struct {
	NoTocar []string `json:"no_tocar,omitempty" yaml:"no_tocar,omitempty"`
}

// Find busca el archivo de especificación en la raíz del repo.
func Find(root string) (string, error) {
	for _, name := range DefaultSpecNames {
		p := filepath.Join(root, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf(idioma.T("no se encontró archivo de especificación (probó: %s)"), strings.Join(DefaultSpecNames, ", "))
}

// Load lee y parsea una especificación desde el disco.
func Load(path string) (Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, err
	}
	return Parse(data)
}

// AssignCorrelativeIDs asigna IDs correlativos (T-001, T-002, ...) a las tareas que no tengan ID asignado.
func AssignCorrelativeIDs(tasksDir string, tasks []task.Task) ([]task.Task, error) {
	out := make([]task.Task, len(tasks))
	copy(out, tasks)

	var needID []int
	for i, t := range out {
		if strings.TrimSpace(t.ID) == "" {
			needID = append(needID, i)
		}
	}

	if len(needID) > 0 {
		var nextNum int = 1
		if existing, err := task.List(tasksDir); err == nil && len(existing) > 0 {
			var ids []string
			for _, e := range existing {
				ids = append(ids, e.ID)
			}
			sort.Strings(ids)
			last := ids[len(ids)-1]
			var n int
			if _, err := fmt.Sscanf(last, "T-%d", &n); err == nil {
				nextNum = n + 1
			}
		}

		for _, idx := range needID {
			out[idx].ID = fmt.Sprintf("T-%03d", nextNum)
			nextNum++
		}
	}

	// sin un solo id escrito, quien armó el spec no pudo saber qué ids le
	// iban a tocar: su "T-001" es la primera tarea DEL SPEC, no la T-001
	// que ya hubiera en el repo
	if len(needID) == len(out) {
		ids := make([]string, len(out))
		for i := range out {
			ids[i] = out[i].ID
		}
		for i := range out {
			out[i].DependeDe = task.DependenciasPorPosicion(out[i].DependeDe, ids)
		}
	}

	return out, nil
}

// notasConReglas antepone las reglas de la especificación a las notas
// propias de una tarea. El bucle inyecta las notas en cada prompt
// ("Notas:" en loop.promptPara), así que este es el canal por el que
// las reglas llegan al agente sin tocar el contrato ni la constitución.
func notasConReglas(reglas []string, notas string) string {
	var b strings.Builder
	b.WriteString("Spec rules:\n")
	for _, r := range reglas {
		b.WriteString("- " + r + "\n")
	}
	if n := strings.TrimSpace(notas); n != "" {
		b.WriteString("\n" + n)
	}
	return strings.TrimRight(b.String(), "\n")
}

// appendUnique devuelve dst con los xs que no estaban, sin escribir sobre el
// arreglo de dst: el spec original no se contamina al aplicarlo.
func appendUnique(dst []string, xs ...string) []string {
	out := append([]string(nil), dst...)
	seen := map[string]bool{}
	for _, x := range out {
		seen[x] = true
	}
	for _, x := range xs {
		if !seen[x] {
			out = append(out, x)
			seen[x] = true
		}
	}
	return out
}

// Apply valida y guarda las tareas de la especificación en tasksDir (.loom/tasks/).
func Apply(tasksDir string, s Spec, dryRun bool) ([]task.Task, error) {
	tasksWithIDs, err := AssignCorrelativeIDs(tasksDir, s.Tasks)
	if err != nil {
		return nil, err
	}

	defIntentos := task.DefaultLimiteIntentos
	if s.Limites.Intentos > 0 {
		defIntentos = s.Limites.Intentos
	}
	defLineas := task.DefaultLimiteLineas
	if s.Limites.Lineas > 0 {
		defLineas = s.Limites.Lineas
	}

	// Aplicar defaults y agente por defecto si no vienen
	for i := range tasksWithIDs {
		if tasksWithIDs[i].Version == 0 {
			tasksWithIDs[i].Version = task.Version
		}
		if tasksWithIDs[i].Agente == "" && s.Agente != "" {
			tasksWithIDs[i].Agente = s.Agente
		}
		if tasksWithIDs[i].LimiteIntentos == 0 {
			tasksWithIDs[i].LimiteIntentos = defIntentos
		}
		if tasksWithIDs[i].LimiteLineas == 0 {
			tasksWithIDs[i].LimiteLineas = defLineas
		}
		// las restricciones del humano tienen que sobrevivir el viaje
		// spec -> IR. Se aplican aquí y no en Parse porque el
		// planificador agrega sus contratos DESPUÉS de parsear: en el
		// camino de requirements sin tasks, Parse no tiene nada que
		// restringir todavía.
		if len(s.Constraints.NoTocar) > 0 {
			tasksWithIDs[i].NoTocar = appendUnique(tasksWithIDs[i].NoTocar, s.Constraints.NoTocar...)
		}
		// las reglas de la especificación van al prompt de cada tarea:
		// se anteponen a sus notas, que es el canal que el bucle ya
		// inyecta ("Notas:" en promptPara). Se componen aquí y no en
		// Parse para que un Marshal posterior no las escriba dos veces.
		if len(s.Reglas) > 0 {
			tasksWithIDs[i].Notas = notasConReglas(s.Reglas, tasksWithIDs[i].Notas)
		}
	}
	previas, err := task.List(tasksDir)
	if err != nil {
		return nil, err
	}
	if issues := ValidatePlan(s, tasksWithIDs, previas...); len(issues) > 0 {
		var fatal []string
		for _, issue := range issues {
			if issue.Level == "error" {
				fatal = append(fatal, issue.Message)
			}
		}
		if len(fatal) > 0 {
			return nil, fmt.Errorf(idioma.T("plan inválido: %s"), strings.Join(fatal, " · "))
		}
	}

	// Validar todas las tareas antes de escribir nada
	for _, t := range tasksWithIDs {
		if errs := t.Validate(); len(errs) > 0 {
			var msgs []string
			for _, e := range errs {
				msgs = append(msgs, e.Error())
			}
			return nil, fmt.Errorf(idioma.T("tarea %s inválida: %s"), t.ID, strings.Join(msgs, " · "))
		}
	}

	if dryRun {
		return tasksWithIDs, nil
	}

	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		return nil, err
	}

	for _, t := range tasksWithIDs {
		if err := task.Save(tasksDir, t); err != nil {
			return nil, err
		}
	}

	return tasksWithIDs, nil
}

// Marshal genera el contenido YAML de una especificación.
func Marshal(s Spec) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "version: %d\n", s.Version)
	if s.Feature != "" {
		fmt.Fprintf(&b, "feature: %s\n", kv.Quote(s.Feature))
	}
	if s.Motivo != "" {
		fmt.Fprintf(&b, "why: %s\n", kv.Quote(s.Motivo))
	}
	if s.Agente != "" {
		fmt.Fprintf(&b, "agent: %s\n", s.Agente)
	}
	if s.Agentes > 0 {
		fmt.Fprintf(&b, "agents: %d\n", s.Agentes)
	}
	if s.Ship {
		b.WriteString("ship: true\n")
	}
	if s.Limites.Intentos > 0 || s.Limites.Lineas > 0 {
		if s.Limites.Intentos > 0 {
			fmt.Fprintf(&b, "max_retries: %d\n", s.Limites.Intentos)
		}
		if s.Limites.Lineas > 0 {
			fmt.Fprintf(&b, "max_lines: %d\n", s.Limites.Lineas)
		}
	}
	if len(s.Reglas) > 0 {
		b.WriteString("rules:\n")
		for _, r := range s.Reglas {
			fmt.Fprintf(&b, "  - %s\n", kv.Quote(r))
		}
	}
	if len(s.Requirements) > 0 {
		b.WriteString("requirements:\n")
		for _, r := range s.Requirements {
			fmt.Fprintf(&b, "  - %s\n", kv.Quote(r))
		}
	}
	if len(s.Acceptance) > 0 {
		b.WriteString("acceptance:\n")
		for _, a := range s.Acceptance {
			if a.Command == "" {
				fmt.Fprintf(&b, "  - %s\n", kv.Quote(a.Criterion))
				continue
			}
			fmt.Fprintf(&b, "  - criterion: %s\n    command: %s\n", kv.Quote(a.Criterion), kv.Quote(a.Command))
		}
	}
	if len(s.Constraints.NoTocar) > 0 {
		b.WriteString("constraints:\n")
		fmt.Fprintf(&b, "  dont_touch: %s\n", kv.MarshalList(s.Constraints.NoTocar))
	}
	if len(s.Tasks) > 0 {
		b.WriteString("\ntasks:\n")
	}
	for _, t := range s.Tasks {
		fmt.Fprintf(&b, "  - id: %s\n", t.ID)
		fmt.Fprintf(&b, "    title: %s\n", kv.Quote(t.Titulo))
		if t.Porque != "" {
			fmt.Fprintf(&b, "    why: %s\n", kv.Quote(t.Porque))
		}
		fmt.Fprintf(&b, "    done_when: %s\n", kv.Quote(t.ListoCuando))
		if len(t.TocarSolo) > 0 {
			fmt.Fprintf(&b, "    only_touch: %s\n", kv.MarshalList(t.TocarSolo))
		}
		if len(t.NoTocar) > 0 {
			fmt.Fprintf(&b, "    dont_touch: %s\n", kv.MarshalList(t.NoTocar))
		}
		if len(t.DependeDe) > 0 {
			fmt.Fprintf(&b, "    depends_on: %s\n", kv.MarshalList(t.DependeDe))
		}
		if len(t.Expone) > 0 {
			fmt.Fprintf(&b, "    exposes: %s\n", kv.MarshalList(t.Expone))
		}
		if len(t.Usa) > 0 {
			fmt.Fprintf(&b, "    uses: %s\n", kv.MarshalList(t.Usa))
		}
		if t.Peso != "" {
			fmt.Fprintf(&b, "    weight: %s\n", task.PesoEn(t.Peso))
		}
		if t.Agente != "" && t.Agente != s.Agente {
			fmt.Fprintf(&b, "    agent: %s\n", t.Agente)
		}
		if t.Skills != nil {
			fmt.Fprintf(&b, "    skills: %s\n", kv.MarshalList(t.Skills))
		}
		if t.LimiteIntentos > 0 {
			fmt.Fprintf(&b, "    max_retries: %d\n", t.LimiteIntentos)
		}
		fmt.Fprintf(&b, "    max_lines: %d\n", t.LimiteLineas)
		if t.Riesgos != "" {
			fmt.Fprintf(&b, "    risks: %s\n", kv.Quote(t.Riesgos))
		}
		if t.Notas != "" {
			fmt.Fprintf(&b, "    notes: %s\n", kv.Quote(t.Notas))
		}
	}
	return []byte(b.String())
}

// IDRequerimiento es el id de un requerimiento sin id explícito: R- y el
// principio del sha1 de su texto normalizado. Estable mientras el texto
// no cambie; para sobrevivir a una reescritura, el spec declara id.
func IDRequerimiento(texto string) string {
	h := sha1.Sum([]byte(strings.Join(strings.Fields(strings.ToLower(texto)), " ")))
	return "R-" + hex.EncodeToString(h[:])[:6]
}

// IDs devuelve el id de cada requerimiento, alineado con Requirements.
func (s Spec) IDs() []string {
	out := make([]string, len(s.Requirements))
	for i, r := range s.Requirements {
		if i < len(s.RequirementIDs) && s.RequirementIDs[i] != "" {
			out[i] = s.RequirementIDs[i]
		} else {
			out[i] = IDRequerimiento(r)
		}
	}
	return out
}
