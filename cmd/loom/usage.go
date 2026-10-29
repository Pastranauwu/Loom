package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/budget"
	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/examiner"
	"github.com/Pastranauwu/loom/internal/executor"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/ventanas"
)

func newUsageCmd() *cobra.Command {
	var sonda bool
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "gasto por ventanas (5h, semanal, mensual) y presupuesto",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsage(sonda)
		},
	}
	cmd.Flags().BoolVar(&sonda, "probe", false, "vuelve a consultar el uso de la cuenta de Claude (ignora la caché, respeta el 429)")
	return cmd
}

func runUsage(sonda bool) error {
	root, err := projectRoot()
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	var lineas []string

	if cfg.PresupuestoTokens > 0 {
		usado := budget.GastoEnDisco(root)
		lineas = append(lineas, "presupuesto absoluto "+budget.Barra(usado, cfg.PresupuestoTokens))
	}

	registro := ventanas.Nuevo(ventanas.LedgerPath(), cfg.PresupuestoVentanas)
	proveedores := map[string]bool{}
	for _, p := range config.Clis {
		proveedores[p] = true
	}
	for p := range cfg.PresupuestoVentanas {
		proveedores[p] = true
	}
	var nombres []string
	for p := range proveedores {
		nombres = append(nombres, p)
	}
	sort.Strings(nombres)
	for _, p := range nombres {
		if l := ventanas.LineaVentanas(registro, p); l != "" {
			lineas = append(lineas, "gasto "+l)
		}
	}

	if err := out.Data(lineas); err != nil {
		return err
	}

	for _, l := range lineas {
		out.Line("%s", l)
	}

	if err := mostrarGastoPorTarea(root); err != nil {
		return err
	}

	// uso real de las cuentas: lo mismo que muestra el /usage de cada CLI
	cuentas := []ventanas.Cuenta{ventanas.CuentaClaudeCached(context.Background(), sonda), ventanas.CuentaCodex()}
	if err := out.Data(cuentas); err != nil {
		return err
	}
	for _, c := range cuentas {
		if c.Motivo != "" {
			out.Line(idioma.T("cuenta %s · %s"), c.Proveedor, idioma.T(c.Motivo))
			continue
		}
		var partes []string
		for _, m := range c.Medidores {
			p := fmt.Sprintf("%s %d%%", idioma.T(m.Nombre), m.Pct)
			if !m.Reinicia.IsZero() && m.Reinicia.After(time.Now()) {
				p += " (" + idioma.T("reinicia") + " " + m.Reinicia.Local().Format("02 Jan 15:04") + ")"
			}
			partes = append(partes, p)
		}
		if c.Proveedor == "claude" && time.Since(c.Leido) > 10*time.Minute {
			partes = append(partes, idioma.T("leído")+" "+c.Leido.Local().Format("15:04"))
		}
		out.Line(idioma.T("cuenta %s · %s"), strings.TrimSpace(c.Proveedor+" "+c.Plan), strings.Join(partes, " · "))
	}
	return nil
}

// gastoTarea es el gasto de una tarea con un modelo, sumado de sus
// intentos. La entrada sin caché es una fracción mínima del prompt real:
// sin las dos columnas de caché el consumo parece casi nulo.
//
// PrimerTurno es el contexto base del primer intento: lo que el CLI carga
// (prompt de sistema, herramientas, skills, MCP) más el prompt de loom,
// antes de que el agente haga nada.
type gastoTarea struct {
	Tarea              string  `json:"tarea"`
	Modelo             string  `json:"modelo"`
	Intentos           int     `json:"intentos"`
	Turnos             int     `json:"turnos"`
	PrimerTurno        int     `json:"primer_turno"`
	PrimerTurnoEscrita int     `json:"primer_turno_escrita"`
	Entrada            int     `json:"entrada"`
	Salida             int     `json:"salida"`
	CacheLeida         int     `json:"cache_leida"`
	CacheEscrita       int     `json:"cache_escrita"`
	CostoUSD           float64 `json:"costo_usd"`
	// PicoContexto is the largest context a session reached; Reanudados counts attempts that continued a session.
	PicoContexto int `json:"pico_contexto"`
	Reanudados   int `json:"reanudados"`
}

