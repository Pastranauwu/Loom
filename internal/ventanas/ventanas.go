// Package ventanas implementa el ledger de gasto por ventanas rodantes
// (5 horas, semanal, mensual) por proveedor. Es la contrapartida local
// de las ventanas de uso reales de las cuentas: Anthropic no publica los
// límites absolutos en tokens de sus ventanas de 5h/semanal, y el CLI no
// los expone, así que loom lleva la cuenta de lo que ÉL quema y lo
// compara contra topes que el usuario declara por proveedor.
//
// El ledger es GLOBAL al usuario (en ~/.loom/ventanas.jsonl), no por
// proyecto: la ventana de 5 horas es de la cuenta, y si loom corre en
// varios repos, todos gastan del mismo plato.
//
// Registrar hace las dos cosas a la vez: registra el gasto y, si ese
// gasto pasaría un tope configurado para el proveedor en alguna ventana,
// no lo registra y devuelve false — el bucle corta. Sin topes
// configurados, registra siempre: el ledger es la fuente de las barras
// aunque no haya límite que hacer cumplir.
package ventanas

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Pastranauwu/loom/internal/budget"
)

// Ventanas soportadas, de menor a mayor.
const (
	Ventana5h      = "5h"
	VentanaSemanal = "semanal"
	VentanaMensual = "mensual"
)

// Duracion de cada ventana rodante.
func Duracion(v string) time.Duration {
	switch v {
	case Ventana5h:
		return 5 * time.Hour
	case VentanaSemanal:
		return 7 * 24 * time.Hour
	case VentanaMensual:
		return 30 * 24 * time.Hour
	}
	return 0
}

// ventanasMax es lo que el ledger conserva: la ventana más larga (30 días)
// más un margen. Todo evento más viejo se poda.
const ventanasMax = 31 * 24 * time.Hour

// Evento es una línea del ledger: un intento que gastó tokens.
type Evento struct {
	TS        time.Time `json:"ts"`
	Proveedor string    `json:"proveedor"`
	Tokens    int       `json:"tokens"`
}

// Registro es el ledger de gasto de la cuenta, con los topes por
// proveedor y ventana. Compartido entre todas las tareas de una corrida.
type Registro struct {
	path string
	caps map[string]map[string]int // proveedor -> ventana -> tope
	mu   sync.Mutex
	evs  []Evento // en memoria, cargado una vez desde disco
}

// LedgerPath devuelve dónde vive el ledger global del usuario.
func LedgerPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return filepath.Join(home, ".loom", "ventanas.jsonl")
}

// Nuevo crea el registro. caps puede ser nil (sin topes: solo mide).
func Nuevo(path string, caps map[string]map[string]int) *Registro {
	return &Registro{path: path, caps: caps}
}

// cargar lee el ledger de disco si no está en memoria. Debe tener r.mu.
func (r *Registro) cargar() {
	if r.evs != nil {
		return
	}
	r.evs = []Evento{}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return // no existe: ledger vacío
	}
	for _, linea := range splitLines(string(data)) {
		var e Evento
		if json.Unmarshal([]byte(linea), &e) != nil {
			continue
		}
		r.evs = append(r.evs, e)
	}
	r.podar()
}

// podar descarta los eventos más viejos que la ventana máxima. Debe tener r.mu.
func (r *Registro) podar() {
	corte := time.Now().UTC().Add(-ventanasMax)
	out := r.evs[:0]
	for _, e := range r.evs {
		if e.TS.After(corte) {
			out = append(out, e)
		}
	}
	r.evs = out
}

// Registrar anota un gasto de proveedor. Si ese gasto pasaría un tope
// configurado en alguna ventana, no lo anota y devuelve false: el bucle
// debe detenerse. Sin topes, anota siempre y devuelve true.
func (r *Registro) Registrar(proveedor string, n int) bool {
	if r == nil {
		return true // sin ledger configurado: nada que hacer cumplir
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 || proveedor == "" {
		return true
	}
	r.cargar()
	if cs, ok := r.caps[proveedor]; ok {
		ahora := time.Now().UTC()
		for ventana, tope := range cs {
			if tope <= 0 {
				continue
			}
			if r.gastoVentana(proveedor, ventana, ahora)+n > tope {
				return false
			}
		}
	}
	r.evs = append(r.evs, Evento{TS: time.Now().UTC(), Proveedor: proveedor, Tokens: n})
	// el ledger es para medir y proteger; si el disco falla, el registro
	// en memoria sigue valiendo para esta corrida
	_ = guardarLedger(r.path, r.evs)
	return true
}

// Gasto devuelve los tokens que el proveedor gastó dentro de la ventana.
func (r *Registro) Gasto(proveedor, ventana string) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cargar()
	return r.gastoVentana(proveedor, ventana, time.Now().UTC())
}

// Limites devuelve los topes configurados para un proveedor.
func (r *Registro) Limites(proveedor string) map[string]int {
	if r == nil || r.caps == nil {
		return nil
	}
	return r.caps[proveedor]
}

// gastoVentana suma los eventos del proveedor dentro de la ventana
// respecto a `ahora`. Debe tener r.mu.
func (r *Registro) gastoVentana(proveedor, ventana string, ahora time.Time) int {
	d := Duracion(ventana)
	if d <= 0 {
		return 0
	}
	corte := ahora.Add(-d)
	total := 0
	for _, e := range r.evs {
		if e.Proveedor == proveedor && e.TS.After(corte) {
			total += e.Tokens
		}
	}
	return total
}

// LineaVentanas arma la línea de barras de un proveedor con sus topes:
// "claude · 5h ██ 12k/40k · semanal ░ 45k/120k". Las ventanas sin tope
// configurado muestran el gasto pelado (solo si es > 0); sin nada, "".
func LineaVentanas(r *Registro, proveedor string) string {
	if r == nil {
		return ""
	}
	limites := r.Limites(proveedor)
	partes := []string{}
	for _, v := range []string{Ventana5h, VentanaSemanal, VentanaMensual} {
		gasto := r.Gasto(proveedor, v)
		tope := 0
		if limites != nil {
			tope = limites[v]
		}
		if tope > 0 {
			partes = append(partes, v+" "+budget.Barra(gasto, tope))
		} else if gasto > 0 {
			partes = append(partes, v+" "+budget.FormatearGasto(gasto))
		}
	}
	if len(partes) == 0 {
		return ""
	}
	return proveedor + " · " + strings.Join(partes, " · ")
}

// guardarLedger escribe los eventos. Debe tener r.mu.
func guardarLedger(path string, evs []Evento) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	for _, e := range evs {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == '\n' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
