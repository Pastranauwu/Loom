// Package executor wraps agent CLIs as subprocesses behind the
// Executor interface. loom does not implement its
// own tool loop: it inherits editing, context and tools from the CLI.
package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Executor is the adapter contract for agent CLIs.
type Executor interface {
	Name() string
	Available() error // verifica binario y versión
	Run(ctx context.Context, req Request) (Result, error)
	// Models lista los ids de modelo que este CLI acepta de verdad.
	// loom nunca inventa ids: los pide al CLI y valida contra esta
	// lista antes de gastar un token.
	Models(ctx context.Context) ([]string, error)
}

// Todos son los CLIs que loom sabe manejar, en orden de preferencia
// cuando nadie eligió uno.
var Todos = []Executor{OpenCode{}, Claude{}, Codex{}}

// Nombres devuelve los nombres de Todos, para mensajes y validación.
func Nombres() []string {
	var n []string
	for _, e := range Todos {
		n = append(n, e.Name())
	}
	return n
}

// Request is one agent invocation.
type Request struct {
	RoomPath     string   // cwd del agente, su cuarto
	Prompt       string   // contrato + resultado del intento anterior
	AllowedGlobs []string // tocar_solo
	Model        string
	Effort       string // intensidad opcional; vacío conserva el valor del CLI
	Timeout      time.Duration
	// Env carries the room's own variables (PORT, ...).
	Env []string
	// Rol decide qué herramientas recibe el agente; vacío es el
	// implementador.
	Rol Rol
	// Sesion continúa una conversación anterior del mismo CLI en este
	// cuarto (Result.Sesion): el agente conserva lo que ya leyó. Solo
	// claude; los demás la ignoran y quien llama manda el prompt completo.
	Sesion string
	// TopeUSD corta la invocación al pasar ese gasto, a precio de lista.
	// 0 = sin tope. Solo claude.
	TopeUSD int
	// Avance recibe, mientras el CLI corre, lo importante que va
	// haciendo (un comando, un archivo leído, un turno terminado). Nil
	// no reporta nada.
	Avance func(string)
	// avanceDe traduce una línea de stdout del CLI a un avance; "" la
	// ignora. Lo pone cada adaptador según su formato de eventos.
	avanceDe func(string) string
}

// Rol es para qué se invoca al agente. Cada herramienta que el CLI carga
// viaja en el contexto de todos los turnos, así que cada rol recibe solo
// las que usa.
type Rol string

const (
	RolImplementador Rol = ""
	// RolTexto responde con texto a partir del prompt: examinador,
	// revisor, constitución.
	RolTexto        Rol = "texto"
	RolPlanificador Rol = "planificador"
	// RolVisual solo lee archivos: mira las capturas de la interfaz.
	RolVisual Rol = "visual"
)

// Result is what one invocation produced.
type Result struct {
	FilesChanged []string `json:"files_changed"`
	Tokens       Usage    `json:"tokens"`
	Stdout       string   `json:"stdout"`
	Stderr       string   `json:"stderr"` // diagnóstico del CLI: sin esto un fallo de infra es invisible
	Text         string   `json:"text"`   // la respuesta textual del agente, si el adaptador la saca
	ExitCode     int      `json:"exit_code"`
	// Sesion identifica la conversación para continuarla con
	// Request.Sesion; "" si el CLI no la da.
	Sesion string `json:"sesion,omitempty"`
}

// ErrTope es una invocación cortada por Request.TopeUSD: no se arregla
// reintentando con el mismo tope.
var ErrTope = errors.New(idioma.T("tope de gasto alcanzado"))

// Usage is the token spend of one invocation, best-effort per adapter.
//
// Turns son las llamadas al modelo de la invocación. FirstTurn es el
// contexto con que arrancó el primer turno (entrada + caché leída + caché
// escrita): prompt de sistema, herramientas, skills y el prompt de
// loom, antes de que el agente trabaje. FirstTurnWrite es la parte
// de ese contexto que no estaba en caché.
type Usage struct {
	Input          int `json:"input"`
	Output         int `json:"output"`
	CacheRead      int `json:"cache_read,omitempty"`
	CacheWrite     int `json:"cache_write,omitempty"`
	Turns          int `json:"turns,omitempty"`
	FirstTurn      int `json:"first_turn,omitempty"`
	FirstTurnWrite int `json:"first_turn_write,omitempty"`
	// LastTurn is the context size of the final turn: what the session carries into the next call.
	LastTurn int `json:"last_turn,omitempty"`
	// CostUSD es el costo que el propio CLI calcula, a precio de lista.
	CostUSD float64 `json:"cost_usd,omitempty"`
}

