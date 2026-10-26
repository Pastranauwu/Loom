package spec

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const FeatureStateFile = "feature.json"

// SaveFeatureState conserva la intención humana separada del IR de tareas.
func SaveFeatureState(root string, s Spec) error {
	dir := filepath.Join(root, ".loom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FeatureStateFile), append(b, '\n'), 0o644)
}
func LoadFeatureState(root string) (Spec, error) {
	b, err := os.ReadFile(filepath.Join(root, ".loom", FeatureStateFile))
	if err != nil {
		return Spec{}, err
	}
	var s Spec
	err = json.Unmarshal(b, &s)
	return s, err
}
func (s Spec) AcceptanceCommands() []string {
	var out []string
	for _, a := range s.Acceptance {
		if a.Command != "" {
			out = append(out, a.Command)
		}
	}
	return out
}

// IntencionFile guarda lo que el humano declaró la última vez que se
// planeó el spec. Sin esto, cada `up` sobre un spec de requirements
// volvía a pagar al arquitecto y duplicaba las tareas; con esto, un spec
// igual no se replanea y uno editado solo manda lo que cambió.
const IntencionFile = "intencion.json"

// Intencion es la parte del spec que decide qué construir. Agentes, ship
// y límites no cambian el plan.
type Intencion struct {
	Feature      string       `json:"feature"`
	Requirements []string     `json:"requirements,omitempty"`
	IDs          []string     `json:"requirement_ids,omitempty"`
	Reglas       []string     `json:"reglas,omitempty"`
	Acceptance   []Acceptance `json:"acceptance,omitempty"`
	Constraints  Constraints  `json:"constraints,omitempty"`
}

func IntencionDe(s Spec) Intencion {
	return Intencion{Feature: s.Feature, Requirements: s.Requirements, IDs: s.IDs(), Reglas: s.Reglas, Acceptance: s.Acceptance, Constraints: s.Constraints}
}

// Igual compara dos intenciones por contenido. Los ids se completan
// antes: una intención guardada antes de que existieran no cuenta como
// cambio (replanearía todo el spec al actualizar loom).
func (i Intencion) Igual(o Intencion) bool {
	return i.Hash() == o.Hash()
}

// Hash identifica lo que el spec pide construir. Dos specs con la misma
// intención dan el mismo hash aunque cambien comentarios, el motivo o el
// orden de las claves: es lo que la entrega anota para que `loom
// archive` no archive un spec distinto del que se probó.
func (i Intencion) Hash() string {
	i.IDs = i.TodosLosIDs()
	b, _ := json.Marshal(i)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// TodosLosIDs devuelve un id por requerimiento, calculando los que falten.
func (i Intencion) TodosLosIDs() []string {
	return Spec{Requirements: i.Requirements, RequirementIDs: i.IDs}.IDs()
}

func SaveIntencion(root string, i Intencion) error {
	b, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ".loom", IntencionFile), append(b, '\n'), 0o644)
}

// LoadIntencion devuelve la última intención planeada; false si nunca se
// planeó un spec en este repo.
func LoadIntencion(root string) (Intencion, bool) {
	b, err := os.ReadFile(filepath.Join(root, ".loom", IntencionFile))
	if err != nil {
		return Intencion{}, false
	}
	var i Intencion
	if json.Unmarshal(b, &i) != nil {
		return Intencion{}, false
	}
	return i, true
}
