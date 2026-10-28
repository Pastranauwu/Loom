package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Pastranauwu/loom/internal/ventanas"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestLineasCuenta(t *testing.T) {
	c := ventanas.Cuenta{Proveedor: "claude", Plan: "pro", Medidores: []ventanas.Medidor{
		{Nombre: "5h", Pct: 23, Reinicia: time.Now().Add(3*time.Hour + 12*time.Minute + 30*time.Second)},
		{Nombre: "semana", Pct: 80, Reinicia: time.Now().Add(-time.Minute)},
	}}
	ls := lineasCuenta(c, 50)
	txt := ansi.ReplaceAllString(strings.Join(ls, "\n"), "")
	for _, quiero := range []string{"claude · pro", "23%", "↻ 3h12m", "  0%"} {
		if !strings.Contains(txt, quiero) {
			t.Errorf("falta %q en:\n%s", quiero, txt)
		}
	}
	if strings.Contains(txt, "80%") {
		t.Error("una ventana ya reiniciada no muestra el porcentaje viejo")
	}
	if m := ansi.ReplaceAllString(Medidor(50, 10), ""); m != "█████░░░░░" {
		t.Errorf("medidor = %q", m)
	}
	if got := ansi.ReplaceAllString(strings.Join(lineasCuenta(ventanas.Cuenta{Proveedor: "claude", Motivo: "sin red"}, 50), ""), ""); !strings.Contains(got, "claude") {
		t.Errorf("sin datos dice por qué: %q", got)
	}
}