// run executes a CLI with timeout and returns its stdout, stderr, exit
// code and error. A timeout yields exit code 124.
//
// stderr se captura aparte a propósito: es donde los CLIs de agente
// escriben el motivo real de un fallo (modelo inexistente, key ausente,
// rate limit). Descartarlo dejaba al usuario con "exit status 1" y nada
// más, que es indistinguible de "el agente trabajó y las pruebas
// fallaron".
func run(ctx context.Context, req Request, name string, args ...string) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	bin, err := findBinary(name)
	if err != nil {
		bin = name
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = req.RoomPath
	cmd.Env = append(os.Environ(), req.Env...)
	cmd.SysProcAttr = morirConPadre()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if req.Avance != nil && req.avanceDe != nil {
		cmd.Stdout = io.MultiWriter(&stdout, &lineas{fn: func(l string) {
			if msg := req.avanceDe(l); msg != "" {
				req.Avance(msg)
			}
		}})
	}
	// Sin WaitDelay, matar el proceso por timeout no basta: si el CLI
	// dejó un nieto vivo con el pipe abierto, cmd.Wait se queda esperando
	// para siempre y `timeout_agente` deja de ser un límite. Con esto se
	// cierran los pipes y la invocación vuelve, colgada o no.
	cmd.WaitDelay = 10 * time.Second

	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return stdout.String(), stderr.String(), 124, err
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), stderr.String(), exitErr.ExitCode(), err
		}
		return stdout.String(), stderr.String(), -1, err
	}
	return stdout.String(), stderr.String(), 0, nil
}

// SinSaldo reporta si un fallo es la cuenta del proveedor sin fondos. No
// es de la tarea sino de toda la corrida: reintentar o escalar de modelo
// por la misma cuenta devuelve el mismo 402. En una corrida real, un saldo agotado
// dejó 5 tareas detenidas tras 36 invocaciones fallidas y el resto
// colgado de ellas.
func SinSaldo(msg string) bool {
	m := strings.ToLower(msg)
	for _, s := range []string{"insufficient account funds", "insufficient funds", "insufficient balance", "credit balance is too low", "payment required"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

// lineas parte en líneas lo que el CLI va escribiendo y se las pasa a
// fn a medida que se completan.
type lineas struct {
	buf []byte
	fn  func(string)
}

func (l *lineas) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		l.fn(string(l.buf[:i]))
		l.buf = l.buf[i+1:]
	}
}

// recortar deja s en una línea de a lo sumo n runas.
func recortar(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// modelosDeCLI corre un subcomando que lista modelos, una línea cada
// uno, y devuelve los ids tal cual los acepta el CLI.
func modelosDeCLI(ctx context.Context, name string, args ...string) ([]string, error) {
	bin, err := findBinary(name)
	if err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		return nil, fmt.Errorf(idioma.T("%s no pudo listar modelos · %s"), name, err)
	}
	var ids []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			ids = append(ids, l)
		}
	}
	return ids, nil
}

func init() {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		cur := os.Getenv("PATH")
		paths := []string{
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, "bin"),
		}
		for _, p := range paths {
			if !strings.Contains(cur, p) {
				if info, err := os.Stat(p); err == nil && info.IsDir() {
					cur = p + string(os.PathListSeparator) + cur
				}
			}
		}
		_ = os.Setenv("PATH", cur)
	}
}

// findBinary busca un binario ejecutable en el PATH.
func findBinary(name string) (string, error) {
	bin, err := exec.LookPath(name)
	if err != nil {
		return "", errors.New(name + " no está instalado · instálalo o elige otro ejecutor")
	}
	return bin, nil
}
