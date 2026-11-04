package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Pastranauwu/loom/internal/topo"
)

func TestVistaArquitectura(t *testing.T) {
	m := topo.Mapa{NumCapas: 2,
		Modulos:    []topo.Modulo{{Ruta: "cmd/app", Archivos: 3, Capa: 0}, {Ruta: "internal/db", Archivos: 5, Capa: 1, Proposito: "guarda las órdenes"}, {Ruta: "web/db", Archivos: 1, Capa: 1, Contenido: []topo.Parte{{Archivo: "pool.py", Doc: "reusa conexiones"}}}, {Ruta: "cmd/app/tests", Archivos: 9, Capa: 0}},
		Aristas:    []topo.Arista{{De: "cmd/app", A: "internal/db"}},
		Compuertas: topo.Compuertas{Tareas: map[string]int{"lista": 4}}}
	if EnObra(m) {
		t.Fatal("todo listo no es obra")
	}
	txt := ansi.ReplaceAllString(VistaArquitectura(m, 60), "")
	for _, quiero := range []string{"app · 3 archivos", "internal/db · 5 archivos", "web/db · 1 archivo", "→ internal/db", "guarda las órdenes", "▼", "pool reusa conexiones"} {
		if !strings.Contains(txt, quiero) {
			t.Errorf("falta %q en:\n%s", quiero, txt)
		}
	}
	if got := lista([]string{"alfa", "beta", "gama", "delta"}, 16); got != "alfa, beta +2" {
		t.Errorf("lista = %q", got)
	}
}

func TestArquitecturaAjustaAlAncho(t *testing.T) {
	largo := "guarda los productos elegidos y calcula el total con impuestos y el costo de envío por pedido"
	m := topo.Mapa{NumCapas: 1, Modulos: []topo.Modulo{{Ruta: "carrito", Archivos: 2, Proposito: largo, NumSimbolos: 2, Simbolos: []string{"class Carrito(models.Model)", "def total(c)"}}}}
	for _, ancho := range []int{40, 64, 100} {
		txt := ansi.ReplaceAllString(VistaArquitectura(m, ancho), "")
		for _, l := range strings.Split(txt, "\n") {
			if w := lipgloss.Width(l); w > ancho {
				t.Errorf("ancho %d: línea de %d: %q", ancho, w, l)
			}
		}
		plano := strings.Join(strings.Fields(strings.NewReplacer("│", " ").Replace(txt)), " ")
		if !strings.Contains(plano, "envío por pedido") || !strings.Contains(plano, "◇ Carrito · total") {
			t.Errorf("ancho %d: el propósito se ajusta sin cortarse y se ve qué contiene:\n%s", ancho, txt)
		}
	}
}

func TestArquitecturaSinAndamio(t *testing.T) {
	m := topo.Mapa{NumCapas: 1, Modulos: []topo.Modulo{{Ruta: "core/tests"}, {Ruta: "core/migrations"}, {Ruta: "."}}}
	txt := ansi.ReplaceAllString(VistaArquitectura(m, 60), "")
	if strings.Contains(txt, "tests") || strings.Contains(txt, "migrations") || !strings.Contains(txt, "raíz") {
		t.Fatalf("andamio en el bosquejo:\n%s", txt)
	}
}

func TestLineaArchivoPintaCadaRenglon(t *testing.T) {
	AplicarTema("loom")
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	l := lineaArchivo("cobro", "cobra con tarjeta y guarda el recibo de cada pedido del cliente", 24)
	rs := strings.Split(l, "\n")
	if len(rs) < 2 {
		t.Fatalf("debe partirse en varios renglones: %q", l)
	}
	for i, r := range rs[1:] {
		if !strings.HasPrefix(r, "\x1b[") {
			t.Errorf("el renglón %d perdió el color: %q", i+2, r)
		}
	}
}

func TestArquitecturaConIconosNerdCabeEnElAncho(t *testing.T) {
	ic()
	iconosAhora = iconosNerd
	defer func() { iconosAhora = iconosPlano }()
	m := topo.Mapa{NumCapas: 1, Modulos: []topo.Modulo{{Ruta: "pagos", Lenguaje: ".py", Archivos: 2,
		Contenido: []topo.Parte{{Archivo: "cobro.py", Doc: "cobra con tarjeta"}}, NumSimbolos: 1, Simbolos: []string{"class Pedido"}}}}
	txt := ansi.ReplaceAllString(VistaArquitectura(m, 60), "")
	for _, quiero := range []string{"\ue606 pagos", "\ue606 cobro", "\ueb5b Pedido", "\uebd2 "} {
		if !strings.Contains(txt, quiero) {
			t.Errorf("falta %q:\n%s", quiero, txt)
		}
	}
	for _, l := range strings.Split(txt, "\n") {
		if w := lipgloss.Width(l); w > 60 {
			t.Errorf("línea de %d: %q", w, l)
		}
	}
}
