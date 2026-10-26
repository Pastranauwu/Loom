// Package metrics computes the five project metrics from the artifacts:
// attempts.jsonl, the task states and the delivery records that ship
// leaves behind. Un agente nunca reporta su avance: todo se mide del
// artefacto del parte de datos.
package metrics

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Pastranauwu/loom/internal/loop"
)

// Entrega es lo que ship deja escrito al terminar una esclusa de salida,
// tanto si entregó como si se frenó. Alimenta las métricas de ruido y
// roce, que attempts.jsonl no cubre.
type Entrega struct {
	ID          string    `json:"id"`
	Fecha       time.Time `json:"fecha"`
	LineasMas   int       `json:"lineas_mas"`
	LineasMenos int       `json:"lineas_menos"`
	Ruido       int       `json:"ruido"`
	Conflicto   bool      `json:"conflicto"`
	PR          string    `json:"pr,omitempty"`
	Aprobado    bool      `json:"aprobado"`
	Brecha      *float64  `json:"brecha,omitempty"` // visible_pct - hidden_pct del examen ciego
}

func entregaPath(root, id string) string {
	return filepath.Join(loop.RunsDir(root), id, "entrega.json")
}

// GuardarEntrega escribe el registro de entrega de una tarea.
func GuardarEntrega(root string, e Entrega) error {
	p := entregaPath(root, e.ID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o644)
}

// ListarEntregas devuelve todos los registros de entrega, por ID.
func ListarEntregas(root string) ([]Entrega, error) {
	entries, err := os.ReadDir(loop.RunsDir(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entrega
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(loop.RunsDir(root), e.Name(), "entrega.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var en Entrega
		if err := json.Unmarshal(data, &en); err != nil {
			return nil, err
		}
		out = append(out, en)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
