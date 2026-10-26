// Package budget implementa el tope de gasto de una corrida
// (`presupuesto_tokens` en config.yml). Es la única salvaguarda contra
// una recursión que se descontrola: sin tope, una tarea de dos niveles
// de profundidad por cinco subtareas por tres intentos puede quemar lo
// que no se ve venir.
//
// El contador en memoria es la fuente de verdad de la corrida (suma los
// tokens que los intentos reportan de verdad). GastoEnDisco reconstruye
// la misma cifra desde los archivos para board/standup que corren desde
// otra terminal: como los intentos de las subtareas ya viven en
// .loom/runs/<id>/ (no dentro del cuarto que se destruye), barrer
// esos attempts.jsonl alcanza — sin latidos, para no contar doble.
package budget

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/Pastranauwu/loom/internal/examiner"
	"github.com/Pastranauwu/loom/internal/loop"
)

// Contador es un tope compartido entre todas las tareas de una corrida.
// Gastar es atómico: N tareas en paralelo pueden pedir al mismo tiempo
// sin pasarse del tope.
type Contador struct {
	limite int
	usado  int64
	mu     sync.Mutex
}

// Nuevo crea un contador con un tope; limite <= 0 significa sin tope.
func Nuevo(limite int) *Contador { return &Contador{limite: limite} }

// Gastar cuenta n tokens si todavía hay margen. Devuelve false cuando la
// corrida ya quemó su presupuesto: el llamador debe detenerse.
func (c *Contador) Gastar(n int) bool {
	if c == nil || c.limite <= 0 {
		return true
	}
	if n <= 0 {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.usado+int64(n) > int64(c.limite) {
		return false
	}
	c.usado += int64(n)
	return true
}

// Usado devuelve los tokens gastados hasta ahora.
func (c *Contador) Usado() int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.usado
}

// Restante devuelve lo que queda del presupuesto (0 si no hay tope).
func (c *Contador) Restante() int64 {
	if c == nil || c.limite <= 0 {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if r := int64(c.limite) - c.usado; r > 0 {
		return r
	}
	return 0
}

// Limite devuelve el tope configurado.
func (c *Contador) Limite() int {
	if c == nil {
		return 0
	}
	return c.limite
}

// Agotado reporta si no queda margen. Sin tope configurado (limite <= 0)
// nunca está agotado: correr sin presupuesto es el default, no un corte.
func (c *Contador) Agotado() bool {
	if c == nil || c.limite <= 0 {
		return false
	}
	return c.Restante() <= 0
}

// GastoEnDisco suma los tokens de todos los attempts.jsonl del proyecto,
// de tareas raíz y de subtareas de la recursión por igual — los archivos
// no distinguen. No usa el contador en memoria, así que sirve desde otra
// terminal o tras una corrida interrumpida. Subestima apenas lo que está
// en vuelo ahora mismo (el intento en curso todavía no se escribió).
func GastoEnDisco(root string) int {
	runs := loop.RunsDir(root)
	entradas, err := os.ReadDir(runs)
	if err != nil {
		return 0
	}
	total := 0
	for _, e := range entradas {
		if !e.IsDir() {
			continue
		}
		as, err := loop.ReadAttempts(root, e.Name())
		if err != nil {
			continue
		}
		for _, a := range as {
			total += a.Tokens.Gasto()
		}
		if usos, err := examiner.LeerUso(root, e.Name()); err == nil {
			for _, u := range usos {
				total += u.Gasto()
			}
		}
	}
	return total
}

// FormatearGasto renderiza una cifra en la unidad humana: 12.4k para
// miles, 1.2M para millones.
func FormatearGasto(n int) string {
	switch {
	case n >= 1_000_000:
		return fnum(float64(n)/1_000_000) + "M"
	case n >= 1_000:
		return fnum(float64(n)/1_000) + "k"
	default:
		return strconv.Itoa(n)
	}
}

// fnum deja un número con un solo decimal, sin ceros de sobra.
func fnum(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	if len(s) > 1 && s[len(s)-1] == '0' {
		s = s[:len(s)-2]
	}
	return s
}

// celdas es el ancho de la barra de presupuesto.
const celdas = 12

// Barra renderiza la barra de presupuesto: ██ relleno por lo quemado,
// ░░ por lo que queda, y las cifras en unidades humanas.
func Barra(usado, limite int) string {
	if limite <= 0 {
		return FormatearGasto(usado) + " tokens (sin tope)"
	}
	pct := float64(usado) / float64(limite)
	if pct > 1 {
		pct = 1
	}
	llenas := int(pct * celdas)
	if llenas > celdas {
		llenas = celdas
	}
	barra := strings.Repeat("█", llenas) + strings.Repeat("░", celdas-llenas)
	restante := limite - usado
	if restante < 0 {
		restante = 0
	}
	return fmt.Sprintf("%s %s / %s · %d%% · quedan %s", barra, FormatearGasto(usado), FormatearGasto(limite), int(pct*100), FormatearGasto(restante))
}
