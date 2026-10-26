package metrics

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Pastranauwu/loom/internal/config"
)

// Snapshot es una fotografía de las métricas en un momento dado. Cada
// corrida de `report` apunta una línea en .loom/historial.jsonl para
// poder dibujar la flecha de tendencia: valor actual frente a la
// corrida anterior. Nada se recalcula leyendo el repo.
type Snapshot struct {
	Fecha    time.Time `json:"fecha"`
	Metricas Metricas  `json:"metricas"`
}

func historialPath(root string) string {
	return filepath.Join(config.Dir(root), "historial.jsonl")
}

// GuardarHistorial apunta las métricas actuales al final del historial,
// una línea JSON por corrida.
func GuardarHistorial(root string, m Metricas) error {
	p := historialPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(Snapshot{Fecha: time.Now().UTC(), Metricas: m})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// LeerHistorial devuelve todas las fotografías apuntadas, más antigua
// primero. Sin historial no es un error.
func LeerHistorial(root string) ([]Snapshot, error) {
	f, err := os.Open(historialPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Snapshot
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		var s Snapshot
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Tendencia es la flecha de cada métrica: ↑ subió, ↓ bajó, · sin cambio
// o sin dato previo.
type Tendencia struct {
	IntentosHastaVerde string `json:"intentos_hasta_verde"`
	Ruido              string `json:"ruido_pct"`
	Roce               string `json:"roce_por_10"`
	Friccion           string `json:"friccion_min"`
	RechazoEntrada     string `json:"rechazo_entrada_pct"`
}

// Comparar devuelve la tendencia de curr frente a prev. Si prev es nil
// (primera corrida) todas las flechas son "·".
func Comparar(prev *Metricas, curr Metricas) Tendencia {
	if prev == nil {
		return Tendencia{
			IntentosHastaVerde: "·",
			Ruido:              "·",
			Roce:               "·",
			Friccion:           "·",
			RechazoEntrada:     "·",
		}
	}
	return Tendencia{
		IntentosHastaVerde: flecha(prev.IntentosHastaVerde, curr.IntentosHastaVerde),
		Ruido:              flecha(prev.Ruido, curr.Ruido),
		Roce:               flecha(prev.Roce, curr.Roce),
		Friccion:           flechaPtr(prev.Friccion, curr.Friccion),
		RechazoEntrada:     flecha(prev.RechazoEntrada, curr.RechazoEntrada),
	}
}

// Reporte es lo que `report` muestra: las métricas actuales y su
// tendencia frente a la corrida anterior.
type Reporte struct {
	Metricas  Metricas  `json:"metricas"`
	Tendencia Tendencia `json:"tendencia"`
}

// flecha compara dos valores redondeados a un decimal: ↑ subió, ↓ bajó,
// · sin cambio (dentro de media décima).
func flecha(prev, curr float64) string {
	diff := curr - prev
	if diff > 0.05 {
		return "↑"
	}
	if diff < -0.05 {
		return "↓"
	}
	return "·"
}

// flechaPtr compara valores que pueden ser null.
func flechaPtr(prev, curr *float64) string {
	if prev == nil || curr == nil {
		return "·"
	}
	return flecha(*prev, *curr)
}
