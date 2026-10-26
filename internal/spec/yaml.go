package spec

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Pastranauwu/loom/internal/task"
	"gopkg.in/yaml.v3"
)

// Parse usa YAML 1.2 completo. Los Node personalizados mantienen la forma
// corta histórica de tasks ("- hacer algo") y permiten agrupar requirements
// y acceptance por categorías sin cambiar el modelo interno.
func Parse(data []byte) (Spec, error) {
	// `loom archive` deja el spec en 0 bytes; sin esto el decoder
	// respondía "EOF" y nada más
	if strings.TrimSpace(string(data)) == "" {
		return Spec{}, errors.New("el spec está vacío · escribe el siguiente feature (el anterior quedó en .loom/historial)")
	}
	var root yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&root); err != nil {
		return Spec{}, err
	}
	if len(root.Content) == 0 {
		return Spec{Version: 1}, nil
	}
	n := root.Content[0]
	if n.Kind != yaml.MappingNode {
		return Spec{}, fmt.Errorf("la especificación debe ser un mapa YAML")
	}
	s := Spec{Version: 1}
	for i := 0; i < len(n.Content); i += 2 {
		key, val := n.Content[i].Value, n.Content[i+1]
		var err error
		switch key {
		case "version":
			err = val.Decode(&s.Version)
		case "feature", "title", "titulo":
			err = val.Decode(&s.Feature)
		case "why", "motivo":
			err = val.Decode(&s.Motivo)
		case "agent", "agente":
			err = val.Decode(&s.Agente)
		case "agents", "agentes":
			err = val.Decode(&s.Agentes)
			if err == nil && s.Agentes < 1 {
				err = fmt.Errorf("agentes inválido: %d · mínimo 1", s.Agentes)
			}
		case "ship":
			err = val.Decode(&s.Ship)
		case "reglas", "rules":
			s.Reglas, err = flattenStrings(val)
		case "requirements", "requisitos":
			s.Requirements, s.RequirementIDs, err = flattenConIDs(val)
		case "acceptance", "aceptacion":
			s.Acceptance, err = decodeAcceptance(val)
		case "constraints", "restricciones":
			var c struct {
				Es []string `yaml:"no_tocar"`
				En []string `yaml:"dont_touch"`
			}
			if err = val.Decode(&c); err == nil {
				s.Constraints.NoTocar = append(c.En, c.Es...)
			}
		case "limits", "limites":
			var l struct {
				Intentos, Retries int
				Lineas, Lines     int
			}
			if err = val.Decode(&l); err == nil {
				s.Limites = Limites{Intentos: l.Intentos + l.Retries, Lineas: l.Lineas + l.Lines}
			}
		case "max_retries", "limite_intentos":
			err = val.Decode(&s.Limites.Intentos)
		case "max_lines", "limite_lineas":
			err = val.Decode(&s.Limites.Lineas)
		case "tasks", "tareas":
			s.Tasks, err = decodeTasks(val)
		case "architecture", "delivery": // reservados para evolución; YAML anidado ya se conserva sintácticamente
		default:
			err = fmt.Errorf("campo desconocido en especificación: %s", key)
		}
		if err != nil {
			return s, fmt.Errorf("%s: %w", key, err)
		}
	}
	for i := range s.Tasks {
		t := &s.Tasks[i]
		if t.Version == 0 {
			t.Version = task.Version
		}
		if t.Agente == "" {
			t.Agente = s.Agente
		}
		if t.LimiteIntentos == 0 {
			t.LimiteIntentos = task.DefaultLimiteIntentos
			if s.Limites.Intentos > 0 {
				t.LimiteIntentos = s.Limites.Intentos
			}
		}
		if t.LimiteLineas == 0 {
			t.LimiteLineas = task.DefaultLimiteLineas
			if s.Limites.Lineas > 0 {
				t.LimiteLineas = s.Limites.Lineas
			}
		}
	}
	return s, nil
}

// flattenStrings aplana requirements y reglas: lista simple, categorías
// anidadas, o una mezcla.
//
// enSecuencia distingue los dos usos de un mapa. Bajo la clave, un mapa
// son categorías (`functional:`) y el nombre de la categoría se descarta.
// Dentro de una LISTA, en cambio, `- texto: más texto` no es una
// categoría: es una frase con dos puntos, y YAML la lee como un mapa de
// un solo entry. Descartar ahí la clave se comía la mitad del requisito
// sin avisar —"la lógica no lee el teclado: eso vive en la terminal"
// llegaba al planificador como "eso vive en la terminal"—, así que se
// vuelve a unir.
func flattenStrings(n *yaml.Node) ([]string, error) {
	out, _, err := flattenConIDs(n)
	return out, err
}

