//go:build bench

package loop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/constitution"
	"github.com/Pastranauwu/loom/internal/executor"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/task"
)

// TestBenchPrompts mide los prompts y los intentos de un proyecto real.
// No corre en `go test ./...`: pide el tag y la ruta del proyecto.
//
//	LOOM_BENCH_DIR=../prueba go test -tags bench -run TestBenchPrompts -v ./internal/loop/
//
// Los prompts se arman con el código actual sobre los contratos del
// proyecto, sin skills (dependen del rol) ni error previo. Los intentos
// salen de .loom/runs: son de la corrida que ya pasó, así que para
// comparar un cambio hay que correr el plan de nuevo con él.
// LOOM_BENCH_TAREA=T-008 vuelca además el prompt de esa tarea.
func TestBenchPrompts(t *testing.T) {
	root := os.Getenv("LOOM_BENCH_DIR")
	if root == "" {
		t.Skip("LOOM_BENCH_DIR sin definir")
	}
	tasks, err := task.List(config.TasksDir(root))
	if err != nil || len(tasks) == 0 {
		t.Fatalf("sin tareas en %s: %v", root, err)
	}
	constitucion, _ := constitution.Load(root)

	var prompts []string
	total := 0
	for _, tk := range tasks {
		p := promptPara(tk, tk.Usa, constitucion, nil, "", "", false)
		prompts = append(prompts, p)
		if tk.ID == os.Getenv("LOOM_BENCH_TAREA") {
			t.Logf("prompt de %s:\n%s", tk.ID, p)
		}
		total += len(p)
	}
	// el prefijo se mide entre las tareas que traen arquitectura: una
	// tarea de integración de un plan viejo no la lleva y lo corta
	var conArq []string
	for i, tk := range tasks {
		if plan.SepararNotas(tk.Notas).Arquitectura != "" {
			conArq = append(conArq, prompts[i])
		}
	}
	comun := prefijoComun(prompts)
	comunArq := prefijoComun(conArq)

	var intentos, conIntentos, primera int
	var tk Tokens
	var base, baseEscrita, conBase int
	for _, x := range tasks {
		as, _ := ReadAttempts(root, x.ID)
		if len(as) == 0 {
			continue
		}
		conIntentos++
		intentos += len(as)
		if c := as[0].SalidaCodigo; c != nil && *c == 0 && (as[0].Revision == nil || as[0].Revision.Aprobada) {
			primera++
		}
		for i, a := range as {
			g := a.Tokens
			if g.Turnos == 0 && g.CostoUSD == 0 {
				g = tokensDelLog(root, a.Log) // corrida anterior a la medición
			}
			tk.Entrada += g.Entrada
			tk.Salida += g.Salida
			tk.CacheLeida += g.CacheLeida
			tk.CacheEscrita += g.CacheEscrita
			tk.Turnos += g.Turnos
			tk.CostoUSD += g.CostoUSD
			if i == 0 && g.PrimerTurno > 0 {
				base += g.PrimerTurno
				baseEscrita += g.PrimerTurnoEscrita
				conBase++
			}
		}
	}

	t.Logf("tareas                  %d", len(tasks))
	t.Logf("prompt promedio         %d bytes (~%d tokens)", total/len(tasks), total/len(tasks)/4)
	t.Logf("prompts en total        %d bytes (~%d tokens)", total, total/4)
	t.Logf("prefijo común           %d bytes (todas) · %d bytes (%d con arquitectura)", comun, comunArq, len(conArq))
	if conIntentos > 0 {
		t.Logf("intentos por tarea      %.2f (%d tareas con corrida)", float64(intentos)/float64(conIntentos), conIntentos)
		t.Logf("verdes al 1er intento   %d/%d", primera, conIntentos)
		t.Logf("turnos por intento      %.1f", float64(tk.Turnos)/float64(intentos))
		if conBase > 0 {
			t.Logf("base del 1er turno      %d tokens promedio (%d sin caché) · %d tareas", base/conBase, baseEscrita/conBase, conBase)
		} else {
			t.Logf("base del 1er turno      sin medir (corrida con --output-format json)")
		}
		t.Logf("tokens                  entrada %d · salida %d · caché leída %d · caché escrita %d", tk.Entrada, tk.Salida, tk.CacheLeida, tk.CacheEscrita)
		t.Logf("costo                   %.3f usd", tk.CostoUSD)
	}
}

func prefijoComun(ps []string) int {
	if len(ps) == 0 {
		return 0
	}
	n := len(ps[0])
	for _, p := range ps[1:] {
		i := 0
		for i < n && i < len(p) && p[i] == ps[0][i] {
			i++
		}
		n = i
	}
	return n
}

// tokensDelLog reparsea el stdout guardado en el log de un intento. Las
// corridas anteriores a la medición registraron un gasto incompleto (sin
// modelUsage, turnos ni costo); el log tiene la salida cruda de claude.
func tokensDelLog(root, log string) Tokens {
	data, err := os.ReadFile(filepath.Join(root, log))
	if err != nil {
		return Tokens{}
	}
	s := string(data)
	ini := strings.Index(s, "=== stdout ===\n")
	fin := strings.LastIndex(s, "\n=== stderr ===")
	if ini == -1 || fin < ini {
		return Tokens{}
	}
	u := executor.ParseClaudeUsage(s[ini+len("=== stdout ===\n") : fin])
	return Tokens{Entrada: u.Input, Salida: u.Output, CacheLeida: u.CacheRead, CacheEscrita: u.CacheWrite,
		Turnos: u.Turns, PrimerTurno: u.FirstTurn, PrimerTurnoEscrita: u.FirstTurnWrite, CostoUSD: u.CostUSD}
}
