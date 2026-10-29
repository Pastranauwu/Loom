package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/ventanas"
)

// Consumo is the usage pane of loom ide: the real account windows of each
// CLI (what its /usage shows), then loom's own task counts and spend.
// claude is queried live; codex is the snapshot of its last session;
// opencode has no account meter.
func Consumo(root string, ancho, alto int) string {
	cfg, _ := config.Load(root)
	var ls []string
	if c := ventanas.CuentaClaudeCached(context.Background(), false); usaCLI(cfg, "claude") || len(c.Medidores) > 0 {
		ls = append(ls, lineasCuenta(c, ancho)...)
	}
	if c := ventanas.CuentaCodex(); usaCLI(cfg, "codex") || len(c.Medidores) > 0 {
		ls = append(ls, lineasCuenta(c, ancho)...)
	}
	if usaCLI(cfg, "opencode") {
		ls = append(ls, estiloTitulo.Render("opencode")+estiloApagado.Render(" · "+idioma.T("sin medidor de cuenta")))
	}
	ls = append(ls, lineaLoom(root, cfg))
	for _, l := range lineasPresupuesto(root) {
		ls = append(ls, estiloApagado.Render("  tokens "+l))
	}
	if alto > 0 && len(ls) > alto {
		ls = ls[:alto]
	}
	for i, l := range ls {
		ls[i] = lipgloss.NewStyle().MaxWidth(ancho).Render(l)
	}
	return strings.Join(ls, "\n")
}

func usaCLI(cfg config.Config, cli string) bool {
	if cfg.Cli == cli {
		return true
	}
	for _, m := range cfg.TodosLosAgentes() {
		if m.Provider == cli || strings.Contains(strings.ToLower(m.Modelo), cli) {
			return true
		}
	}
	return false
}

func lineasCuenta(c ventanas.Cuenta, ancho int) []string {
	cab := estiloTitulo.Render(c.Proveedor)
	if c.Plan != "" {
		cab += estiloApagado.Render(" · " + c.Plan)
	}
	if c.Motivo != "" {
		return []string{cab + estiloApagado.Render(" · "+idioma.T(c.Motivo))}
	}
	if c.Proveedor == "codex" && time.Since(c.Leido) > time.Hour {
		cab += estiloApagado.Render(" · " + idioma.T("última sesión") + " " + hace(c.Leido))
	}
	if c.Proveedor == "claude" && time.Since(c.Leido) > 10*time.Minute {
		cab += estiloApagado.Render(" · " + idioma.T("leído") + " " + hace(c.Leido))
	}
	out := []string{cab}
	barra := min(max(ancho-25, 6), 30)
	for _, m := range c.Medidores {
		if !m.Reinicia.IsZero() && m.Reinicia.Before(time.Now()) {
			m.Pct, m.Reinicia = 0, time.Time{} // la ventana ya se reinició desde la lectura
		}
		nombre := m.Nombre
		if nombre == "semana" {
			nombre = idioma.T("semana")
		}
		fila := fmt.Sprintf("  %-7s %s %s", nombre, Medidor(m.Pct, barra), estiloDePct(m.Pct).Render(fmt.Sprintf("%3d%%", m.Pct)))
		if !m.Reinicia.IsZero() {
			fila += estiloApagado.Render("  ↻ " + falta(m.Reinicia))
		}
		out = append(out, fila)
	}
	return out
}

// Medidor dibuja una barra de ancho celdas llena al pct, coloreada por
// cercanía al tope.
func Medidor(pct, ancho int) string {
	llenas := min(max(pct*ancho/100, 0), ancho)
	if pct > 0 && llenas == 0 {
		llenas = 1
	}
	return estiloDePct(pct).Render(strings.Repeat("█", llenas)) + estiloApagado.Render(strings.Repeat("░", ancho-llenas))
}

func estiloDePct(pct int) lipgloss.Style {
	switch {
	case pct >= 90:
		return estiloAlerta
	case pct >= 70:
		return estiloEspera
	}
	return estiloPresion
}

func lineaLoom(root string, cfg config.Config) string {
	l := estiloTitulo.Render("loom")
	if filas, err := Tablero(root); err == nil {
		n := map[string]int{}
		for _, f := range filas {
			n[f.Estado]++
		}
		l += estiloTinta.Render(fmt.Sprintf("  ✓ %d  ◐ %d  ⏸ %d  · %d", n[state.Lista], n[state.EnCurso], n[state.Detenida], n[state.Pendiente]))
	}
	return l
}

// falta es el tiempo hasta t, corto: 45m, 3h12m, 2d4h.
func falta(t time.Time) string {
	return corto(time.Until(t))
}

func hace(t time.Time) string {
	return idioma.T("hace") + " " + corto(time.Since(t))
}

func corto(d time.Duration) string {
	switch {
	case d <= 0:
		return "0m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
}