// mostrarGastoPorTarea lee .loom/runs/*/attempts.jsonl y muestra el
// gasto por tarea y por modelo. Los intentos anteriores a la medición de
// turnos y caché salen con esas columnas en cero.
func mostrarGastoPorTarea(root string) error {
	dirs, err := os.ReadDir(loop.RunsDir(root))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var filas []gastoTarea
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		as, err := loop.ReadAttempts(root, d.Name())
		if err != nil {
			return err
		}
		por := map[string]*gastoTarea{}
		var modelos []string
		for _, a := range as {
			g := por[a.Modelo]
			if g == nil {
				g = &gastoTarea{Tarea: d.Name(), Modelo: a.Modelo, PrimerTurno: a.Tokens.PrimerTurno, PrimerTurnoEscrita: a.Tokens.PrimerTurnoEscrita}
				por[a.Modelo] = g
				modelos = append(modelos, a.Modelo)
			}
			g.Intentos++
			g.Turnos += a.Tokens.Turnos
			g.Entrada += a.Tokens.Entrada
			g.Salida += a.Tokens.Salida
			g.CacheLeida += a.Tokens.CacheLeida
			g.CacheEscrita += a.Tokens.CacheEscrita
			g.CostoUSD += a.Tokens.CostoUSD
			g.PicoContexto = max(g.PicoContexto, a.Tokens.UltimoTurno)
			if a.Reanudado {
				g.Reanudados++
			}
		}
		sort.Strings(modelos)
		for _, m := range modelos {
			filas = append(filas, *por[m])
		}
		// El arquitecto corre antes de los intentos de implementación y su
		// gasto no aparece en attempts.jsonl. Sus logs conservan el resultado
		// del CLI; los contamos una sola vez como fase separada.
		logs, err := filepath.Glob(filepath.Join(loop.RunsDir(root), d.Name(), "esqueleto-*.log"))
		if err != nil {
			return err
		}
		if len(logs) > 0 {
			g := gastoTarea{Tarea: d.Name() + "/arq", Modelo: "arquitecto"}
			for _, path := range logs {
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				var u executor.Usage
				usagePath := strings.TrimSuffix(path, ".log") + ".usage.json"
				if exacto, err := os.ReadFile(usagePath); err == nil {
					if err := json.Unmarshal(exacto, &u); err != nil {
						return fmt.Errorf(idioma.T("uso inválido en %s: %w"), usagePath, err)
					}
				} else if errors.Is(err, os.ErrNotExist) {
					// Los logs anteriores al archivo estructurado guardan solo
					// entrada, salida y USD redondeado en la primera línea.
					cabecera, _, _ := strings.Cut(string(b), "\n")
					var vuelta int
					_, _ = fmt.Sscanf(cabecera, "=== esqueleto %d · %d tokens de entrada · %d de salida · $%f", &vuelta, &u.Input, &u.Output, &u.CostUSD)
				} else {
					return err
				}
				g.Intentos++
				g.Turnos += u.Turns
				g.Entrada += u.Input
				g.Salida += u.Output
				g.CacheLeida += u.CacheRead
				g.CacheEscrita += u.CacheWrite
				g.CostoUSD += u.CostUSD
				if g.PrimerTurno == 0 {
					g.PrimerTurno, g.PrimerTurnoEscrita = u.FirstTurn, u.FirstTurnWrite
				}
			}
			filas = append(filas, g)
		}
		usos, err := examiner.LeerUso(root, d.Name())
		if err != nil {
			return err
		}
		if len(usos) > 0 {
			g := gastoTarea{Tarea: d.Name() + "/exam", Modelo: "examinador", Intentos: len(usos)}
			for _, u := range usos {
				g.Turnos += u.Turnos
				g.Entrada += u.Entrada
				g.Salida += u.Salida
				g.CacheLeida += u.CacheLeida
				g.CacheEscrita += u.CacheEscrita
				g.CostoUSD += u.CostoUSD
				if g.PrimerTurno == 0 {
					g.PrimerTurno, g.PrimerTurnoEscrita = u.PrimerTurno, u.PrimerTurnoEscrita
				}
			}
			filas = append(filas, g)
		}
	}
	if err := out.Data(filas); err != nil {
		return err
	}
	if len(filas) == 0 {
		return nil
	}
	const formato = "%-8s %-18s %4d %6d %9d %9d %9d %9d %11d %11d %8.3f %5s %6d"
	out.Line("")
	out.Line("%-8s %-18s %4s %6s %9s %9s %9s %9s %11s %11s %8s %5s %6s", "tarea", "modelo", "int", "turnos", "base 1er", "base escr", "entrada", "salida", "caché leída", "caché escr", "usd", "ctx%", "resume")
	var total gastoTarea
	for _, f := range filas {
		modelo := f.Modelo
		if modelo == "" {
			modelo = "(por defecto)"
		}
		out.Line(formato, f.Tarea, modelo, f.Intentos, f.Turnos, f.PrimerTurno, f.PrimerTurnoEscrita, f.Entrada, f.Salida, f.CacheLeida, f.CacheEscrita, f.CostoUSD, pctContexto(f), f.Reanudados)
		total.Intentos += f.Intentos
		total.Turnos += f.Turnos
		total.PrimerTurno += f.PrimerTurno
		total.PrimerTurnoEscrita += f.PrimerTurnoEscrita
		total.Entrada += f.Entrada
		total.Salida += f.Salida
		total.CacheLeida += f.CacheLeida
		total.CacheEscrita += f.CacheEscrita
		total.CostoUSD += f.CostoUSD
		total.Reanudados += f.Reanudados
	}
	out.Line(formato, "total", "", total.Intentos, total.Turnos, total.PrimerTurno, total.PrimerTurnoEscrita, total.Entrada, total.Salida, total.CacheLeida, total.CacheEscrita, total.CostoUSD, "", total.Reanudados)
	out.Line(idioma.T("usd = precio de lista informado por el CLI, no cargo de la suscripción · incluye implementación, arquitectura y examinador; otros roles pueden faltar"))
	return nil
}

// pctContexto is the session's peak context as a % of the model window.
func pctContexto(g gastoTarea) string {
	if g.PicoContexto == 0 {
		return "-"
	}
	return fmt.Sprintf("%d%%", g.PicoContexto*100/executor.ContextWindow(g.Modelo))
}