// flattenConIDs es flattenStrings que además reconoce un requerimiento
// con id explícito ("- id: fondo" + "texto: ..."): ese id sobrevive a
// que el humano reescriba el texto. Sin id, el lugar queda vacío y
// Spec.IDs lo calcula del texto.
func flattenConIDs(n *yaml.Node) ([]string, []string, error) {
	var out, ids []string
	var walk func(*yaml.Node, bool) error
	walk = func(x *yaml.Node, enSecuencia bool) error {
		switch x.Kind {
		case yaml.ScalarNode:
			out = append(out, x.Value)
			ids = append(ids, "")
		case yaml.SequenceNode:
			for _, c := range x.Content {
				if err := walk(c, true); err != nil {
					return err
				}
			}
		case yaml.MappingNode:
			if enSecuencia {
				if id, texto, ok := conID(x); ok {
					out = append(out, texto)
					ids = append(ids, id)
					return nil
				}
			}
			for i := 1; i < len(x.Content); i += 2 {
				clave, valor := x.Content[i-1], x.Content[i]
				if enSecuencia && valor.Kind == yaml.ScalarNode {
					texto := clave.Value
					if valor.Value != "" {
						texto += ": " + valor.Value
					}
					out = append(out, texto)
					ids = append(ids, "")
					continue
				}
				if err := walk(valor, false); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("se esperaba texto, lista o categorías")
		}
		return nil
	}
	err := walk(n, false)
	return out, ids, err
}

// conID lee un requerimiento {id, texto}; "text" también vale.
func conID(x *yaml.Node) (id, texto string, ok bool) {
	for i := 1; i < len(x.Content); i += 2 {
		switch x.Content[i-1].Value {
		case "id":
			id = strings.TrimSpace(x.Content[i].Value)
		case "texto", "text":
			texto = strings.TrimSpace(x.Content[i].Value)
		}
	}
	return id, texto, id != "" && texto != ""
}

func decodeAcceptance(n *yaml.Node) ([]Acceptance, error) {
	if n.Kind == yaml.MappingNode {
		var out []Acceptance
		for i := 1; i < len(n.Content); i += 2 {
			a, err := decodeAcceptance(n.Content[i])
			if err != nil {
				return nil, err
			}
			out = append(out, a...)
		}
		return out, nil
	}
	if n.Kind != yaml.SequenceNode {
		return []Acceptance{{Criterion: n.Value}}, nil
	}
	var out []Acceptance
	for _, item := range n.Content {
		if item.Kind == yaml.ScalarNode {
			out = append(out, Acceptance{Criterion: item.Value})
			continue
		}
		var raw struct {
			Criterion string `yaml:"criterion"`
			Criterio  string `yaml:"criterio"`
			Command   string `yaml:"command"`
			Comando   string `yaml:"comando"`
		}
		if err := item.Decode(&raw); err != nil {
			return nil, err
		}
		if raw.Criterion == "" {
			raw.Criterion = raw.Criterio
		}
		if raw.Command == "" {
			raw.Command = raw.Comando
		}
		if raw.Criterion == "" {
			return nil, fmt.Errorf("criterio de aceptación vacío")
		}
		out = append(out, Acceptance{Criterion: raw.Criterion, Command: raw.Command})
	}
	return out, nil
}

func decodeTasks(n *yaml.Node) ([]task.Task, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("tasks debe ser una lista")
	}
	var out []task.Task
	for _, item := range n.Content {
		if item.Kind == yaml.ScalarNode {
			out = append(out, task.Task{Version: task.Version, Titulo: item.Value})
			continue
		}
		var raw struct {
			ID         string   `yaml:"id"`
			Titulo     string   `yaml:"titulo"`
			Porque     string   `yaml:"porque"`
			Listo      string   `yaml:"listo_cuando"`
			Tocar      []string `yaml:"tocar_solo"`
			NoTocar    []string `yaml:"no_tocar"`
			Depende    []string `yaml:"depende_de"`
			Expone     []string `yaml:"expone"`
			Usa        []string `yaml:"usa"`
			Riesgos    string   `yaml:"riesgos"`
			Peso       string   `yaml:"peso"`
			Agente     string   `yaml:"agente"`
			Skills     []string `yaml:"skills"`
			Intentos   int      `yaml:"limite_intentos"`
			Lineas     int      `yaml:"limite_lineas"`
			Notas      string   `yaml:"notas"`
			Title      string   `yaml:"title"`
			Why        string   `yaml:"why"`
			DoneWhen   string   `yaml:"done_when"`
			OnlyTouch  []string `yaml:"only_touch"`
			DontTouch  []string `yaml:"dont_touch"`
			DependsOn  []string `yaml:"depends_on"`
			Exposes    []string `yaml:"exposes"`
			Uses       []string `yaml:"uses"`
			Risks      string   `yaml:"risks"`
			Weight     string   `yaml:"weight"`
			Agent      string   `yaml:"agent"`
			MaxRetries int      `yaml:"max_retries"`
			MaxLines   int      `yaml:"max_lines"`
			Notes      string   `yaml:"notes"`
		}
		if err := item.Decode(&raw); err != nil {
			return nil, err
		}
		out = append(out, task.Task{
			Version: task.Version, ID: raw.ID,
			Titulo: primero(raw.Title, raw.Titulo), Porque: primero(raw.Why, raw.Porque), ListoCuando: primero(raw.DoneWhen, raw.Listo),
			TocarSolo: primeros(raw.OnlyTouch, raw.Tocar), NoTocar: primeros(raw.DontTouch, raw.NoTocar), DependeDe: primeros(raw.DependsOn, raw.Depende),
			Expone: primeros(raw.Exposes, raw.Expone), Usa: primeros(raw.Uses, raw.Usa), Riesgos: primero(raw.Risks, raw.Riesgos),
			Peso: task.NormalizarPeso(primero(raw.Weight, raw.Peso)), Agente: primero(raw.Agent, raw.Agente), Skills: raw.Skills,
			LimiteIntentos: max(raw.MaxRetries, raw.Intentos), LimiteLineas: max(raw.MaxLines, raw.Lineas), Notas: primero(raw.Notes, raw.Notas),
		})
	}
	return out, nil
}

func primero(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func primeros(a, b []string) []string {
	if a != nil {
		return a
	}
	return b
}
