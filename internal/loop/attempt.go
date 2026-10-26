// Package loop implements the work loop and its instrumentation:
// cada intento escribe una línea en
// .loom/runs/<id>/attempts.jsonl. Ese archivo es la única fuente de
// las métricas y del parte de datos; nada se recalcula leyendo el
// repo después.
package loop

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Attempt is one line of attempts.jsonl. Los campos que el bucle no
// puede derivar con certeza van en nil, que en JSON es null: un número
// que no se pudo medir nunca se inventa.
type Attempt struct {
	Intento                  int       `json:"intento"`
	Inicio                   time.Time `json:"inicio"`
	Fin                      time.Time `json:"fin"`
	SalidaCodigo             *int      `json:"salida_codigo"`
	TestsPasaron             *int      `json:"tests_pasaron"`
	TestsFallaron            *int      `json:"tests_fallaron"`
	ArchivosTocados          []string  `json:"archivos_tocados"`
	SimbolosExportados       *[]string `json:"simbolos_exportados"`
	LineasMas                int       `json:"lineas_mas"`
	LineasMenos              int       `json:"lineas_menos"`
	RevertidosFueraDeAlcance []string  `json:"revertidos_fuera_de_alcance"`
	Tokens                   Tokens    `json:"tokens"`
	Modelo                   string    `json:"modelo"`
	// Base es el árbol del commit del que partía el cuarto en este
	// intento. El árbol y no el commit: la rama de integración se rehace
	// en cada corrida y su hash cambia aunque el contenido sea el mismo.
	Base string `json:"base,omitempty"`

	// AgenteSalidaCodigo y ErrorAgente separan "el agente reventó" de
	// "el agente trabajó y las pruebas fallaron". Sin esta distinción un
	// modelo mal configurado se veía igual que un test rojo, y el bucle
	// quemaba los tres intentos contra un error de infraestructura.
	AgenteSalidaCodigo *int   `json:"agente_salida_codigo"`
	ErrorAgente        string `json:"error_agente,omitempty"`
	// Log es la ruta relativa al volcado completo de la invocación
	// (prompt, stdout y stderr del CLI).
	Log string `json:"log,omitempty"`

	// Revision es el veredicto del revisor sobre un intento verde en
	// tests (rol `revisor`): si pidió cambios, el intento no quedó
	// verde pese al exit 0. Nil en intentos sin revisión.
	Revision *Revision `json:"revision,omitempty"`
}

// Revision es lo que el revisor dictaminó sobre un intento que pasó
// listo_cuando. Solo existe cuando el bucle corre con un Revisor.
type Revision struct {
	Aprobada bool   `json:"aprobada"`
	Cambios  string `json:"cambios,omitempty"` // qué corregir, si no está aprobada
	// Visual es la crítica de la revisión con capturas, si la devolvió.
	Visual string `json:"visual,omitempty"`
}

// Tokens is the token spend of one attempt.
//
// Entrada es la entrada sin caché: con claude es una fracción mínima del
// prompt real, que llega casi entero como caché leída o escrita.
//
// PrimerTurno es el contexto con que arrancó el agente (entrada + caché
// leída + caché escrita del primer turno) y PrimerTurnoEscrita la parte
// que no estaba en caché: lo que el CLI carga antes de trabajar.
type Tokens struct {
	Entrada            int     `json:"entrada"`
	Salida             int     `json:"salida"`
	CacheLeida         int     `json:"cache_leida,omitempty"`
	CacheEscrita       int     `json:"cache_escrita,omitempty"`
	Turnos             int     `json:"turnos,omitempty"`
	PrimerTurno        int     `json:"primer_turno,omitempty"`
	PrimerTurnoEscrita int     `json:"primer_turno_escrita,omitempty"`
	CostoUSD           float64 `json:"costo_usd,omitempty"`
}

// Gasto es lo que cuenta contra el presupuesto y las ventanas: entrada y
// salida enteras, la caché con su peso de precio (leída 0.1×, escrita
// 1.25×). Sin la caché, un intento de claude con 789k leídos contaba 129.
func (t Tokens) Gasto() int {
	return t.Entrada + t.Salida + t.CacheLeida/10 + t.CacheEscrita*5/4
}

// RunsDir returns the runs directory of the repository at root.
func RunsDir(root string) string { return filepath.Join(root, ".loom", "runs") }

// attemptsPath returns the attempts.jsonl path of a task.
func attemptsPath(root, id string) string {
	return filepath.Join(RunsDir(root), id, "attempts.jsonl")
}

// store writes attempts.jsonl lines append-only. Cada Run abre uno.
type store struct {
	path string
}

func openStore(root, id string) (*store, error) {
	p := attemptsPath(root, id)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	return &store{path: p}, nil
}

// Append writes one attempt as a single JSON line.
func (s *store) Append(a Attempt) error {
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// ReadAttempts returns every recorded attempt of a task, oldest first.
// A task that never ran has no attempts (not an error).
func ReadAttempts(root, id string) ([]Attempt, error) {
	f, err := os.Open(attemptsPath(root, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Attempt
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		var a Attempt
		if err := json.Unmarshal([]byte(line), &a); err != nil {
			return nil, fmt.Errorf("attempts.jsonl corrupto · %s", err)
		}
		out = append(out, a)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
