package ship

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AceptacionFile guarda el desenlace de la última entrega conjunta. Antes
// el resultado de los comandos de aceptación se imprimía y se perdía:
// nadie podía saber después si el feature quedó probado, que es lo que
// `loom archive` exige para archivarlo.
const AceptacionFile = "aceptacion.json"

// Criterio es un comando de aceptación del spec y cómo le fue. Corrio es
// falso si la entrega se cortó antes de llegar a él.
type Criterio struct {
	Comando string `json:"comando"`
	Corrio  bool   `json:"corrio"`
	Paso    bool   `json:"paso"`
}

// Aceptacion es lo que quedó probado en una entrega conjunta.
type Aceptacion struct {
	Fecha    time.Time `json:"fecha"`
	Rama     string    `json:"rama,omitempty"`
	Commit   string    `json:"commit,omitempty"`
	Aprobado bool      `json:"aprobado"`
	// Motivo es el primer paso que frenó, si no quedó aprobada.
	Motivo string `json:"motivo,omitempty"`
	// Spec es el hash de la intención del spec que se probó.
	Spec      string     `json:"spec,omitempty"`
	Fallo     *Fallo     `json:"fallo,omitempty"`
	Tareas    []string   `json:"tareas"`
	Criterios []Criterio `json:"criterios"`
}

// aceptacionDe resume una entrega: qué tareas llevó y qué comando de
// aceptación pasó, falló o no llegó a correr.
func aceptacionDe(o OpcionesEntrega, e Entrega) Aceptacion {
	a := Aceptacion{Fecha: time.Now().UTC().Truncate(time.Second), Rama: e.Rama, Aprobado: e.Aprobado, Motivo: e.PrimerMotivo(), Spec: o.Spec, Fallo: e.Fallo}
	for _, t := range o.Tareas {
		a.Tareas = append(a.Tareas, t.ID)
	}
	for _, c := range o.Acceptance {
		cr := Criterio{Comando: c}
		for _, p := range e.Pasos {
			if p.Nombre == "aceptación" && (p.Detalle == c || strings.HasPrefix(p.Detalle, c+" · ")) {
				cr.Corrio, cr.Paso = true, p.OK
			}
		}
		a.Criterios = append(a.Criterios, cr)
	}
	if e.Rama != "" {
		if out, err := gitRun(o.Root, "rev-parse", "--verify", "--quiet", e.Rama); err == nil {
			a.Commit = strings.TrimSpace(out)
		}
	}
	return a
}

func guardarAceptacion(root string, a Aceptacion) error {
	dir := filepath.Join(root, ".loom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, AceptacionFile), append(b, '\n'), 0o644)
}

// LeerAceptacion devuelve el desenlace de la última entrega conjunta;
// false si nunca hubo una.
func LeerAceptacion(root string) (Aceptacion, bool) {
	var a Aceptacion
	b, err := os.ReadFile(filepath.Join(root, ".loom", AceptacionFile))
	if err != nil || json.Unmarshal(b, &a) != nil {
		return a, false
	}
	return a, true
}
